package imgpack

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"
)

// fakeResizer は Resize の呼び出しを記録するだけのテスト用 Resizer。
type fakeResizer struct {
	mu    sync.Mutex
	calls []fakeCall
}

type fakeCall struct {
	files    []string
	geometry string
	quality  int
	jobs     int
}

func (f *fakeResizer) Resize(files []string, geometry string, quality, jobs int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeCall{append([]string(nil), files...), geometry, quality, jobs})
	return nil
}

func (f *fakeResizer) allFiles() []string {
	var all []string
	for _, c := range f.calls {
		all = append(all, c.files...)
	}
	sort.Strings(all)
	return all
}

func TestNormalizeExts(t *testing.T) {
	got := NormalizeExts([]string{".PNG", "jpg", " jpeg ", "jpg", "", "AVIF"})
	want := []string{"png", "jpg", "jpeg", "avif"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestIsImage(t *testing.T) {
	exts := DefaultExts()
	cases := map[string]bool{
		"a.png": true, "b.JPG": true, "c.avif": true, "d.heic": true,
		"e.txt": false, ".hidden.png": false, "noext": false, "f.zip": false,
	}
	for name, want := range cases {
		if got := isImage(name, exts); got != want {
			t.Errorf("isImage(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestResolveGeometry(t *testing.T) {
	cases := []struct {
		opt  Options
		want string
	}{
		{Options{Height: 1600}, "x1600>"},
		{Options{Width: 1200}, "1200>"},
		{Options{Width: 1200, Height: 1600}, "1200x1600>"},
		{Options{Geometry: "50%"}, "50%"},
		{Options{Height: 1600, Geometry: "800x800"}, "800x800"}, // Geometry が優先
		{Options{}, ""},
	}
	for _, c := range cases {
		if got := c.opt.ResolveGeometry(); got != c.want {
			t.Errorf("ResolveGeometry(%+v) = %q, want %q", c.opt, got, c.want)
		}
	}
}

func TestSplitChunks(t *testing.T) {
	files := []string{"a", "b", "c", "d", "e"}
	chunks := splitChunks(files, 2)
	if len(chunks) != 2 {
		t.Fatalf("len(chunks) = %d, want 2", len(chunks))
	}
	total := 0
	for _, c := range chunks {
		total += len(c)
	}
	if total != len(files) {
		t.Fatalf("分割で枚数が変わった: %d != %d", total, len(files))
	}
	// jobs > files のときは files 数に丸める。
	if got := len(splitChunks(files, 10)); got != len(files) {
		t.Fatalf("splitChunks over-jobs len = %d, want %d", got, len(files))
	}
}

// writeFile はテスト用にファイルを作る。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestZipDirAndExtractRoundTrip(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "p001.png"), "PNGDATA-1")
	writeFile(t, filepath.Join(src, "sub", "p002.jpg"), "JPGDATA-2")
	writeFile(t, filepath.Join(src, "notes.txt"), "hello")
	writeFile(t, filepath.Join(src, ".DS_Store"), "junk") // 除外されるべき

	dest := filepath.Join(t.TempDir(), "out.zip")
	if err := ZipDir(src, dest, "book", DefaultExts()); err != nil {
		t.Fatal(err)
	}

	// zip 内エントリ・圧縮方式を検査。
	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	methods := map[string]uint16{}
	for _, f := range zr.File {
		methods[f.Name] = f.Method
	}
	zr.Close()

	want := []string{"book/p001.png", "book/sub/p002.jpg", "book/notes.txt"}
	for _, w := range want {
		if _, ok := methods[w]; !ok {
			t.Errorf("エントリ %q が無い (entries=%v)", w, keys(methods))
		}
	}
	if _, ok := methods["book/.DS_Store"]; ok {
		t.Error(".DS_Store が除外されていない")
	}
	// 画像は Store、それ以外は Deflate。
	if methods["book/p001.png"] != zip.Store {
		t.Errorf("png は Store であるべき, got %d", methods["book/p001.png"])
	}
	if methods["book/notes.txt"] != zip.Deflate {
		t.Errorf("txt は Deflate であるべき, got %d", methods["book/notes.txt"])
	}

	// 解凍して内容一致を確認。
	ex := t.TempDir()
	if err := ExtractZip(dest, ex); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(ex, "book", "p001.png")); got != "PNGDATA-1" {
		t.Errorf("解凍内容不一致: %q", got)
	}
	if got := readFile(t, filepath.Join(ex, "book", "sub", "p002.jpg")); got != "JPGDATA-2" {
		t.Errorf("解凍内容不一致(sub): %q", got)
	}
}

func TestExtractZipSlip(t *testing.T) {
	// "../evil" を名前に持つ不正な zip を作る。
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("../evil.txt")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("pwned"))
	zw.Close()

	zpath := filepath.Join(t.TempDir(), "evil.zip")
	if err := os.WriteFile(zpath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ExtractZip(zpath, t.TempDir()); err == nil {
		t.Fatal("Zip Slip を検出できていない（エラーになるべき）")
	}
}

func TestFindZipsAndFolders(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.zip"), "z")
	writeFile(t, filepath.Join(root, "b.ZIP"), "z")
	writeFile(t, filepath.Join(root, ".hidden.zip"), "z") // 除外
	writeFile(t, filepath.Join(root, "ch01", "x.png"), "x")
	writeFile(t, filepath.Join(root, "empty", "readme.md"), "x") // 画像なし→除外

	zips, err := FindZips(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(zips) != 2 {
		t.Fatalf("FindZips = %v, want 2 件", zips)
	}

	folders, err := FindImageFolders(root, DefaultExts())
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 1 || filepath.Base(folders[0]) != "ch01" {
		t.Fatalf("FindImageFolders = %v, want [ch01]", folders)
	}
}

func makeZip(t *testing.T, zipPath string, files map[string]string) {
	t.Helper()
	staging := t.TempDir()
	for rel, content := range files {
		writeFile(t, filepath.Join(staging, filepath.FromSlash(rel)), content)
	}
	if err := ZipDir(staging, zipPath, "", DefaultExts()); err != nil {
		t.Fatal(err)
	}
}

func TestRunZipMode_DefaultKeepsOriginal(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "book.zip")
	makeZip(t, zipPath, map[string]string{"book/p001.png": "P1", "book/p002.jpg": "J2"})
	orig, _ := os.Stat(zipPath)

	fr := &fakeResizer{}
	opt := DefaultOptions() // Overwrite=false, Suffix="_resized"
	opt.Resizer = fr

	if err := Run(root, opt); err != nil {
		t.Fatal(err)
	}

	// リサイズは x1600> で解凍画像 2 枚に対して呼ばれた。
	if got := len(fr.allFiles()); got != 2 {
		t.Errorf("リサイズ対象 = %d 枚, want 2", got)
	}
	for _, c := range fr.calls {
		if c.geometry != "x1600>" {
			t.Errorf("geometry = %q, want x1600>", c.geometry)
		}
	}

	// 元 zip は無変更で残り、_resized.zip が新規生成される。
	after, _ := os.Stat(zipPath)
	if after.ModTime() != orig.ModTime() || after.Size() != orig.Size() {
		t.Error("元 zip が書き換わった（残すべき）")
	}
	out := filepath.Join(root, "book_resized.zip")
	ex := t.TempDir()
	if err := ExtractZip(out, ex); err != nil {
		t.Fatalf("book_resized.zip が読めない: %v", err)
	}
	for _, p := range []string{"book/p001.png", "book/p002.jpg"} {
		if _, err := os.Stat(filepath.Join(ex, filepath.FromSlash(p))); err != nil {
			t.Errorf("出力に %q が無い: %v", p, err)
		}
	}
	// 一時ディレクトリが残っていない。
	for _, e := range mustReadDir(t, root) {
		if e.IsDir() {
			t.Errorf("一時ディレクトリが残存: %s", e.Name())
		}
	}
}

func TestRunZipMode_OverwriteReplacesOriginal(t *testing.T) {
	root := t.TempDir()
	makeZip(t, filepath.Join(root, "book.zip"), map[string]string{"book/p001.png": "P1"})

	opt := DefaultOptions()
	opt.Overwrite = true
	opt.Resizer = &fakeResizer{}
	if err := Run(root, opt); err != nil {
		t.Fatal(err)
	}
	for _, e := range mustReadDir(t, root) {
		if e.Name() != "book.zip" {
			t.Errorf("想定外の残存物: %s（上書き時は元名のみ）", e.Name())
		}
	}
}

func TestRunZipMode_SkipsAlreadyResized(t *testing.T) {
	root := t.TempDir()
	// 出力済み(_resized)のみが存在 → 入力対象なしでエラー（二重サフィックス防止）。
	makeZip(t, filepath.Join(root, "book_resized.zip"), map[string]string{"book/p.png": "P"})
	opt := DefaultOptions()
	opt.Resizer = &fakeResizer{}
	if err := Run(root, opt); err == nil {
		t.Fatal("_resized のみのとき対象なしエラーを期待")
	}
}

func TestRunDirMode_DefaultPreservesSourceImages(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "ch01", "a.png")
	b := filepath.Join(root, "ch01", "b.jpg")
	writeFile(t, a, "A")
	writeFile(t, b, "B")

	fr := &fakeResizer{}
	opt := DefaultOptions() // Overwrite=false
	opt.Resizer = fr

	if err := Run(root, opt); err != nil {
		t.Fatal(err)
	}
	if got := len(fr.allFiles()); got != 2 {
		t.Errorf("リサイズ対象 = %d 枚, want 2", got)
	}
	// リサイズは一時コピーに対して行われ、ソース画像のパスは触られていない。
	for _, c := range fr.calls {
		for _, f := range c.files {
			if f == a || f == b {
				t.Errorf("ソース画像が直接リサイズされた: %s", f)
			}
		}
	}
	// ソース画像は元のまま、<folder>.zip は生成。
	if got := readFile(t, a); got != "A" {
		t.Errorf("ソース画像が変化: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "ch01.zip")); err != nil {
		t.Errorf("ch01.zip が生成されていない: %v", err)
	}
}

func TestRun_DryRun_NoSideEffects(t *testing.T) {
	root := t.TempDir()
	staging := t.TempDir()
	writeFile(t, filepath.Join(staging, "p.png"), "P")
	zipPath := filepath.Join(root, "x.zip")
	if err := ZipDir(staging, zipPath, "", DefaultExts()); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(zipPath)

	fr := &fakeResizer{}
	opt := DefaultOptions()
	opt.Resizer = fr
	opt.DryRun = true
	if err := Run(root, opt); err != nil {
		t.Fatal(err)
	}
	if len(fr.calls) != 0 {
		t.Error("dry-run なのに Resizer が呼ばれた")
	}
	after, _ := os.Stat(zipPath)
	if before.ModTime() != after.ModTime() || before.Size() != after.Size() {
		t.Error("dry-run なのに zip が書き換わった")
	}
}

// --- ImageMagick が在るときだけ走る統合テスト ---

func TestMagickResizer_Integration(t *testing.T) {
	if _, err := LookupMagick(); err != nil {
		t.Skip("ImageMagick が無いのでスキップ")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "big.png")
	writePNG(t, p, 1000, 2400) // 高さ2400 → x800> で 800 に縮小されるはず

	if err := (MagickResizer{}).Resize([]string{p}, "x800>", 0, 1); err != nil {
		t.Fatal(err)
	}
	_, h := pngSize(t, p)
	if h != 800 {
		t.Errorf("リサイズ後の高さ = %d, want 800", h)
	}
}

// --- helpers ---

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustReadDir(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func keys(m map[string]uint16) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 0, 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func pngSize(t *testing.T, path string) (int, int) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		// PNG 以外（avif 等）で再エンコードされた場合に備えて image.DecodeConfig も試す。
		f.Seek(0, io.SeekStart)
		ic, _, err2 := image.DecodeConfig(f)
		if err2 != nil {
			t.Fatalf("画像サイズ取得失敗: %v / %v", err, err2)
		}
		return ic.Width, ic.Height
	}
	return cfg.Width, cfg.Height
}
