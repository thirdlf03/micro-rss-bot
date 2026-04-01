# micro-rss-bot Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** RSSフィードを購読し新着記事をDiscordに投稿する最小Botを構築する

**Architecture:** Go単一パッケージ、4ソースファイル構成。SQLiteで永続化、goroutine 1つでポーリング。discordgoのスラッシュコマンドでCRUD操作。

**Tech Stack:** Go, discordgo, gofeed, modernc.org/sqlite

---

### Task 1: プロジェクト初期化

**Files:**
- Create: `go.mod`
- Create: `main.go`

**Step 1: Go module 初期化**

Run:
```bash
go mod init github.com/thirdlf03/micro-rss-bot
```

**Step 2: 依存追加**

Run:
```bash
go get github.com/bwmarrin/discordgo
go get github.com/mmcdole/gofeed
go get modernc.org/sqlite
```

**Step 3: 最小 main.go を作成**

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "DISCORD_TOKEN is required")
		os.Exit(1)
	}
	fmt.Println("micro-rss-bot starting...")
}
```

**Step 4: ビルド確認**

Run: `go build -o micro-rss-bot .`
Expected: バイナリ生成、エラーなし

**Step 5: Commit**

```bash
git add go.mod go.sum main.go
git commit -m "feat: initialize Go project with dependencies"
```

---

### Task 2: DB層 — スキーマ初期化とCRUD

**Files:**
- Create: `db.go`
- Create: `db_test.go`

**Step 1: db_test.go にスキーマ初期化テストを書く**

```go
package main

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := InitDB(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestInitDB(t *testing.T) {
	db := setupTestDB(t)

	// テーブルが存在するか確認
	tables := []string{"feeds", "articles", "config"}
	for _, table := range tables {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %s not found: %v", table, err)
		}
	}

	// config デフォルト値確認
	var val string
	if err := db.QueryRow("SELECT value FROM config WHERE key='interval_minutes'").Scan(&val); err != nil {
		t.Fatal(err)
	}
	if val != "5" {
		t.Errorf("expected interval_minutes=5, got %s", val)
	}
}
```

**Step 2: テスト実行、失敗を確認**

Run: `go test -run TestInitDB -v`
Expected: FAIL（InitDB が未定義）

**Step 3: db.go にスキーマ初期化を実装**

```go
package main

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

func OpenDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")
	if err := InitDB(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func InitDB(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS feeds (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		url TEXT NOT NULL UNIQUE,
		title TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS articles (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		feed_id INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
		guid TEXT NOT NULL,
		posted_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(feed_id, guid)
	);
	CREATE TABLE IF NOT EXISTS config (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	INSERT OR IGNORE INTO config (key, value) VALUES ('interval_minutes', '5');
	INSERT OR IGNORE INTO config (key, value) VALUES ('channel_id', '');
	`
	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("init schema: %w", err)
	}
	return nil
}
```

**Step 4: テスト実行、パスを確認**

Run: `go test -run TestInitDB -v`
Expected: PASS

**Step 5: フィードCRUDテストを書く**

```go
func TestAddFeed(t *testing.T) {
	db := setupTestDB(t)

	id, err := AddFeed(db, "https://example.com/feed", "Example")
	if err != nil {
		t.Fatal(err)
	}
	if id != 1 {
		t.Errorf("expected id=1, got %d", id)
	}

	// 重複追加はエラー
	_, err = AddFeed(db, "https://example.com/feed", "Example")
	if err == nil {
		t.Error("expected error on duplicate feed")
	}
}

func TestListFeeds(t *testing.T) {
	db := setupTestDB(t)
	AddFeed(db, "https://a.com/feed", "A")
	AddFeed(db, "https://b.com/feed", "B")

	feeds, err := ListFeeds(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 2 {
		t.Errorf("expected 2 feeds, got %d", len(feeds))
	}
}

func TestEditFeed(t *testing.T) {
	db := setupTestDB(t)
	AddFeed(db, "https://old.com/feed", "Old")

	err := EditFeed(db, 1, "https://new.com/feed", "New")
	if err != nil {
		t.Fatal(err)
	}

	feeds, _ := ListFeeds(db)
	if feeds[0].URL != "https://new.com/feed" {
		t.Errorf("expected new URL, got %s", feeds[0].URL)
	}
}

func TestDeleteFeed(t *testing.T) {
	db := setupTestDB(t)
	AddFeed(db, "https://example.com/feed", "Example")

	err := DeleteFeed(db, 1)
	if err != nil {
		t.Fatal(err)
	}

	feeds, _ := ListFeeds(db)
	if len(feeds) != 0 {
		t.Errorf("expected 0 feeds, got %d", len(feeds))
	}
}
```

**Step 6: テスト実行、失敗を確認**

Run: `go test -run "TestAdd|TestList|TestEdit|TestDelete" -v`
Expected: FAIL

**Step 7: フィードCRUDを実装**

```go
type Feed struct {
	ID    int64
	URL   string
	Title string
}

func AddFeed(db *sql.DB, url, title string) (int64, error) {
	res, err := db.Exec("INSERT INTO feeds (url, title) VALUES (?, ?)", url, title)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func ListFeeds(db *sql.DB) ([]Feed, error) {
	rows, err := db.Query("SELECT id, url, title FROM feeds ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var feeds []Feed
	for rows.Next() {
		var f Feed
		if err := rows.Scan(&f.ID, &f.URL, &f.Title); err != nil {
			return nil, err
		}
		feeds = append(feeds, f)
	}
	return feeds, rows.Err()
}

func EditFeed(db *sql.DB, id int64, url, title string) error {
	res, err := db.Exec("UPDATE feeds SET url=?, title=? WHERE id=?", url, title, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("feed %d not found", id)
	}
	// 既読リセット
	db.Exec("DELETE FROM articles WHERE feed_id=?", id)
	return nil
}

func DeleteFeed(db *sql.DB, id int64) error {
	res, err := db.Exec("DELETE FROM feeds WHERE id=?", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("feed %d not found", id)
	}
	return nil
}
```

**Step 8: テスト実行、パスを確認**

Run: `go test -run "TestAdd|TestList|TestEdit|TestDelete" -v`
Expected: PASS

**Step 9: 既読管理・config テストを書く**

```go
func TestArticleSeen(t *testing.T) {
	db := setupTestDB(t)
	AddFeed(db, "https://example.com/feed", "Example")

	seen, err := IsArticleSeen(db, 1, "guid-123")
	if err != nil {
		t.Fatal(err)
	}
	if seen {
		t.Error("expected not seen")
	}

	if err := MarkArticleSeen(db, 1, "guid-123"); err != nil {
		t.Fatal(err)
	}

	seen, _ = IsArticleSeen(db, 1, "guid-123")
	if !seen {
		t.Error("expected seen")
	}
}

func TestConfig(t *testing.T) {
	db := setupTestDB(t)

	val, err := GetConfig(db, "interval_minutes")
	if err != nil {
		t.Fatal(err)
	}
	if val != "5" {
		t.Errorf("expected 5, got %s", val)
	}

	if err := SetConfig(db, "interval_minutes", "10"); err != nil {
		t.Fatal(err)
	}
	val, _ = GetConfig(db, "interval_minutes")
	if val != "10" {
		t.Errorf("expected 10, got %s", val)
	}
}
```

**Step 10: テスト実行、失敗を確認**

Run: `go test -run "TestArticle|TestConfig" -v`
Expected: FAIL

**Step 11: 既読管理・configを実装**

```go
func IsArticleSeen(db *sql.DB, feedID int64, guid string) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM articles WHERE feed_id=? AND guid=?", feedID, guid).Scan(&count)
	return count > 0, err
}

func MarkArticleSeen(db *sql.DB, feedID int64, guid string) error {
	_, err := db.Exec("INSERT OR IGNORE INTO articles (feed_id, guid) VALUES (?, ?)", feedID, guid)
	return err
}

func GetConfig(db *sql.DB, key string) (string, error) {
	var val string
	err := db.QueryRow("SELECT value FROM config WHERE key=?", key).Scan(&val)
	return val, err
}

func SetConfig(db *sql.DB, key, value string) error {
	_, err := db.Exec("INSERT OR REPLACE INTO config (key, value) VALUES (?, ?)", key, value)
	return err
}
```

**Step 12: 全テスト実行、パスを確認**

Run: `go test -v`
Expected: ALL PASS

**Step 13: Commit**

```bash
git add db.go db_test.go
git commit -m "feat: add SQLite DB layer with feed CRUD, article tracking, config"
```

---

### Task 3: フェッチャー

**Files:**
- Create: `fetcher.go`
- Create: `fetcher_test.go`

**Step 1: fetcher_test.go にテストを書く**

```go
package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const testRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
<channel>
  <title>Test Feed</title>
  <item>
    <title>Article 1</title>
    <link>https://example.com/1</link>
    <guid>guid-1</guid>
  </item>
  <item>
    <title>Article 2</title>
    <link>https://example.com/2</link>
    <guid>guid-2</guid>
  </item>
</channel>
</rss>`

func TestFetchNewArticles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(testRSS))
	}))
	defer srv.Close()

	db := setupTestDB(t)
	AddFeed(db, srv.URL, "Test")

	var posted []string
	poster := func(title, link string) error {
		posted = append(posted, title)
		return nil
	}

	err := FetchAndPost(db, poster)
	if err != nil {
		t.Fatal(err)
	}
	if len(posted) != 2 {
		t.Errorf("expected 2 posts, got %d", len(posted))
	}

	// 2回目は新着なし
	posted = nil
	FetchAndPost(db, poster)
	if len(posted) != 0 {
		t.Errorf("expected 0 posts on second run, got %d", len(posted))
	}
}

