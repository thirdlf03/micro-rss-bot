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

	tables := []string{"feeds", "articles", "config"}
	for _, table := range tables {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %s not found: %v", table, err)
		}
	}

	var val string
	if err := db.QueryRow("SELECT value FROM config WHERE key='interval_minutes'").Scan(&val); err != nil {
		t.Fatal(err)
	}
	if val != "5" {
		t.Errorf("expected interval_minutes=5, got %s", val)
	}
}

func TestAddFeed(t *testing.T) {
	db := setupTestDB(t)

	id, err := AddFeed(db, "https://example.com/feed", "Example")
	if err != nil {
		t.Fatal(err)
	}
	if id != 1 {
		t.Errorf("expected id=1, got %d", id)
	}

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
