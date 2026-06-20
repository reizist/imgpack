package imgpack

import (
	"path/filepath"
	"sort"
	"strings"
)

// defaultExts は既定で処理対象とする画像拡張子（小文字・ドット無し）。
var defaultExts = []string{"png", "jpg", "jpeg", "avif", "webp", "gif", "heic", "heif", "bmp", "tiff", "tif"}

// DefaultExts は既定の対象拡張子のコピーを返す。
func DefaultExts() []string {
	out := make([]string, len(defaultExts))
	copy(out, defaultExts)
	return out
}

// NormalizeExts は拡張子リストを正規化する（小文字化・先頭ドット除去・空要素と重複の除去）。
func NormalizeExts(list []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range list {
		e = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(e, ".")))
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}

// SplitExts はカンマ区切り文字列を正規化済みの拡張子リストにする（CLI 入力用）。
func SplitExts(csv string) []string {
	return NormalizeExts(strings.Split(csv, ","))
}

// isImage は name が exts に含まれる画像かを返す。隠しファイル（先頭ドット）は対象外。
func isImage(name string, exts []string) bool {
	if strings.HasPrefix(name, ".") {
		return false
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	for _, e := range exts {
		if ext == e {
			return true
		}
	}
	return false
}

// splitChunks は files を最大 n 個のチャンクへラウンドロビンで分割する。
func splitChunks(files []string, n int) [][]string {
	if n < 1 {
		n = 1
	}
	if n > len(files) {
		n = len(files)
	}
	if n == 0 {
		return nil
	}
	chunks := make([][]string, n)
	for i, f := range files {
		chunks[i%n] = append(chunks[i%n], f)
	}
	return chunks
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
