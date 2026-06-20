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

	// Resizer は nil なら MagickResizer を自動利用する。
	Resizer Resizer
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

	zips, err := FindZips(abs)
	if err != nil {
		return err
	}
	if len(zips) > 0 && !r.FromDir {
		return runZips(zips, r)
	}
	return runFolders(abs, r)
}

func runZips(zips []string, opt Options) error {
	opt.Logf("zipモード: %d 個の zip / geometry=%q jobs=%d quality=%s%s\n",
		len(zips), opt.ResolveGeometry(), opt.Jobs, qualityLabel(opt.Quality), dryLabel(opt.DryRun))
	opt.Logf("各zip: 解凍 → リサイズ → 再zip(上書き)\n")

	var failed int
	for i, z := range zips {
		opt.Logf("\n[%d/%d] %s\n", i+1, len(zips), filepath.Base(z))
		if err := processZip(z, opt); err != nil {
			failed++
			opt.Logf("  ! 失敗: %v\n", err)
		}
	}
	opt.Logf("\n完了: %d 成功 / %d 失敗\n", len(zips)-failed, failed)
	if failed > 0 {
		return fmt.Errorf("%d 個の zip で失敗しました", failed)
	}
	return nil
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

	var failed int
	for i, dir := range folders {
		opt.Logf("\n[%d/%d] %s\n", i+1, len(folders), dir)
		if err := processFolder(dir, opt); err != nil {
			failed++
			opt.Logf("  ! 失敗: %v\n", err)
		}
	}
	opt.Logf("\n完了: %d 成功 / %d 失敗\n", len(folders)-failed, failed)
	if failed > 0 {
		return fmt.Errorf("%d フォルダで失敗しました", failed)
	}
	return nil
}

// ProcessZip は 1 つの zip を「解凍 → リサイズ → 再zip(上書き)」する。
func ProcessZip(zipPath string, opt Options) error {
	r, err := opt.resolve()
	if err != nil {
		return err
	}
	return processZip(zipPath, r)
}

func processZip(zipPath string, opt Options) error {
	if opt.DryRun {
		n, err := CountImagesInZip(zipPath, opt.Exts)
		if err != nil {
			return err
		}
		opt.Logf("  - 画像 %d 枚をリサイズ(geometry=%q) → %s を上書き\n",
			n, opt.ResolveGeometry(), filepath.Base(zipPath))
		return nil
	}

	// 同一ファイルシステム上に一時展開先を作る（最後の rename を atomic にするため）。
	tmpDir, err := os.MkdirTemp(filepath.Dir(zipPath),
		".imgpack-"+strings.TrimSuffix(filepath.Base(zipPath), ".zip")+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	opt.Logf("  - 解凍中…\n")
	if err := ExtractZip(zipPath, tmpDir); err != nil {
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

	// 元と同じ内部構造（prefix 無し）で再zipし、上書き。
	opt.Logf("  - 再zip(上書き) %s\n", filepath.Base(zipPath))
	return ZipDir(tmpDir, zipPath, "", opt.Exts)
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
	if opt.Resize {
		files := listImages(dir, opt.Exts)
		if len(files) == 0 {
			opt.Logf("  - 画像なし。リサイズskip\n")
		} else {
			opt.Logf("  - リサイズ %d 枚 (geometry=%q)\n", len(files), opt.ResolveGeometry())
			if !opt.DryRun {
				if err := opt.Resizer.Resize(files, opt.ResolveGeometry(), opt.Quality, opt.Jobs); err != nil {
					return err
				}
			}
		}
	}
	if opt.Zip {
		zipPath := dir + ".zip"
		opt.Logf("  - zip 生成 %s\n", filepath.Base(zipPath))
		if !opt.DryRun {
			if err := ZipDir(dir, zipPath, filepath.Base(dir), opt.Exts); err != nil {
				return err
			}
		}
	}
	return nil
}

// FindZips は root 直下の *.zip を返す（隠しファイルは除外）。
func FindZips(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var zips []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if strings.EqualFold(filepath.Ext(e.Name()), ".zip") {
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
