package imgpack

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Options は処理の挙動を制御する設定。ゼロ値ではなく DefaultOptions から作るのが推奨。
type Options struct {
	Height   int      // 高さ上限(px)。0 で未指定
	Width    int      // 幅上限(px)。0 で未指定
	Geometry string   // ImageMagick geometry の直接指定（Height/Width より優先）
	Quality  int      // 出力品質(1-100)。0 で既定
	Exts     []string // 対象拡張子（空なら DefaultExts）
	Jobs     int      // 並列数（0 以下なら NumCPU）
	Resize   bool     // リサイズを実行するか
	Zip      bool     // フォルダモードで zip 生成を実行するか（zip モードでは常に再zip）
	FromDir  bool     // zip があってもフォルダモードを強制する

	// Overwrite が false（既定）なら元ファイルを残す:
	//   zip モード     … 元zipは触らず <name><Suffix>.zip を出力
	//   フォルダモード … ソース画像を破壊せず一時コピーをリサイズして <folder>.zip を出力
	// true なら従来どおり元を上書き（zip は同名、フォルダはインプレースでリサイズ）。
	Overwrite bool
	// Suffix は Overwrite=false の zip モード出力に付ける接尾辞（既定 "_resized"）。
	Suffix string

	// Resizer は nil なら MagickResizer を自動利用する。
	Resizer Resizer
	// RarExtractor は rar/cbr の展開処理。nil なら外部ツール(unrar/7z/bsdtar)を使う。
	RarExtractor func(src, destDir string) error
	// Logf は進捗ログの出力先。nil なら無出力。
	Logf func(format string, args ...interface{})
	// DryRun が true なら、解凍・上書き・リサイズを行わず計画のみログ出力する。
	DryRun bool
}

// DefaultOptions は既定値（高さ1600の縮小、リサイズ＋zip 有効、CPU数並列）を返す。
func DefaultOptions() Options {
	return Options{
		Height: 1600,
		Resize: true,
		Zip:    true,
		Jobs:   runtime.NumCPU(),
		Exts:   DefaultExts(),
		Suffix: "_resized",
	}
}

// resolve は欠けている既定値を埋め、Resizer を解決した複製を返す。
func (o Options) resolve() (Options, error) {
	r := o
	if r.Jobs < 1 {
		r.Jobs = runtime.NumCPU()
	}
	if len(r.Exts) == 0 {
		r.Exts = DefaultExts()
	} else {
		r.Exts = NormalizeExts(r.Exts)
		if len(r.Exts) == 0 {
			return r, fmt.Errorf("対象拡張子が空です")
		}
	}
	if r.Logf == nil {
		r.Logf = func(string, ...interface{}) {}
	}
	if !r.Resize && !r.Zip {
		return r, fmt.Errorf("Resize と Zip の両方が無効です。何も実行されません")
	}
	if !r.Overwrite && strings.TrimSpace(r.Suffix) == "" {
		// 上書きしないのに接尾辞が空だと出力が元と衝突するため既定値を補う。
		r.Suffix = "_resized"
	}
	if r.Resize && r.Resizer == nil && !r.DryRun {
		bin, err := LookupMagick()
		if err != nil {
			return r, err
		}
		r.Resizer = MagickResizer{Bin: bin}
	}
	return r, nil
}

// ResolveGeometry は Height/Width/Geometry から ImageMagick の geometry 文字列を組み立てる。
// 末尾 ">" は「縮小のみ(拡大しない)」を表す。
func (o Options) ResolveGeometry() string {
	if o.Geometry != "" {
		return o.Geometry
	}
	switch {
	case o.Width > 0 && o.Height > 0:
		return fmt.Sprintf("%dx%d>", o.Width, o.Height)
	case o.Height > 0:
		return fmt.Sprintf("x%d>", o.Height)
	case o.Width > 0:
		return fmt.Sprintf("%d>", o.Width)
	default:
		return ""
	}
}

