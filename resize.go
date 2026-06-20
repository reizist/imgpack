package imgpack

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Resizer は画像ファイル群をインプレースでリサイズする処理を抽象化する。
// 既定の実装は MagickResizer。テストや別エンジン利用のために差し替えられる。
type Resizer interface {
	// Resize は files をインプレースで geometry にリサイズする。
	// quality が 0 より大きければ出力品質として渡す。jobs は並列度のヒント。
	Resize(files []string, geometry string, quality, jobs int) error
}

// ErrMagickNotFound は ImageMagick(magick/mogrify)が見つからない場合に返る。
var ErrMagickNotFound = errors.New("ImageMagick が見つかりません(magick/mogrify)。`brew install imagemagick` 等で導入してください")

// LookupMagick は ImageMagick 実行ファイル(IM7: magick / IM6: mogrify)を探す。
func LookupMagick() (string, error) {
	if p, err := exec.LookPath("magick"); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("mogrify"); err == nil {
		return p, nil
	}
	return "", ErrMagickNotFound
}

// MagickResizer は ImageMagick の mogrify を呼び出す Resizer 実装。
type MagickResizer struct {
	// Bin は実行ファイルパス。空なら LookupMagick で自動解決する。
	Bin string
}

// Resize は files を jobs 個のチャンクに分割し、各チャンクを 1 回の mogrify 呼び出しで
// 並列にインプレース変換する。
func (m MagickResizer) Resize(files []string, geometry string, quality, jobs int) error {
	if len(files) == 0 {
		return nil
	}
	bin := m.Bin
	if bin == "" {
		var err error
		if bin, err = LookupMagick(); err != nil {
			return err
		}
	}
	if jobs < 1 {
		jobs = 1
	}

	chunks := splitChunks(files, jobs)
	var wg sync.WaitGroup
	errCh := make(chan error, len(chunks))
	for _, chunk := range chunks {
		if len(chunk) == 0 {
			continue
		}
		wg.Add(1)
		go func(batch []string) {
			defer wg.Done()
			if err := runMogrify(bin, batch, geometry, quality); err != nil {
				errCh <- err
			}
		}(chunk)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func runMogrify(bin string, files []string, geometry string, quality int) error {
	var args []string
	if strings.HasSuffix(filepath.Base(bin), "magick") {
		args = append(args, "mogrify") // IM7: `magick mogrify ...`
	}
	if geometry != "" {
		args = append(args, "-resize", geometry)
	}
	if quality > 0 {
		args = append(args, "-quality", fmt.Sprintf("%d", quality))
	}
	args = append(args, files...)

	cmd := exec.Command(bin, args...)
	// プロセス側で jobs 並列にしているので、各 mogrify は 1 スレッドに固定する。
	// ImageMagick の OpenMP による内部マルチスレッドとの二重並列(コア数超過の
	// スレッド競合)を防ぎ、全体スループットを上げる。
	cmd.Env = append(os.Environ(), "MAGICK_THREAD_LIMIT=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mogrify 失敗: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
