package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Simulates existing forum tags on Discord
func makeExistingTags(names ...string) map[string]string {
	m := make(map[string]string)
	for i, name := range names {
		m[strings.ToLower(name)] = fmt.Sprintf("tag-%d", i+1)
	}
	return m
}

func TestMatchTags_WithCategories(t *testing.T) {
	existing := makeExistingTags("AI", "Cloud", "Security")

	a := Article{
		Title:      "New AI model released",
		Categories: []string{"AI", "Machine Learning"},
		FeedTitle:  "Tech Blog",
	}

	result := matchTags(a, existing)

	t.Logf("記事: %q", a.Title)
	t.Logf("カテゴリ: %v", a.Categories)
	t.Logf("既存タグにマッチ: %v", result.MatchedIDs)
	t.Logf("新規作成するタグ: %v", result.NewTagNames)

	if len(result.MatchedIDs) != 1 || result.MatchedIDs[0] != "tag-1" {
		t.Errorf("expected AI tag matched, got %v", result.MatchedIDs)
	}
	if len(result.NewTagNames) != 1 || result.NewTagNames[0] != "Machine Learning" {
		t.Errorf("expected Machine Learning as new tag, got %v", result.NewTagNames)
	}
}

func TestMatchTags_NoCategoriesTitleMatch(t *testing.T) {
	existing := makeExistingTags("AI", "Cloud", "Security", "Go")

	a := Article{
		Title:     "Go 1.23 security patch released",
		FeedTitle: "Golang Blog",
	}

	result := matchTags(a, existing)

	t.Logf("記事: %q", a.Title)
	t.Logf("カテゴリなし → タイトルからマッチ")
	t.Logf("既存タグにマッチ: %v", result.MatchedIDs)
	t.Logf("新規作成するタグ: %v", result.NewTagNames)

	// Should match "go" and "security" from title
	if len(result.MatchedIDs) != 2 {
		t.Errorf("expected 2 title matches (go, security), got %v", result.MatchedIDs)
	}
	if len(result.NewTagNames) != 0 {
		t.Errorf("expected no new tags, got %v", result.NewTagNames)
	}
}

func TestMatchTags_NoCategoriesNoTitleMatch_FallbackToFeedTitle(t *testing.T) {
	existing := makeExistingTags("AI", "Cloud")

	a := Article{
		Title:     "Version 2.0 is here",
		FeedTitle: "OpenClaw Releases",
	}

	result := matchTags(a, existing)

	t.Logf("記事: %q", a.Title)
	t.Logf("カテゴリなし、タイトルマッチなし → フィード名をタグに")
	t.Logf("既存タグにマッチ: %v", result.MatchedIDs)
	t.Logf("新規作成するタグ: %v", result.NewTagNames)

	if len(result.MatchedIDs) != 0 {
		t.Errorf("expected no matched IDs, got %v", result.MatchedIDs)
	}
	if len(result.NewTagNames) != 1 || result.NewTagNames[0] != "OpenClaw Releases" {
		t.Errorf("expected feed title as new tag, got %v", result.NewTagNames)
	}
}

func TestMatchTags_FeedTitleAlreadyExists(t *testing.T) {
	existing := makeExistingTags("OpenClaw Releases", "Cloud")

	a := Article{
		Title:     "v2026.4.1 hotfix",
		FeedTitle: "OpenClaw Releases",
	}

	result := matchTags(a, existing)

	t.Logf("記事: %q", a.Title)
	t.Logf("カテゴリなし、タイトルマッチなし → フィード名が既存タグ")
	t.Logf("既存タグにマッチ: %v", result.MatchedIDs)

	if len(result.MatchedIDs) != 1 || result.MatchedIDs[0] != "tag-1" {
		t.Errorf("expected feed title tag matched, got %v", result.MatchedIDs)
	}
	if len(result.NewTagNames) != 0 {
		t.Errorf("expected no new tags, got %v", result.NewTagNames)
	}
}