func TestStartFetcher_Cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	db := setupTestDB(t)
	poster := func(title, link string) error { return nil }

	go func() {
		StartFetcher(ctx, db, poster, 1*time.Second)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// OK: goroutine stopped
	case <-time.After(3 * time.Second):
		t.Error("fetcher did not stop after cancel")
	}
}
```

**Step 2: テスト実行、失敗を確認**

Run: `go test -run "TestFetch|TestStartFetcher" -v`
Expected: FAIL

**Step 3: fetcher.go を実装**

```go
package main

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/mmcdole/gofeed"
)

type Poster func(title, link string) error

func FetchAndPost(db *sql.DB, post Poster) error {
	feeds, err := ListFeeds(db)
	if err != nil {
		return err
	}

	fp := gofeed.NewParser()
	for _, f := range feeds {
		feed, err := fp.ParseURL(f.URL)
		if err != nil {
			log.Printf("fetch %s: %v", f.URL, err)
			continue
		}
		for _, item := range feed.Items {
			guid := item.GUID
			if guid == "" {
				guid = item.Link
			}
			seen, err := IsArticleSeen(db, f.ID, guid)
			if err != nil {
				log.Printf("check seen: %v", err)
				continue
			}
			if seen {
				continue
			}
			if err := post(item.Title, item.Link); err != nil {
				log.Printf("post: %v", err)
				continue
			}
			if err := MarkArticleSeen(db, f.ID, guid); err != nil {
				log.Printf("mark seen: %v", err)
			}
		}
	}
	return nil
}

func StartFetcher(ctx context.Context, db *sql.DB, post Poster, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 起動時に1回実行
	FetchAndPost(db, post)

	for {
		select {
		case <-ticker.C:
			FetchAndPost(db, post)
		case <-ctx.Done():
			return
		}
	}
}
```

**Step 4: テスト実行、パスを確認**

Run: `go test -run "TestFetch|TestStartFetcher" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add fetcher.go fetcher_test.go
git commit -m "feat: add RSS fetcher with polling loop and duplicate detection"
```

---

### Task 4: Discordハンドラー

**Files:**
- Create: `handler.go`

**Step 1: handler.go を実装**

```go
package main

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/mmcdole/gofeed"
)