// Run は target ディレクトリの中身を見てモードを自動選択して処理する。
// ・直下に *.zip があれば zip モード（FromDir で無効化）
// ・無ければフォルダモード
func Run(target string, opt Options) error {
	r, err := opt.resolve()
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}

	archives, err := FindArchives(abs)
	if err != nil {
		return err
	}
	if len(archives) > 0 && !r.FromDir {
		return runArchives(archives, r)
	}
	return runFolders(abs, r)
}

func runArchives(archives []string, opt Options) error {
	// 上書きしない場合、過去の出力(<name><Suffix>.zip)を入力から除外して
	// 二重サフィックス(<name><Suffix><Suffix>.zip)を防ぐ。
	if !opt.Overwrite {
		var in []string
		for _, a := range archives {
			if stemHasSuffix(a, opt.Suffix) {
				continue
			}
			in = append(in, a)
		}
		archives = in
		if len(archives) == 0 {
			return fmt.Errorf("処理対象のアーカイブが見つかりません（%q 付きは出力とみなして除外）", opt.Suffix)
		}
	}

	opt.Logf("アーカイブモード: %d 個 (zip/rar) / geometry=%q jobs=%d quality=%s%s\n",
		len(archives), opt.ResolveGeometry(), opt.Jobs, qualityLabel(opt.Quality), dryLabel(opt.DryRun))
	opt.Logf("各アーカイブ: 解凍 → リサイズ → %s\n", zipOutLabel(opt))

	var failures []failure
	for i, a := range archives {
		opt.Logf("\n[%d/%d] %s\n", i+1, len(archives), filepath.Base(a))
		if err := processArchive(a, opt); err != nil {
			failures = append(failures, failure{filepath.Base(a), err})
			opt.Logf("  ! 失敗: %v\n", err)
		}
	}
	opt.Logf("\n完了: %d 成功 / %d 失敗\n", len(archives)-len(failures), len(failures))
	logFailures(opt, failures)
	if len(failures) > 0 {
		return fmt.Errorf("%d 個のアーカイブで失敗しました", len(failures))
	}
	return nil
}

// failure は失敗した 1 件（表示用）。
type failure struct {
	name string
	err  error
}

func logFailures(opt Options, failures []failure) {
	if len(failures) == 0 {
		return
	}
	opt.Logf("\n失敗した %d 件:\n", len(failures))
	for _, f := range failures {
		opt.Logf("  - %s\n", f.name)
	}
}

func runFolders(root string, opt Options) error {
	folders, err := FindImageFolders(root, opt.Exts)
	if err != nil {
		return err
	}
	if len(folders) == 0 {
		return fmt.Errorf("対象の zip も画像フォルダも見つかりません: %s", root)
	}
	opt.Logf("フォルダモード: %d フォルダ / geometry=%q jobs=%d resize=%v zip=%v%s\n",
		len(folders), opt.ResolveGeometry(), opt.Jobs, opt.Resize, opt.Zip, dryLabel(opt.DryRun))

	var failures []failure
	for i, dir := range folders {
		opt.Logf("\n[%d/%d] %s\n", i+1, len(folders), dir)
		if err := processFolder(dir, opt); err != nil {
			failures = append(failures, failure{filepath.Base(dir), err})
			opt.Logf("  ! 失敗: %v\n", err)
		}
	}
	opt.Logf("\n完了: %d 成功 / %d 失敗\n", len(folders)-len(failures), len(failures))
	logFailures(opt, failures)
	if len(failures) > 0 {
		return fmt.Errorf("%d フォルダで失敗しました", len(failures))
	}
	return nil
}

// ProcessZip は 1 つのアーカイブ(zip/cbz/rar/cbr)を「解凍 → リサイズ → zip出力」する。
// 互換のため名前は ProcessZip のままだが rar も扱える。新規コードは ProcessArchive を推奨。
func ProcessZip(zipPath string, opt Options) error { return ProcessArchive(zipPath, opt) }

// ProcessArchive は 1 つのアーカイブ(zip/cbz/rar/cbr)を「解凍 → リサイズ → zip出力」する。
func ProcessArchive(path string, opt Options) error {
	r, err := opt.resolve()
	if err != nil {
		return err
	}
	return processArchive(path, r)
}

