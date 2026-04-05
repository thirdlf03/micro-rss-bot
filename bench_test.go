package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

func printMemUsage(label string) {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("[%s] Alloc=%dKB, Sys=%dKB, HeapInuse=%dKB, StackInuse=%dKB\n",
		label,
		m.Alloc/1024,
		m.Sys/1024,
		m.HeapInuse/1024,
		m.StackSys/1024,
	)
}

func TestMemoryUsage(t *testing.T) {
	printMemUsage("起動直後")

	// DB初期化
	db := setupTestDB(t)
	printMemUsage("DB初期化後")

	// フィード10件追加
	for i := 0; i < 10; i++ {
		AddFeed(db, fmt.Sprintf("https://example.com/feed/%d", i), fmt.Sprintf("Feed %d", i), "", "")
	}
	printMemUsage("フィード10件追加後")

	// 既読記事100件追加
	for i := 0; i < 10; i++ {
		for j := 0; j < 10; j++ {
			MarkArticleSeen(db, int64(i+1), fmt.Sprintf("guid-%d-%d", i, j), "", "")
		}
	}
	printMemUsage("既読記事100件追加後")

	// RSSパース（実際のfetch込み）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(testRSS))
	}))
	defer srv.Close()

	// 既存フィードをテストサーバーURLに差し替え
	db.Exec("DELETE FROM feeds")
	db.Exec("DELETE FROM articles")
	AddFeed(db, srv.URL, "Test", "", "")

	poster := func(a Article) error { return nil }
	FetchAndPost(db, "test-ch", poster)
	printMemUsage("RSSフェッチ+パース後")
}
