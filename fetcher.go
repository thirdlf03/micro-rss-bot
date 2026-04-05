package main

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

func isBridgeError(item *gofeed.Item) bool {
	if strings.Contains(item.Title, "Bridge returned error") {
		return true
	}
	if strings.Contains(item.Link, "action=display&bridge=") && strings.Contains(item.Link, "Bridge") {
		return true
	}
	return false
}

type Article struct {
	Title      string
	Link       string
	Categories []string
	Content    string
	FeedTitle  string
	FeedFormat string
	ChannelID  string
}

type Poster func(article Article) error

func FetchAndPost(db *sql.DB, defaultChannelID string, post Poster) error {
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

		chID := f.ChannelID
		if chID == "" {
			chID = defaultChannelID
		}

		for _, item := range feed.Items {
			if isBridgeError(item) {
				log.Printf("skipping bridge error: %s (feed: %s)", item.Title, f.URL)
				continue
			}
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
			// Cross-feed duplicate check by link
			linkSeen, err := IsArticleLinkSeen(db, item.Link)
			if err != nil {
				log.Printf("check link seen: %v", err)
				continue
			}
			if linkSeen {
				MarkArticleSeen(db, f.ID, guid, item.Link, item.Title)
				continue
			}

			var categories []string
			for _, c := range item.Categories {
				categories = append(categories, c)
			}
			content := ""
			if item.Description != "" {
				content = item.Description
			} else if item.Content != "" {
				content = item.Content
			}

			a := Article{
				Title:      item.Title,
				Link:       item.Link,
				Categories: categories,
				Content:    content,
				FeedTitle:  f.Title,
				FeedFormat: f.Format,
				ChannelID:  chID,
			}
			if err := post(a); err != nil {
				log.Printf("post: %v", err)
				continue
			}
			if err := MarkArticleSeen(db, f.ID, guid, item.Link, item.Title); err != nil {
				log.Printf("mark seen: %v", err)
			}
		}
	}
	return nil
}

func StartFetcher(ctx context.Context, db *sql.DB, defaultChannelID func() string, post Poster, interval time.Duration, resetInterval <-chan time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	FetchAndPost(db, defaultChannelID(), post)

	for {
		select {
		case <-ticker.C:
			FetchAndPost(db, defaultChannelID(), post)
		case d := <-resetInterval:
			ticker.Reset(d)
			log.Printf("polling interval changed to %v", d)
		case <-ctx.Done():
			return
		}
	}
}