// archiveOutPath は出力 zip のパスを決める。出力は常に zip。
//   Overwrite=false … <stem><Suffix>.zip（元は残す）
//   Overwrite=true  … zip系は同名で上書き / rar系は <stem>.zip（元rarは別途削除）
func archiveOutPath(path string, opt Options) string {
	stem := archiveStem(path)
	if opt.Overwrite {
		if isZipArchive(filepath.Base(path)) {
			return path
		}
		return stem + ".zip"
	}
	return stem + opt.Suffix + ".zip"
}

func processArchive(path string, opt Options) error {
	dest := archiveOutPath(path, opt)
	isRar := isRarArchive(filepath.Base(path))

	if opt.DryRun {
		n := countImagesInArchive(path, opt)
		opt.Logf("  - 画像 %s 枚をリサイズ(geometry=%q) → %s\n",
			countLabel(n), opt.ResolveGeometry(), zipDestLabel(path, dest, opt.Overwrite))
		return nil
	}

	// 同一ファイルシステム上に一時展開先を作る（最後の rename を atomic にするため）。
	tmpDir, err := os.MkdirTemp(filepath.Dir(path),
		".imgpack-"+filepath.Base(archiveStem(path))+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	opt.Logf("  - 解凍中…\n")
	if err := extractArchive(path, tmpDir, opt); err != nil {
		return fmt.Errorf("解凍失敗: %w", err)
	}

	if opt.Resize {
		files := listImagesRecursive(tmpDir, opt.Exts)
		if len(files) == 0 {
			opt.Logf("  - 画像なし。リサイズskip\n")
		} else {
			opt.Logf("  - リサイズ %d 枚 (geometry=%q)\n", len(files), opt.ResolveGeometry())
			if err := opt.Resizer.Resize(files, opt.ResolveGeometry(), opt.Quality, opt.Jobs); err != nil {
				return err
			}
		}
	}

	// 元と同じ内部構造（prefix 無し）で zip 出力。
	opt.Logf("  - zip出力 → %s\n", zipDestLabel(path, dest, opt.Overwrite))
	if err := ZipDir(tmpDir, dest, "", opt.Exts); err != nil {
		return err
	}

	// 上書き指定で rar を zip 化した場合、元 rar は別名(.zip)になるため元ファイルを削除して置き換える。
	if opt.Overwrite && isRar && dest != path {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("元 rar の削除に失敗: %w", err)
		}
		opt.Logf("  - 元 %s を削除(zip化)\n", filepath.Base(path))
	}
	return nil
}

// ProcessFolder はフォルダ内の画像をインプレースでリサイズし <dir>.zip を生成する。
func ProcessFolder(dir string, opt Options) error {
	r, err := opt.resolve()
	if err != nil {
		return err
	}
	return processFolder(dir, r)
}

func processFolder(dir string, opt Options) error {
	if opt.DryRun {
		if opt.Resize {
			n := len(listImages(dir, opt.Exts))
			opt.Logf("  - リサイズ %d 枚 (geometry=%q)%s\n", n, opt.ResolveGeometry(), inplaceLabel(opt.Overwrite))
		}
		if opt.Zip {
			opt.Logf("  - zip 生成 %s\n", filepath.Base(dir)+".zip")
		}
		return nil
	}

	// リサイズ対象ディレクトリ。Overwrite=false なら一時コピーを作り、
	// ソース画像を破壊せずにそちらをリサイズして zip にする。
	src := dir
	if opt.Resize && !opt.Overwrite {
		tmp, err := os.MkdirTemp(filepath.Dir(dir), ".imgpack-"+filepath.Base(dir)+"-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		if err := copyTree(dir, tmp); err != nil {
			return err
		}
		src = tmp
	}

	if opt.Resize {
		files := listImages(src, opt.Exts)
		if len(files) == 0 {
			opt.Logf("  - 画像なし。リサイズskip\n")
		} else {
			opt.Logf("  - リサイズ %d 枚 (geometry=%q)%s\n", len(files), opt.ResolveGeometry(), inplaceLabel(opt.Overwrite))
			if err := opt.Resizer.Resize(files, opt.ResolveGeometry(), opt.Quality, opt.Jobs); err != nil {
				return err
			}
		}
	}
	if opt.Zip {
		zipPath := dir + ".zip"
		opt.Logf("  - zip 生成 %s\n", filepath.Base(zipPath))
		if err := ZipDir(src, zipPath, filepath.Base(dir), opt.Exts); err != nil {
			return err
		}
	}
	return nil
}