func TestMatchTags_CaseInsensitive(t *testing.T) {
	existing := makeExistingTags("ai", "CLOUD")

	a := Article{
		Title:      "AI update",
		Categories: []string{"AI", "Cloud"},
	}

	result := matchTags(a, existing)

	t.Logf("記事: %q (カテゴリ: %v)", a.Title, a.Categories)
	t.Logf("大文字小文字無視でマッチ")
	t.Logf("マッチ: %v", result.MatchedIDs)

	if len(result.MatchedIDs) != 2 {
		t.Errorf("expected 2 case-insensitive matches, got %v", result.MatchedIDs)
	}
}

// Integration: multiple feeds with different categories through FetchAndPost
func TestMultipleFeedsWithCategories(t *testing.T) {
	rss1 := `<?xml version="1.0"?>
<rss version="2.0">
<channel><title>Tech Blog</title>
  <item><title>New AI breakthrough</title><link>https://tech.example.com/ai-1</link><guid>ai-1</guid>
    <category>AI</category><category>Research</category></item>
  <item><title>Cloud security update</title><link>https://tech.example.com/sec-1</link><guid>sec-1</guid>
    <category>Security</category><category>Cloud</category></item>
</channel></rss>`

	rss2 := `<?xml version="1.0"?>
<rss version="2.0">
<channel><title>OpenClaw Releases</title>
  <item><title>v2026.4.1</title><link>https://github.com/openclaw/openclaw/releases/v2026.4.1</link><guid>v2026.4.1</guid>
    <description>Breaking changes in this release</description></item>
  <item><title>v2026.4.0</title><link>https://github.com/openclaw/openclaw/releases/v2026.4.0</link><guid>v2026.4.0</guid></item>
</channel></rss>`

	rss3 := `<?xml version="1.0"?>
<rss version="2.0">
<channel><title>Go Blog</title>
  <item><title>Go 1.23 Released</title><link>https://go.dev/blog/go1.23</link><guid>go123</guid>
    <category>Release</category></item>
</channel></rss>`

	mux := http.NewServeMux()
	mux.HandleFunc("/feed1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(rss1))
	})
	mux.HandleFunc("/feed2", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(rss2))
	})
	mux.HandleFunc("/feed3", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(rss3))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	db := setupTestDB(t)
	AddFeed(db, srv.URL+"/feed1", "Tech Blog", "", "")
	AddFeed(db, srv.URL+"/feed2", "OpenClaw Releases", "", "release")
	AddFeed(db, srv.URL+"/feed3", "Go Blog", "channel-go", "")

	existing := makeExistingTags("AI", "Security", "Cloud")

	var articles []Article
	poster := func(a Article) error {
		articles = append(articles, a)
		return nil
	}

	FetchAndPost(db, "default-ch", poster)

	t.Logf("\n=== 投稿された記事とタグ選択結果 ===\n")
	for _, a := range articles {
		result := matchTags(a, existing)
		t.Logf("📰 タイトル: %s", a.Title)
		t.Logf("   フィード: %s (format: %s)", a.FeedTitle, a.FeedFormat)
		t.Logf("   チャンネル: %s", a.ChannelID)
		t.Logf("   カテゴリ: %v", a.Categories)
		t.Logf("   → マッチしたタグID: %v", result.MatchedIDs)
		t.Logf("   → 新規作成タグ: %v", result.NewTagNames)
		t.Logf("")
	}

	if len(articles) != 5 {
		t.Errorf("expected 5 articles total, got %d", len(articles))
	}

	// Verify channel routing
	for _, a := range articles {
		if a.FeedTitle == "Go Blog" && a.ChannelID != "channel-go" {
			t.Errorf("Go Blog should route to channel-go, got %s", a.ChannelID)
		}
		if a.FeedTitle != "Go Blog" && a.ChannelID != "default-ch" {
			t.Errorf("%s should route to default-ch, got %s", a.FeedTitle, a.ChannelID)
		}
	}

	// Verify release format
	for _, a := range articles {
		if a.FeedTitle == "OpenClaw Releases" && a.FeedFormat != "release" {
			t.Errorf("OpenClaw should have release format, got %s", a.FeedFormat)
		}
	}
}
