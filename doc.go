// Package imgpack は、画像フォルダ／zip 内の画像を形式（png/jpg/jpeg/avif/webp/gif など）を
// 自動判別して一括リサイズし、zip へ（再）梱包するための機能を提供する。
//
// 2 つの処理形態がある:
//
//	(1) zip モード(ProcessZip): zip を「解凍 → リサイズ → 同じ内部構造で再zip → 元zipを上書き」する。
//	    解凍は一時ディレクトリで行い、処理後に破棄する。
//
//	(2) フォルダモード(ProcessFolder): フォルダ内の画像をインプレースでリサイズし <folder>.zip を生成する。
//
// Run は対象ディレクトリの中身を見て上記モードを自動選択する。
//
// リサイズ処理は Resizer インターフェースに委譲される。既定の実装 MagickResizer は
// ImageMagick(`magick mogrify`) を呼び出すため、ビルド済みの ImageMagick が対応する
// 全形式（avif/heic 含む）をそのまま扱える。Resizer を差し替えれば ImageMagick 非依存で
// 単体テストや別エンジンへの置き換えが可能。
//
// zip の生成・解凍は標準ライブラリ(archive/zip)で行うので外部コマンドは不要。
package imgpack