// FindArchives は root 直下のアーカイブ(.zip/.cbz/.rar/.cbr)を返す（隠しファイルは除外）。
func FindArchives(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if isArchive(e.Name()) {
			out = append(out, filepath.Join(root, e.Name()))
		}
	}
	return sortedStrings(out), nil
}

// FindZips は root 直下の zip 系アーカイブ(.zip/.cbz)を返す（隠しファイルは除外）。
func FindZips(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var zips []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if isZipArchive(e.Name()) {
			zips = append(zips, filepath.Join(root, e.Name()))
		}
	}
	return sortedStrings(zips), nil
}

// FindImageFolders は root から処理すべきフォルダ一覧を返す。
// ・直下のサブディレクトリに画像があればそれらを対象にする
// ・サブディレクトリに画像が無く、root 直下に画像があれば root 自身を対象にする
func FindImageFolders(root string, exts []string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var folders []string
	rootHasImages := false
	for _, e := range entries {
		if e.IsDir() {
			if dirHasImages(filepath.Join(root, e.Name()), exts) {
				folders = append(folders, filepath.Join(root, e.Name()))
			}
		} else if isImage(e.Name(), exts) {
			rootHasImages = true
		}
	}
	if len(folders) == 0 && rootHasImages {
		folders = append(folders, root)
	}
	return sortedStrings(folders), nil
}

func dirHasImages(dir string, exts []string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && isImage(e.Name(), exts) {
			return true
		}
	}
	return false
}

func listImages(dir string, exts []string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && isImage(e.Name(), exts) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return sortedStrings(files)
}

// listImagesRecursive は dir 以下を再帰的に走査して画像ファイルを集める
// （zip 内が flat でも folder/ 始まりでも拾えるように）。
func listImagesRecursive(dir string, exts []string) []string {
	var files []string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if isImage(info.Name(), exts) {
			files = append(files, path)
		}
		return nil
	})
	return sortedStrings(files)
}

// withSuffix は zip パスの拡張子前に suffix を挿入する（book.zip + _resized → book_resized.zip）。
func withSuffix(zipPath, suffix string) string {
	ext := filepath.Ext(zipPath)
	return strings.TrimSuffix(zipPath, ext) + suffix + ext
}

// stemHasSuffix は zip の拡張子を除いた名前が suffix で終わるか（過去の出力か）を返す。
func stemHasSuffix(zipPath, suffix string) bool {
	if suffix == "" {
		return false
	}
	ext := filepath.Ext(zipPath)
	return strings.HasSuffix(strings.TrimSuffix(zipPath, ext), suffix)
}

func zipOutLabel(opt Options) string {
	if opt.Overwrite {
		return "再zip(元を上書き)"
	}
	return "別名で出力(元を保持: <name>" + opt.Suffix + ".zip)"
}

func zipDestLabel(srcZip, dest string, overwrite bool) string {
	if overwrite {
		return filepath.Base(dest) + " (上書き)"
	}
	return filepath.Base(dest) + " (元 " + filepath.Base(srcZip) + " は保持)"
}

func inplaceLabel(overwrite bool) string {
	if overwrite {
		return " [インプレース]"
	}
	return " [元を保持]"
}

func countLabel(n int) string {
	if n < 0 {
		return "?"
	}
	return fmt.Sprintf("%d", n)
}

func dryLabel(dry bool) string {
	if dry {
		return " [dry-run]"
	}
	return ""
}

func qualityLabel(q int) string {
	if q <= 0 {
		return "default"
	}
	return fmt.Sprintf("%d", q)
}
