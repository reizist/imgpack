# imgpack

フォルダや zip の中の画像を、形式（png/jpg/avif/…）を問わず一括でリサイズして zip に戻す CLI

zip / cbz / rar / cbr を解凍 →（形式問わず）リサイズ → 同じ構造で zip 出力、までやる。
デフォルトは元ファイルを残し、`<name>_resized.zip` を別名で出力する（`-overwrite` で元を上書き）。
リサイズ自体は ImageMagick（`magick mogrify`）に投げているので、IM が読める形式はそのまま扱える。

- 入力: `.zip` / `.cbz`（= zip）, `.rar` / `.cbr`（rar）。出力は常に zip（rar は作成できないため）。
- rar/cbr の展開には `unrar`（無ければ `7z` / `bsdtar`）が必要。
- 日本語 Windows 製 zip にありがちな Shift-JIS のファイル名も UTF-8 に変換して展開する。

## USAGE

```sh
# インストール（要 ImageMagick: brew install imagemagick / rar入力には: brew install unrar）
go install github.com/reizist/imgpack/cmd/imgpack@latest

# アーカイブの入ったフォルダを渡す → 各 zip/rar を解凍・リサイズし <name>_resized.zip を出力（元は残す）
imgpack ~/Documents/DIR

# 元 zip を上書きしたいとき
imgpack -overwrite ~/Documents/DIR

# 高さ上限を変える
imgpack -height 2000 ~/Documents/DIR

# png だけ対象
imgpack -ext png ~/Documents/DIR

# 何が起きるか確認だけ（実ファイルは変更しない）
imgpack -dry-run ~/Documents/DIR
```

対象ディレクトリを省略した場合カレントを対象にする。

- 直下に `*.zip`/`*.cbz`/`*.rar`/`*.cbr` がある → 各アーカイブを解凍 → リサイズ → zip 出力。展開物は残さない。
  既定は元を残して `<name>_resized.zip` を出力（`-overwrite` で元を上書き。rar は zip 化して元を置換）。
- アーカイブが無く画像フォルダがある → 各フォルダをリサイズして `<folder>.zip` を作る。
  既定はソース画像を残す（一時コピーを変換）。`-overwrite` でその場（インプレース）変換。

ライブラリとしても使える:

```go
import "github.com/reizist/imgpack"

opt := imgpack.DefaultOptions()
opt.Height = 1600
imgpack.Run("/path/to/dir", opt)
```

## OPTIONS

| フラグ | デフォルト | 説明 |
| --- | --- | --- |
| `-height` | `1600` | 高さの上限(px)。`0` で未指定 |
| `-width` | `0` | 幅の上限(px)。`0` で未指定 |
| `-geometry` | （空） | ImageMagick の geometry を直接指定（例 `1200x1600>`）。`-height`/`-width` より優先 |
| `-quality` | `0` | 出力品質 1-100（jpg/avif/webp 等）。`0` で IM のデフォルト |
| `-ext` | `png,jpg,jpeg,avif,webp,gif,heic,heif,bmp,tiff,tif` | 対象拡張子（カンマ区切り） |
| `-jobs` | CPU 数 | リサイズの並列数 |
| `-resize` | `true` | リサイズする（`-resize=false` で無効） |
| `-zip` | `true` | フォルダモードで zip を生成（`-zip=false` で無効） |
| `-zip-only` | `false` | フォルダモードでリサイズせず zip だけ |
| `-overwrite` | `false` | 元ファイルを上書き（zip は同名／フォルダはインプレース）。既定は元を残す |
| `-suffix` | `_resized` | 上書きしない時に zip 出力へ付ける接尾辞 |
| `-from-dir` | `false` | zip があってもフォルダモードを強制 |
| `-silent` | `false` | 進捗・ファイル名などの標準出力を抑制（エラーのみ stderr） |
| `-dry-run` | `false` | 実行せず内容だけ表示 |

geometry 末尾の `>` は「縮小のみ（指定より小さい画像は拡大しない）」。デフォルトは `x1600>`。

> 既定では元ファイルを残す（zip は `<name>_resized.zip` を別名出力、フォルダはソース画像を保持）。
> `-overwrite` を付けると元を上書きするので注意。

## License

[MIT](./LICENSE)
