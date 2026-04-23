# cockpit-cronplus

**Cockpit 用ビジュアル cron タスクマネージャー** — 従来の crontab を置き換え、秒単位のスケジューリング、タイムアウト制御、自動リトライ、同時実行制限、実行ログ、多言語 UI を提供。すぐに使えます。

> 🌐 [中文](README.md) · [English](README_en.md) · [한국어](README_ko.md) · [Français](README_fr.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Русский](README_ru.md) · [Português](README_pt-BR.md)

## 機能

- 秒単位の cron スケジューリング（6 フィールド：秒/分/時/日/月/曜日）
- タイムアウト時の自動終了、失敗時の自動リトライ、同時実行制御
- タスクごとの環境変数、作業ディレクトリ、ログ保持設定
- Cockpit Web UI：タスクエディタ、スケジュールプリセット、次回実行プレビュー、手動実行
- タスクログ：コマンド/出力分割表示、シンタックスハイライト、多次元フィルタ、一括クリーンアップ
- デーモンログ：リアルタイム表示、キーワードハイライト
- 9 言語、ダーク/ライトテーマ、完全モバイル対応
- CLI ツール：`cronplus status|list|run|logs|reload`

## プロジェクト構造

```
cockpit-cronplus/
├── VERSION                  バージョン（ビルド時に自動インクリメント）
├── build-deb.sh             ワンクリックビルド（フロントエンド + バックエンド）
├── .github/workflows/       GitHub Actions 手動ビルド
├── daemon/                  バックエンド（Python daemon + CLI）
│   ├── src/                 Python ソース
│   └── systemd/             systemd サービスファイル
└── webui/                   フロントエンド（Cockpit プラグイン）
    ├── index.html
    ├── manifest.json
    ├── lang/                9 言語パック
    └── static/              CSS + JS モジュール
```

## インストール

```bash
sudo dpkg -i cronplus_<version>_all.deb
sudo dpkg -i cockpit-cronplus_<version>_all.deb
```

インストール後、Cockpit のサイドバーから **Cronplus** を開きます。

## ビルド

```bash
./build-deb.sh    # VERSION 読み取り → ビルド → 自動 patch+1
```

GitHub Actions から手動トリガー可能 — patch/minor/major を選択して自動ビルド＆リリース作成。

## ファイル

| パス | 説明 |
|------|------|
| `/opt/cronplus/tasks.conf` | タスク設定（JSON） |
| `/opt/cronplus/settings.json` | グローバル設定 |
| `/opt/cronplus/logs/logs.json` | 実行ログ |
| `/opt/cronplus/logs/cronplus.log` | デーモンログ（自動ローテーション） |
| `/usr/bin/cronplus` | CLI ツール |

## License

MIT
