// Command imgpack は imgpack ライブラリの CLI フロントエンド。
//
// 対象ディレクトリの中身を見て自動でモードを選ぶ:
//   - 直下に *.zip があれば zipモード（解凍→リサイズ→再zip→元zipを上書き）
//   - zip が無ければフォルダモード（各画像フォルダをリサイズし <folder>.zip を生成）
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/reizist/imgpack"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "imgpack: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	opt := imgpack.DefaultOptions()
	var (
		extsCSV string
		zipOnly bool
	)

	flag.IntVar(&opt.Height, "height", opt.Height, "高さの上限(px)。長辺を縦に揃える既定運用。0 で未指定")
	flag.IntVar(&opt.Width, "width", 0, "幅の上限(px)。0 で未指定")
	flag.StringVar(&opt.Geometry, "geometry", "", "ImageMagick の geometry を直接指定(例 1200x1600>)。height/width より優先")
	flag.IntVar(&opt.Quality, "quality", 0, "出力品質 1-100(jpg/avif/webp 等)。0 で magick のデフォルト")
	flag.StringVar(&extsCSV, "ext", strings.Join(imgpack.DefaultExts(), ","), "対象拡張子(カンマ区切り)")
	flag.IntVar(&opt.Jobs, "jobs", opt.Jobs, "並列実行数")
	flag.BoolVar(&opt.Resize, "resize", true, "リサイズを実行する(-resize=false で無効)")
	flag.BoolVar(&opt.Zip, "zip", true, "フォルダモードで zip を生成する(-zip=false で無効)")
	flag.BoolVar(&zipOnly, "zip-only", false, "フォルダモードでリサイズせず zip だけ生成する")
	flag.BoolVar(&opt.Overwrite, "overwrite", false, "元ファイルを上書きする(zip は同名/フォルダはインプレース)。既定は元を残す")
	flag.StringVar(&opt.Suffix, "suffix", opt.Suffix, "上書きしない時に zip 出力へ付ける接尾辞")
	flag.BoolVar(&opt.FromDir, "from-dir", false, "zip があってもフォルダモードを強制する")
	flag.BoolVar(&opt.DryRun, "dry-run", false, "実行せず処理内容だけ表示する")

	flag.Usage = usage
	flag.Parse()

	if extsCSV != "" {
		opt.Exts = imgpack.SplitExts(extsCSV)
	}
	if zipOnly {
		opt.Resize = false
		opt.Zip = true
	}
	// 進捗は標準出力へ。
	opt.Logf = func(format string, args ...interface{}) { fmt.Printf(format, args...) }

	target := "."
	if flag.NArg() > 0 {
		target = flag.Arg(0)
	}
	return imgpack.Run(target, opt)
}

func usage() {
	fmt.Fprintf(os.Stderr, `imgpack - 画像アーカイブ/フォルダを一括リサイズして zip 出力する

使い方:
  imgpack [オプション] [対象ディレクトリ]

既定では元ファイルを残す。対象ディレクトリ(省略時はカレント)を見て自動でモードを選ぶ:
  - 直下に *.zip/*.cbz/*.rar/*.cbr があればアーカイブモード:
      各アーカイブを「解凍 → リサイズ → 同じ構造で zip 出力」。
      既定は元を残して <name>_resized.zip を出力。-overwrite で元を上書き。
      ※ rar/cbr の展開には unrar(無ければ 7z/bsdtar) が必要。出力は常に zip。
      ※ Shift-JIS(日本語Windows製) のエントリ名も UTF-8 へ変換して展開。
  - アーカイブが無ければ フォルダモード:
      直下の各画像フォルダ(または直下の画像)をリサイズし <folder>.zip を生成。
      既定はソース画像を残す(一時コピーを変換)。-overwrite でインプレース変換。

例:
  imgpack ~/Documents/DIR       # 各zipを x1600> でリサイズし <name>_resized.zip を出力(元は保持)
  imgpack -overwrite ~/Documents/DIR  # 元zipを上書き(従来動作)
  imgpack -suffix _1600 .       # 出力接尾辞を変更
  imgpack -height 2000 .        # 高さ上限を 2000px に
  imgpack -quality 85 .         # jpg/avif/webp の品質指定
  imgpack -ext png .            # png だけを対象に
  imgpack -from-dir .           # zipがあってもフォルダモードを強制
  imgpack -dry-run .            # 何をするか確認だけ(実ファイルは変更しない)

オプション:
`)
	flag.PrintDefaults()
}
