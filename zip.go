package imgpack

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/japanese"
)

// ExtractZip は zipPath を destDir へ安全に展開する（Zip Slip 対策込み）。
func ExtractZip(zipPath, destDir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	destAbs, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		// 日本語 Windows 製 zip はエントリ名が Shift-JIS(CP932)で UTF-8 フラグ無しのことが多い。
		// そのままだと macOS 等で不正バイト列となり mkdir 失敗するため UTF-8 へ変換する。
		name := decodeEntryName(f)
		target := filepath.Join(destDir, name)
		// パストラバーサル防止: 展開先が destDir の外を指さないことを保証。
		rel, err := filepath.Rel(destAbs, mustAbs(target))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("不正なパスを含む zip エントリ: %s", name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := extractFile(f, target); err != nil {
			return err
		}
	}
	return nil
}

// decodeEntryName は zip エントリ名を UTF-8 文字列として返す。
// UTF-8 フラグが無く不正バイト列の場合は Shift-JIS(CP932) とみなして変換する。
func decodeEntryName(f *zip.File) string {
	name := f.Name
	if !f.NonUTF8 && utf8.ValidString(name) {
		return name
	}
	if dec, err := japanese.ShiftJIS.NewDecoder().String(name); err == nil && utf8.ValidString(dec) {
		return dec
	}
	return name // 変換不能ならそのまま（後段で失敗し得る）
}

func extractFile(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	mode := f.Mode()
	if mode == 0 {
		mode = 0o644
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ZipDir は srcDir 以下を destZip に梱包する。アーカイブ内パスは prefix を先頭に付ける
// （prefix="" なら srcDir 直下からの相対構造）。隠しファイル(先頭ドット)は除外する。
//
// exts に一致する画像エントリは既に内部圧縮済みなので Store(無圧縮)で格納し、再zipの
// CPU コストを抑える。それ以外は Deflate で圧縮する。
//
// 一時ファイルへ書いてから atomic に差し替えるので、途中失敗で壊れた zip を残さない
// （destZip を上書きする用途でも安全）。
func ZipDir(srcDir, destZip, prefix string, exts []string) (err error) {
	tmp := destZip + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)

	walkErr := filepath.Walk(srcDir, func(path string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if info.IsDir() || strings.HasPrefix(info.Name(), ".") {
			return nil // ディレクトリ自体と .DS_Store 等は出力しない
		}
		rel, rerr := filepath.Rel(srcDir, path)
		if rerr != nil {
			return rerr
		}
		hdr, herr := zip.FileInfoHeader(info)
		if herr != nil {
			return herr
		}
		name := rel
		if prefix != "" {
			name = filepath.Join(prefix, rel)
		}
		hdr.Name = filepath.ToSlash(name)
		if isImage(info.Name(), exts) {
			hdr.Method = zip.Store
		} else {
			hdr.Method = zip.Deflate
		}
		w, cerr := zw.CreateHeader(hdr)
		if cerr != nil {
			return cerr
		}
		src, oerr := os.Open(path)
		if oerr != nil {
			return oerr
		}
		defer src.Close()
		_, ioerr := io.Copy(w, src)
		return ioerr
	})

	cerr := zw.Close()
	ferr := f.Close()
	if e := firstErr(walkErr, cerr, ferr); e != nil {
		os.Remove(tmp)
		return e
	}
	return os.Rename(tmp, destZip)
}

// CountImagesInZip は zip 内の画像エントリ数を、展開せずに数える（dry-run 用）。
func CountImagesInZip(zipPath string, exts []string) (int, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return 0, err
	}
	defer zr.Close()
	n := 0
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if isImage(filepath.Base(f.Name), exts) {
			n++
		}
	}
	return n, nil
}

func mustAbs(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return a
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