var commands = []*discordgo.ApplicationCommand{
	{
		Name:        "rss",
		Description: "RSS feed management",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "add",
				Description: "Add a new RSS feed",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:        "url",
						Description: "Feed URL",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
					},
				},
			},
			{
				Name:        "list",
				Description: "List all feeds",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "edit",
				Description: "Edit a feed URL",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:        "id",
						Description: "Feed ID",
						Type:        discordgo.ApplicationCommandOptionInteger,
						Required:    true,
					},
					{
						Name:        "url",
						Description: "New feed URL",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
					},
				},
			},
			{
				Name:        "delete",
				Description: "Delete a feed",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:        "id",
						Description: "Feed ID",
						Type:        discordgo.ApplicationCommandOptionInteger,
						Required:    true,
					},
				},
			},
			{
				Name:        "channel",
				Description: "Set the posting channel",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:        "target",
						Description: "Channel to post to",
						Type:        discordgo.ApplicationCommandOptionChannel,
						Required:    true,
					},
				},
			},
			{
				Name:        "interval",
				Description: "Set polling interval in minutes",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:        "minutes",
						Description: "Interval (1-1440)",
						Type:        discordgo.ApplicationCommandOptionInteger,
						Required:    true,
						MinValue:    floatPtr(1),
						MaxValue:    1440,
					},
				},
			},
		},
	},
}

func floatPtr(f float64) *float64 { return &f }

type Handler struct {
	db             *sql.DB
	resetInterval  chan time.Duration
}

func NewHandler(db *sql.DB, resetInterval chan time.Duration) *Handler {
	return &Handler{db: db, resetInterval: resetInterval}
}

func (h *Handler) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	data := i.ApplicationCommandData()
	if data.Name != "rss" || len(data.Options) == 0 {
		return
	}

	sub := data.Options[0]
	var content string

	switch sub.Name {
	case "add":
		content = h.handleAdd(sub)
	case "list":
		content = h.handleList()
	case "edit":
		content = h.handleEdit(sub)
	case "delete":
		content = h.handleDelete(sub)
	case "channel":
		content = h.handleChannel(sub)
	case "interval":
		content = h.handleInterval(sub)
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: content},
	})
}

func (h *Handler) handleAdd(sub *discordgo.ApplicationCommandInteractionDataOption) string {
	url := sub.Options[0].StringValue()

	fp := gofeed.NewParser()
	feed, err := fp.ParseURL(url)
	if err != nil {
		return fmt.Sprintf("❌ フィードを取得できませんでした: %v", err)
	}

	id, err := AddFeed(h.db, url, feed.Title)
	if err != nil {
		return fmt.Sprintf("❌ 追加に失敗しました: %v", err)
	}
	return fmt.Sprintf("✅ `%s` を追加しました (ID: %d)", feed.Title, id)
}

func (h *Handler) handleList() string {
	feeds, err := ListFeeds(h.db)
	if err != nil {
		return fmt.Sprintf("❌ エラー: %v", err)
	}
	if len(feeds) == 0 {
		return "フィードが登録されていません"
	}
	msg := ""
	for _, f := range feeds {
		msg += fmt.Sprintf("#%d | %s | %s\n", f.ID, f.Title, f.URL)
	}
	return msg
}

func (h *Handler) handleEdit(sub *discordgo.ApplicationCommandInteractionDataOption) string {
	id := sub.Options[0].IntValue()
	url := sub.Options[1].StringValue()

	fp := gofeed.NewParser()
	feed, err := fp.ParseURL(url)
	if err != nil {
		return fmt.Sprintf("❌ フィードを取得できませんでした: %v", err)
	}

	if err := EditFeed(h.db, id, url, feed.Title); err != nil {
		return fmt.Sprintf("❌ 更新に失敗しました: %v", err)
	}
	return fmt.Sprintf("✅ フィード #%d を `%s` に更新しました", id, feed.Title)
}

func (h *Handler) handleDelete(sub *discordgo.ApplicationCommandInteractionDataOption) string {
	id := sub.Options[0].IntValue()
	if err := DeleteFeed(h.db, id); err != nil {
		return fmt.Sprintf("❌ %v", err)
	}
	return fmt.Sprintf("🗑 フィード #%d を削除しました", id)
}

func (h *Handler) handleChannel(sub *discordgo.ApplicationCommandInteractionDataOption) string {
	ch := sub.Options[0].ChannelValue(nil)
	if err := SetConfig(h.db, "channel_id", ch.ID); err != nil {
		return fmt.Sprintf("❌ %v", err)
	}
	return fmt.Sprintf("📢 投稿先を <#%s> に設定しました", ch.ID)
}

