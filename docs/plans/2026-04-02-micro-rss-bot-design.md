# micro-rss-bot 設計書

## 概要

最小メモリ・最小プロセスサイズで動作するDiscord RSS Bot。
RSSフィードを購読し、新着記事のタイトルとリンクをDiscordチャンネルに投稿する。

## 技術選定

- **言語**: Go
- **DB**: SQLite（modernc.org/sqlite, CGO不要）
- **Discord**: discordgo
- **RSS**: gofeed
- **デプロイ**: VPS（バイナリ直置き）
- **対象**: 1サーバーのみ

## ファイル構成

```
micro-rss-bot/
├── main.go          # エントリポイント + Bot起動
├── handler.go       # スラッシュコマンドハンドラ
├── fetcher.go       # RSSフェッチ + ポーリングループ
├── db.go            # SQLite操作
├── go.mod
└── data.db          # 実行時に生成
```

## データベーススキーマ

```sql
CREATE TABLE feeds (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    url TEXT NOT NULL UNIQUE,
    title TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE articles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    feed_id INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
    guid TEXT NOT NULL,
    posted_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(feed_id, guid)
);

CREATE TABLE config (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
-- 初期値: channel_id=(未設定), interval_minutes=5
```

## コマンド仕様

### `/rss add <feed-url>`
- URLをgofeedでfetchして検証
- feedsテーブルに挿入（タイトル自動取得）
- 成功/失敗メッセージ返却

### `/rss list`
- 全フィード一覧を表示
- `#1 | Zenn Trending | https://zenn.dev/feed` 形式

### `/rss edit <id> <url>`
- 新URLでfetch検証 → URL・タイトル更新
- 該当フィードのarticlesをクリア（既読リセット）

### `/rss delete <id>`
- IDで削除（CASCADE で articles も消える）

### `/rss channel <#channel>`
- configテーブルにchannel_idを保存

### `/rss interval <分>`
- 1〜1440の範囲でバリデーション
- configテーブルに保存 + tickerリセット

## フェッチャー

- goroutine 1つで `time.NewTicker(interval)` によるポーリング
- 各フィードをgofeedでパース
- guid（なければlink）で重複判定
- 新着のみDiscordに投稿してMarkSeen

### 投稿フォーマット

```
📰 記事タイトル
https://example.com/article/123
```

プレーンテキスト。Discordの自動リンクプレビューを活用。

### エラーハンドリング

- fetch失敗 → ログ出力してスキップ（次tickで再試行）
- DB書き込み失敗 → ログ出力してスキップ
- Discord投稿失敗 → ログ出力、MarkSeenしない（次回再試行）
- リトライ/バックオフ不要（ポーリング間隔で自然に再試行）

## 環境変数

| 変数名 | 必須 | 説明 |
|---|---|---|
| `DISCORD_TOKEN` | Yes | Botトークン |
| `DB_PATH` | No | SQLiteファイルパス（デフォルト: `./data.db`） |

## Graceful Shutdown

SIGINT/SIGTERM → context cancel → fetcher停止 → Discord Close → SQLite Close
