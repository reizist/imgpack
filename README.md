# imgpack

画像を**形式を自動判別して一括リサイズ**し、zip へ（再）梱包する Go ライブラリ＆CLI。
対象ディレクトリの中身を見て、2 つのモードを自動で切り替える。

| モード | 入力 | 動作 | 残るもの |
| --- | --- | --- | --- |
| **zipモード** | 直下に `*.zip` | 各zipを **解凍 → リサイズ → 同じ構造で再zip → 元zipを上書き** | 更新後のzipのみ |
| **フォルダモード** | zip無し・画像フォルダ | 各フォルダをインプレースでリサイズし `<folder>.zip` を生成 | フォルダ + zip |

zipモードの解凍は一時フォルダで行い、処理後に破棄する（中間の展開物は残さない）。

これまで手動でやっていた以下を 1 コマンドに集約する:

```sh
# Before（zipを解凍 → 形式ごとに打ち分けてリサイズ → 再zip…を手作業）
unzip foo.zip
for d in ./*/; do ( cd "$d" && mogrify -resize 'x1600>' *.png ) done
for d in ./*/; do ( cd "$d" && mogrify -resize 'x1600>' *.jpg ) done
find . \! -name '*.zip' \! -name '.' -type d -exec zip -r {}.zip {} \;

# After（zipの入ったフォルダを指定するだけ）
imgpack ~/Downloads/a
```

## 特徴

- 形式は png/jpg/jpeg/avif/webp/gif/heic… を**自動検出**（拡張子ごとの打ち分け不要）
- リサイズは ImageMagick(`magick mogrify`) に委譲 → ビルド済み IM が対応する全形式をそのまま処理（avif/heic 含む）
- リサイズは `-jobs` 並列（mogrify は各プロセス 1 スレッド固定で過剰スレッド競合を回避）
- 画像は既に内部圧縮済みなので再zip時は **Store(無圧縮)** で格納し、無駄な Deflate を回避（サイズほぼ不変・CPU 大幅減）
- zip内が `foo/…`（フォルダ始まり）でも画像が直下（flat）でも、**元の内部構造を保持**して再梱包
- zip の生成・解凍は Go 標準ライブラリ（外部 `zip`/`unzip` 不要、Zip Slip 対策込み）
- `.DS_Store` 等の隠しファイルは自動除外
- 途中失敗で壊れた zip を残さない（`.tmp` に書いて atomic に差し替え。上書きでも安全）

## 前提

- ImageMagick（`magick` または `mogrify`）がインストール済みであること
  - 例: `brew install imagemagick`
  - ※ リサイズを差し替える（`Resizer` 実装を渡す）場合は不要

## インストール（CLI）

```sh
# リモートから
go install github.com/reizist/imgpack/cmd/imgpack@latest

# ローカルから
cd ~/go/src/github.com/reizist/imgpack
go install ./cmd/imgpack        # ~/go/bin/imgpack
```

`~/go/bin` に PATH を通しておくと、どこからでも `imgpack` で呼べる:

```sh
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc
```

## CLI の使い方

```sh
imgpack ~/Downloads/a         # a 内の各zipを解凍→x1600>でリサイズ→再zip(上書き)
imgpack                       # カレントを対象
imgpack -height 2000 .        # 高さ上限 2000px
imgpack -width 1200 .         # 幅上限 1200px
imgpack -geometry 1200x1600\> .  # geometry を直接指定（height/width より優先）
imgpack -quality 85 .         # jpg/avif/webp の品質指定
imgpack -ext png .            # png だけを対象に
imgpack -from-dir .           # zipがあってもフォルダモードを強制
imgpack -dry-run .            # 実行せず計画だけ表示（解凍も上書きもしない）
```

geometry 末尾の `>` は「縮小のみ（指定より小さい画像は拡大しない）」。既定は `x1600>`。

### オプション

| フラグ | 既定 | 説明 |
| --- | --- | --- |
| `-height` | `1600` | 高さの上限(px)。`0` で未指定 |
| `-width` | `0` | 幅の上限(px)。`0` で未指定 |
| `-geometry` | – | ImageMagick geometry を直接指定（height/width より優先） |
| `-quality` | `0` | 出力品質 1-100（`0` で IM デフォルト） |
| `-ext` | `png,jpg,jpeg,avif,...` | 対象拡張子（カンマ区切り） |
| `-jobs` | CPU 数 | mogrify の並列数 |
| `-resize` | `true` | リサイズ実行（`-resize=false` で無効） |
| `-zip` | `true` | フォルダモードで zip 生成（`-zip=false` で無効） |
| `-zip-only` | `false` | フォルダモードでリサイズせず zip だけ |
| `-from-dir` | `false` | zip があってもフォルダモードを強制 |
| `-dry-run` | `false` | 実行せず計画表示 |

## ライブラリとして使う

```go
import "github.com/reizist/imgpack"

opt := imgpack.DefaultOptions() // Height:1600, Resize:true, Zip:true, Jobs:NumCPU
opt.Height = 1600
opt.Logf = func(f string, a ...interface{}) { fmt.Printf(f, a...) } // 進捗ログ（任意）

// 対象ディレクトリを自動モード判定で処理
if err := imgpack.Run("/path/to/dir", opt); err != nil {
    log.Fatal(err)
}

// 個別 API
_ = imgpack.ProcessZip("/path/to/foo.zip", opt)   // 解凍→リサイズ→再zip(上書き)
_ = imgpack.ProcessFolder("/path/to/folder", opt) // インプレースリサイズ→<folder>.zip
```

### Resizer の差し替え

リサイズ処理は `Resizer` インターフェースに委譲される。既定は ImageMagick を呼ぶ
`MagickResizer`。テストや別エンジン利用のために差し替えられる:

```go
type Resizer interface {
    Resize(files []string, geometry string, quality, jobs int) error
}

opt := imgpack.DefaultOptions()
opt.Resizer = myResizer // ImageMagick 非依存にできる
```

おもな公開 API: `Run` / `ProcessZip` / `ProcessFolder` / `FindZips` / `FindImageFolders` /
`ExtractZip` / `ZipDir` / `CountImagesInZip` / `Options` / `DefaultOptions` / `Resizer` /
`MagickResizer` / `LookupMagick` / `DefaultExts` / `NormalizeExts`。

## 開発

```sh
go test ./... -cover   # ImageMagick が無い環境でもコアロジックはテスト可能
                       # (magick があれば実リサイズの統合テストも実行)
go vet ./...
gofmt -l .
```

## 構成

```
imgpack/                 ライブラリ (package imgpack) = import path
  doc.go                 パッケージドキュメント
  imgpack.go             Options / Run / ProcessZip / ProcessFolder / 探索
  resize.go              Resizer インターフェース / MagickResizer
  zip.go                 ExtractZip / ZipDir / CountImagesInZip
  util.go                拡張子・画像判定・分割などの補助
  imgpack_test.go        テスト
  cmd/imgpack/main.go    CLI フロントエンド (package main)
```

## 注意

- **zipモードは元zipを上書きする**（軽量化されたものに差し替え）。原本を残したい場合は事前にバックアップを。
- リサイズはインプレース（zipモードでは一時展開先、フォルダモードでは元フォルダ）。
- `-height`/`-width` 併用時は `WxH>`（両辺の上限）になる。

## License

[MIT](./LICENSE)
