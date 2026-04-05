package main

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

type Feed struct {
	ID        int64
	URL       string
	Title     string
	ChannelID string
	Format    string
}

func OpenDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")
	db.Exec("PRAGMA foreign_keys=ON")
	if err := InitDB(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := MigrateDB(db); err != nil {
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
		channel_id TEXT,
		format TEXT NOT NULL DEFAULT 'default',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS articles (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		feed_id INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
		guid TEXT NOT NULL,
		link TEXT,
		title TEXT,
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

func MigrateDB(db *sql.DB) error {
	version, _ := GetConfig(db, "schema_version")
	if version == "" {
		version = "0"
	}
	v, _ := strconv.Atoi(version)

	if v < 1 {
		migrations := []string{
			"ALTER TABLE articles ADD COLUMN link TEXT",
			"ALTER TABLE articles ADD COLUMN title TEXT",
			"ALTER TABLE feeds ADD COLUMN channel_id TEXT",
			"ALTER TABLE feeds ADD COLUMN format TEXT NOT NULL DEFAULT 'default'",
		}
		for _, m := range migrations {
			_, err := db.Exec(m)
			if err != nil && !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("migration v1: %w", err)
			}
		}
		db.Exec("CREATE INDEX IF NOT EXISTS idx_articles_link ON articles(link)")
		SetConfig(db, "schema_version", "1")
	}
	return nil
}

func AddFeed(db *sql.DB, url, title, channelID, format string) (int64, error) {
	if format == "" {
		format = "default"
	}
	var chID *string
	if channelID != "" {
		chID = &channelID
	}
	res, err := db.Exec("INSERT INTO feeds (url, title, channel_id, format) VALUES (?, ?, ?, ?)", url, title, chID, format)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func ListFeeds(db *sql.DB) ([]Feed, error) {
	rows, err := db.Query("SELECT id, url, title, COALESCE(channel_id, ''), format FROM feeds ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var feeds []Feed
	for rows.Next() {
		var f Feed
		if err := rows.Scan(&f.ID, &f.URL, &f.Title, &f.ChannelID, &f.Format); err != nil {
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

func SetFeedFormat(db *sql.DB, id int64, format string) error {
	res, err := db.Exec("UPDATE feeds SET format=? WHERE id=?", format, id)
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

func MarkArticleSeen(db *sql.DB, feedID int64, guid, link, title string) error {
	_, err := db.Exec("INSERT OR IGNORE INTO articles (feed_id, guid, link, title) VALUES (?, ?, ?, ?)", feedID, guid, link, title)
	return err
}

func IsArticleLinkSeen(db *sql.DB, link string) (bool, error) {
	if link == "" {
		return false, nil
	}
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM articles WHERE link=?", link).Scan(&count)
	return count > 0, err
}

type ArticleRow struct {
	ID        int64  `json:"id"`
	FeedID    int64  `json:"feed_id"`
	GUID      string `json:"guid"`
	Link      string `json:"link"`
	Title     string `json:"title"`
	PostedAt  string `json:"posted_at"`
	FeedTitle string `json:"feed_title"`
}

func SearchArticles(db *sql.DB, query string, limit, offset int) ([]ArticleRow, int, error) {
	var total int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM articles a JOIN feeds f ON a.feed_id=f.id WHERE a.title LIKE '%' || ? || '%'",
		query,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := db.Query(
		`SELECT a.id, a.feed_id, a.guid, COALESCE(a.link,''), COALESCE(a.title,''), a.posted_at, f.title
		FROM articles a JOIN feeds f ON a.feed_id=f.id
		WHERE a.title LIKE '%' || ? || '%'
		ORDER BY a.posted_at DESC LIMIT ? OFFSET ?`,
		query, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var articles []ArticleRow
	for rows.Next() {
		var a ArticleRow
		if err := rows.Scan(&a.ID, &a.FeedID, &a.GUID, &a.Link, &a.Title, &a.PostedAt, &a.FeedTitle); err != nil {
			return nil, 0, err
		}
		articles = append(articles, a)
	}
	return articles, total, rows.Err()
}

func ListArticles(db *sql.DB, limit, offset int) ([]ArticleRow, int, error) {
	var total int
	db.QueryRow("SELECT COUNT(*) FROM articles").Scan(&total)

	rows, err := db.Query(
		`SELECT a.id, a.feed_id, a.guid, COALESCE(a.link,''), COALESCE(a.title,''), a.posted_at, f.title
		FROM articles a JOIN feeds f ON a.feed_id=f.id
		ORDER BY a.posted_at DESC LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var articles []ArticleRow
	for rows.Next() {
		var a ArticleRow
		if err := rows.Scan(&a.ID, &a.FeedID, &a.GUID, &a.Link, &a.Title, &a.PostedAt, &a.FeedTitle); err != nil {
			return nil, 0, err
		}
		articles = append(articles, a)
	}
	return articles, total, rows.Err()
}

func GetFeed(db *sql.DB, id int64) (*Feed, error) {
	var f Feed
	err := db.QueryRow("SELECT id, url, title, COALESCE(channel_id,''), format FROM feeds WHERE id=?", id).
		Scan(&f.ID, &f.URL, &f.Title, &f.ChannelID, &f.Format)
	if err != nil {
		return nil, err
	}
	return &f, nil
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
