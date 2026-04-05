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
	AddFeed(db, srv.URL, "Test", "", "")

	var posted []string
	poster := func(a Article) error {
		posted = append(posted, a.Title)
		return nil
	}

	err := FetchAndPost(db, "test-channel", poster)
	if err != nil {
		t.Fatal(err)
	}
	if len(posted) != 2 {
		t.Errorf("expected 2 posts, got %d", len(posted))
	}

	posted = nil
	FetchAndPost(db, "test-channel", poster)
	if len(posted) != 0 {
		t.Errorf("expected 0 posts on second run, got %d", len(posted))
	}
}

func TestStartFetcher_Cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	db := setupTestDB(t)
	poster := func(a Article) error { return nil }
	resetCh := make(chan time.Duration)

	go func() {
		StartFetcher(ctx, db, func() string { return "test-ch" }, poster, 1*time.Second, resetCh)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("fetcher did not stop after cancel")
	}
}
