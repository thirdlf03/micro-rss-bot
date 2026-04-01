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