func (h *Handler) handleInterval(sub *discordgo.ApplicationCommandInteractionDataOption) string {
	minutes := sub.Options[0].IntValue()
	if err := SetConfig(h.db, "interval_minutes", strconv.FormatInt(minutes, 10)); err != nil {
		return fmt.Sprintf("❌ %v", err)
	}
	h.resetInterval <- time.Duration(minutes) * time.Minute
	return fmt.Sprintf("⏱ ポーリング間隔を %d分 に変更しました", minutes)
}

func RegisterCommands(s *discordgo.Session, h *Handler) {
	s.AddHandler(h.Handle)
	for _, cmd := range commands {
		_, err := s.ApplicationCommandCreate(s.State.User.ID, "", cmd)
		if err != nil {
			log.Printf("register command %s: %v", cmd.Name, err)
		}
	}
}
```

**Step 2: ビルド確認**

Run: `go build ./...`
Expected: エラーなし

**Step 3: Commit**

```bash
git add handler.go
git commit -m "feat: add Discord slash command handler for RSS management"
```

---

### Task 5: main.go 統合 + interval リセット対応

**Files:**
- Modify: `main.go`
- Modify: `fetcher.go` (resetInterval チャンネル対応)

**Step 1: fetcher.go に interval リセット対応を追加**

`StartFetcher` の引数に `resetInterval <-chan time.Duration` を追加:

```go
func StartFetcher(ctx context.Context, db *sql.DB, post Poster, interval time.Duration, resetInterval <-chan time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	FetchAndPost(db, post)

	for {
		select {
		case <-ticker.C:
			FetchAndPost(db, post)
		case d := <-resetInterval:
			ticker.Reset(d)
			log.Printf("polling interval changed to %v", d)
		case <-ctx.Done():
			return
		}
	}
}
```

**Step 2: fetcher_test.go の TestStartFetcher_Cancellation を更新**

```go
func TestStartFetcher_Cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	db := setupTestDB(t)
	poster := func(title, link string) error { return nil }
	resetCh := make(chan time.Duration)

	go func() {
		StartFetcher(ctx, db, poster, 1*time.Second, resetCh)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("fetcher did not stop after cancel")
	}
}
```

**Step 3: main.go を完成させる**

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
)

func main() {
	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "DISCORD_TOKEN is required")
		os.Exit(1)
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./data.db"
	}

	db, err := OpenDB(dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatalf("create session: %v", err)
	}

	resetInterval := make(chan time.Duration, 1)
	handler := NewHandler(db, resetInterval)

	dg.AddHandler(handler.Handle)
	dg.Identify.Intents = discordgo.IntentsGuilds

	if err := dg.Open(); err != nil {
		log.Fatalf("open session: %v", err)
	}
	defer dg.Close()

	RegisterCommands(dg, handler)
	log.Println("micro-rss-bot is running")

	// ポーリング間隔取得
	intervalStr, _ := GetConfig(db, "interval_minutes")
	minutes, err := strconv.Atoi(intervalStr)
	if err != nil || minutes < 1 {
		minutes = 5
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	poster := func(title, link string) error {
		chID, _ := GetConfig(db, "channel_id")
		if chID == "" {
			return fmt.Errorf("channel not set")
		}
		msg := fmt.Sprintf("📰 %s\n%s", title, link)
		_, err := dg.ChannelMessageSend(chID, msg)
		return err
	}

	go StartFetcher(ctx, db, poster, time.Duration(minutes)*time.Minute, resetInterval)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down...")
	cancel()
}
```

**Step 4: テスト全件パスを確認**

Run: `go test -v`
Expected: ALL PASS

**Step 5: ビルド確認**

Run: `go build -o micro-rss-bot .`
Expected: バイナリ生成、エラーなし

**Step 6: Commit**

```bash
git add main.go fetcher.go fetcher_test.go handler.go
git commit -m "feat: integrate all components - bot is fully functional"
```

---

### Task 6: .gitignore + 最終確認

**Files:**
- Create: `.gitignore`

**Step 1: .gitignore 作成**

```
micro-rss-bot
data.db
.env
```

**Step 2: 全テスト実行**

Run: `go test -v -count=1`
Expected: ALL PASS

**Step 3: ビルドサイズ確認**

Run: `go build -o micro-rss-bot . && ls -lh micro-rss-bot`
Expected: バイナリが生成され、サイズが確認できる

**Step 4: Commit**

```bash
git add .gitignore
git commit -m "chore: add .gitignore"
```
