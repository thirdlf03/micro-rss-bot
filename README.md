# micro-rss-bot

最小メモリで動くDiscord RSS Bot。フィードを購読して新着記事のタイトルとリンクを投稿します。

## ビルド

```bash
go build -o micro-rss-bot .
```

## Discord Bot セットアップ

1. [Discord Developer Portal](https://discord.com/developers/applications) でアプリケーション作成
2. Bot トークンを取得
3. OAuth2 URL Generator で `bot` + `applications.commands` スコープを選択してサーバーに招待

## コマンド

| コマンド | 説明 |
|---|---|
| `/rss add <url>` | フィードを追加 |
| `/rss list` | 登録済みフィード一覧 |
| `/rss edit <id> <url>` | フィードURLを変更 |
| `/rss delete <id>` | フィードを削除 |
| `/rss channel <#channel>` | 投稿先チャンネルを設定 |
| `/rss interval <分>` | ポーリング間隔を変更（デフォルト: 5分） |

## systemd で動かす

### 1. バイナリを配置

```bash
sudo cp micro-rss-bot /usr/local/bin/
```

### 2. トークンファイルを作成

```bash
sudo mkdir -p /etc/micro-rss-bot
echo 'DISCORD_TOKEN=your-token-here' | sudo tee /etc/micro-rss-bot/env
sudo chmod 600 /etc/micro-rss-bot/env
```

### 3. ユニットファイルを作成

```bash
sudo vi /etc/systemd/system/micro-rss-bot.service
```

```ini
[Unit]
Description=micro-rss-bot
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/micro-rss-bot/env
Environment=DB_PATH=/var/lib/micro-rss-bot/data.db
ExecStart=/usr/local/bin/micro-rss-bot
Restart=on-failure
RestartSec=5

# セキュリティ
DynamicUser=yes
StateDirectory=micro-rss-bot
WorkingDirectory=/var/lib/micro-rss-bot

[Install]
WantedBy=multi-user.target
```

### 4. 起動

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now micro-rss-bot
```

### 5. ログ確認

```bash
journalctl -u micro-rss-bot -f
```
