# imgpack

フォルダや zip の中の画像を、形式（png/jpg/avif/…）を問わず一括でリサイズして zip に戻す CLI

zip を指定すると「解凍 → リサイズ → 同じ構造で再 zip（元 zip を上書き）」までやる。
リサイズ自体は ImageMagick（`magick mogrify`）に投げているので、IM が読める形式はそのまま扱える。

## USAGE

```sh
# インストール（要 ImageMagick: brew install imagemagick）
go install github.com/reizist/imgpack/cmd/imgpack@latest

# zip の入ったフォルダを渡す → 各 zip を解凍・リサイズ・再 zip（上書き）
imgpack ~/Documents/DIR

# 高さ上限を変える
imgpack -height 2000 ~/Documents/DIR

# png だけ対象
imgpack -ext png ~/Documents/DIR

# 何が起きるか確認だけ（解凍も上書きもしない）
imgpack -dry-run ~/Documents/DIR
```

対象ディレクトリを省略した場合カレントを対象にする。

- 直下に `*.zip` がある → 各 zip を解凍 → リサイズ → 再 zip（上書き）。展開物は残さない。
- zip が無く画像フォルダがある → 各フォルダをその場でリサイズして `<folder>.zip` を作る。

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
| `-from-dir` | `false` | zip があってもフォルダモードを強制 |
| `-dry-run` | `false` | 実行せず内容だけ表示 |

geometry 末尾の `>` は「縮小のみ（指定より小さい画像は拡大しない）」。デフォルトは `x1600>`。

> zip モードは元 zip を上書きする。原本を残したいときは先にバックアップを。

## License

[MIT](./LICENSE)
