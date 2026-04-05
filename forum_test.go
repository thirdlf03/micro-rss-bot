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

func TestNormalizeToGroup(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"Go 1.23 released", []string{"Programming", "Release"}},
		{"Docker security vulnerability", []string{"Infrastructure", "Security"}},
		{"PostgreSQL performance on Linux 7.0", []string{"Database", "Infrastructure"}},
		{"New React framework", []string{"Frontend"}},
		{"Claude API update", []string{"AI", "Backend", "Release"}},
		{"random unrelated text", nil},
		{"PDF読書アプリをリリースしました", []string{"Release"}},
		{"Kubernetes セキュリティパッチ", []string{"Infrastructure", "Security"}},
	}

	for _, tt := range tests {
		groups := normalizeToGroup(tt.input)
		t.Logf("%q → %v", tt.input, groups)
		if tt.expected == nil {
			if len(groups) != 0 {
				t.Errorf("  expected no groups, got %v", groups)
			}
			continue
		}
		for _, exp := range tt.expected {
			found := false
			for _, g := range groups {
				if g == exp {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("  expected group %q in result %v", exp, groups)
			}
		}
	}
}

func TestMatchTags_CategoryGrouping(t *testing.T) {
	existing := makeExistingTags("AI", "Security", "Release")

	a := Article{
		Title:      "New GPT model released",
		Categories: []string{"Machine Learning", "AI"},
		FeedTitle:  "Tech Blog",
	}

	result := matchTags(a, existing)

	t.Logf("記事: %q, カテゴリ: %v", a.Title, a.Categories)
	t.Logf("マッチ: %v, 新規: %v", result.MatchedIDs, result.NewTagNames)

	// "Machine Learning" → AI group → matches existing "AI" tag
	// "AI" → also AI group → dedup'd
	// Title "released" → Release group → matches existing "Release" tag
	hasAI := false
	hasRelease := false
	for _, id := range result.MatchedIDs {
		if id == "tag-1" {
			hasAI = true
		}
		if id == "tag-3" {
			hasRelease = true
		}
	}
	if !hasAI {
		t.Error("expected AI tag to match")
	}
	if !hasRelease {
		t.Error("expected Release tag to match from title grouping")
	}
}

func TestMatchTags_TitleGrouping_NoCategories(t *testing.T) {
	existing := makeExistingTags("Programming", "Security", "Database")

	a := Article{
		Title:     "Linux 7.0のプリエンプション変更でPostgreSQLが大きな性能低下",
		FeedTitle: "Xenospectrum",
	}

	result := matchTags(a, existing)

	t.Logf("記事: %q", a.Title)
	t.Logf("マッチ: %v, 新規: %v", result.MatchedIDs, result.NewTagNames)

	// "Linux" → Infrastructure, "PostgreSQL" → Database
	hasDB := false
	for _, id := range result.MatchedIDs {
		if id == "tag-3" {
			hasDB = true
		}
	}
	if !hasDB {
		t.Error("expected Database tag to match from PostgreSQL in title")
	}

	hasInfra := false
	for _, name := range result.NewTagNames {
		if name == "Infrastructure" {
			hasInfra = true
		}
	}
	if !hasInfra {
		t.Error("expected Infrastructure as new tag from Linux in title")
	}
}

func TestMatchTags_FallbackFeedTitleGrouping(t *testing.T) {
	existing := makeExistingTags("Release")

	a := Article{
		Title:     "v2026.4.1",
		FeedTitle: "OpenClaw Releases",
	}

	result := matchTags(a, existing)

	t.Logf("記事: %q, フィード: %q", a.Title, a.FeedTitle)
	t.Logf("マッチ: %v, 新規: %v", result.MatchedIDs, result.NewTagNames)

	// "OpenClaw Releases" feed title → "Release" group → matches existing
	hasRelease := false
	for _, id := range result.MatchedIDs {
		if id == "tag-1" {
			hasRelease = true
		}
	}
	if !hasRelease {
		t.Error("expected Release tag from feed title grouping")
	}
}

func TestMatchTags_NoGroupMatch_FeedTitleAsIs(t *testing.T) {
	existing := makeExistingTags("AI")

	a := Article{
		Title:     "Weekly digest #42",
		FeedTitle: "My Custom Newsletter",
	}

	result := matchTags(a, existing)

	t.Logf("記事: %q, フィード: %q", a.Title, a.FeedTitle)
	t.Logf("マッチ: %v, 新規: %v", result.MatchedIDs, result.NewTagNames)

	// Nothing matches → feed title as-is
	if len(result.NewTagNames) != 1 || result.NewTagNames[0] != "My Custom Newsletter" {
		t.Errorf("expected feed title as fallback, got %v", result.NewTagNames)
	}
}

func TestMatchTags_DeduplicatesGroups(t *testing.T) {
	existing := makeExistingTags("Security")

	a := Article{
		Title:      "CVE-2026-1234: セキュリティ脆弱性",
		Categories: []string{"security", "vulnerability"},
		FeedTitle:  "Security Feed",
	}

	result := matchTags(a, existing)

	t.Logf("記事: %q", a.Title)
	t.Logf("マッチ: %v, 新規: %v", result.MatchedIDs, result.NewTagNames)

	// "security" → Security, "vulnerability" → Security, title also has Security keywords
	// All should dedup to one Security tag
	secCount := 0
	for _, id := range result.MatchedIDs {
		if id == "tag-1" {
			secCount++
		}
	}
	if secCount != 1 {
		t.Errorf("expected exactly 1 Security match (deduped), got %d", secCount)
	}
}

// Integration test with multiple feeds
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

	existing := makeExistingTags("AI", "Security", "Cloud", "Release", "Programming")

	var articles []Article
	poster := func(a Article) error {
		articles = append(articles, a)
		return nil
	}

	FetchAndPost(db, "default-ch", poster)

	t.Logf("\n=== 投稿された記事とタグ選択結果 (グルーピングあり) ===\n")
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
}
