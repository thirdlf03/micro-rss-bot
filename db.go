package main

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type Feed struct {
	ID    int64
	URL   string
	Title string
}

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
