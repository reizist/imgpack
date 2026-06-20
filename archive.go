package imgpack

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// アーカイブ拡張子（小文字・ドット無し）。
//   zip 系: そのまま archive/zip で読める（.cbz は zip と同一フォーマット）
//   rar 系: 外部ツール(unrar/7z/bsdtar)で展開する（rar は作成できないため出力は常に zip）
var (
	zipArchiveExts = []string{"zip", "cbz"}
	rarArchiveExts = []string{"rar", "cbr"}
)

// ErrNoRarTool は rar を展開できる外部ツールが無い場合に返る。
var ErrNoRarTool = errors.New("rar を展開できるツールが見つかりません(unrar/7z/bsdtar)。`brew install unrar` 等で導入してください")

func extInSet(name string, set []string) bool {
	if strings.HasPrefix(name, ".") {
		return false
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	for _, e := range set {
		if ext == e {
			return true
		}
	}
	return false
}

func isZipArchive(name string) bool { return extInSet(name, zipArchiveExts) }
func isRarArchive(name string) bool { return extInSet(name, rarArchiveExts) }
func isArchive(name string) bool    { return isZipArchive(name) || isRarArchive(name) }

// archiveStem はアーカイブパスから拡張子を除いたパスを返す（/a/comic.rar → /a/comic）。
func archiveStem(path string) string {
	return strings.TrimSuffix(path, filepath.Ext(path))
}

// extractArchive は拡張子に応じて zip / rar を tmp へ展開する。
func extractArchive(path, dest string, opt Options) error {
	if isRarArchive(filepath.Base(path)) {
		if opt.RarExtractor != nil {
			return opt.RarExtractor(path, dest)
		}
		return defaultRarExtract(path, dest)
	}
	// zip / cbz は標準ライブラリで安全に展開。
	return ExtractZip(path, dest)
}

// LookupRarTool は利用可能な rar 展開ツール名を返す。
func LookupRarTool() (string, error) {
	for _, t := range []string{"unrar", "7z", "7zz", "bsdtar"} {
		if _, err := exec.LookPath(t); err == nil {
			return t, nil
		}
	}
	return "", ErrNoRarTool
}

// defaultRarExtract は外部ツールで src を dest へ展開する（unrar→7z→bsdtar の順）。
func defaultRarExtract(src, dest string) error {
	if p, err := exec.LookPath("unrar"); err == nil {
		// x: フルパス展開 / -o+: 上書き許可 / -inul: 出力抑制。末尾はディレクトリ指定。
		return runExtract(p, "x", "-o+", "-inul", src, dest+string(os.PathSeparator))
	}
	for _, sevenz := range []string{"7z", "7zz"} {
		if p, err := exec.LookPath(sevenz); err == nil {
			return runExtract(p, "x", "-y", "-bso0", "-bsp0", "-o"+dest, src)
		}
	}
	if p, err := exec.LookPath("bsdtar"); err == nil {
		return runExtract(p, "-x", "-f", src, "-C", dest)
	}
	return ErrNoRarTool
}

func runExtract(bin string, args ...string) error {
	cmd := exec.Command(bin, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s 展開失敗: %v\n%s", filepath.Base(bin), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// countImagesInArchive はアーカイブ内の画像数を展開せずに数える（dry-run 用）。
// 数えられない場合は -1 を返す。
func countImagesInArchive(path string, opt Options) int {
	if isRarArchive(filepath.Base(path)) {
		names, err := listRarNames(path)
		if err != nil {
			return -1
		}
		n := 0
		for _, name := range names {
			if isImage(filepath.Base(name), opt.Exts) {
				n++
			}
		}
		return n
	}
	n, err := CountImagesInZip(path, opt.Exts)
	if err != nil {
		return -1
	}
	return n
}

// listRarNames は rar 内のエントリ名一覧を返す（unrar lb を使用）。
func listRarNames(path string) ([]string, error) {
	p, err := exec.LookPath("unrar")
	if err != nil {
		return nil, err
	}
	out, err := exec.Command(p, "lb", path).Output()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			names = append(names, s)
		}
	}
	return names, nil
}
