package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
)

// DiscoverResult holds the discovered feed URL, title, and which stage found it.
type DiscoverResult struct {
	FeedURL string
	Title   string
	Stage   int
}

// ProgressFunc reports progress to the caller.
type ProgressFunc func(stage int, message string)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// standardPaths are common RSS feed paths to probe.
var standardPaths = []string{"/feed", "/rss", "/rss.xml", "/atom.xml", "/feed.xml", "/index.xml", "/feed/atom", "/feed/rss"}

// DiscoverFeed tries 3 stages to find an RSS feed URL.
func DiscoverFeed(rawURL string, progress ProgressFunc) (*DiscoverResult, error) {
	// Stage 1
	progress(1, "🔍 Stage 1: RSSリンクを探索中...")
	feedURL, err := discoverFromHTML(rawURL)
	if err == nil {
		return buildResult(feedURL, 1)
	}

	// Stage 2
	rssBridgeURL := os.Getenv("RSS_BRIDGE_URL")
	if rssBridgeURL != "" {
		progress(2, "🔍 Stage 2: RSS-Bridgeに問い合わせ中...")
		feedURL, err = discoverFromRSSBridge(rawURL, rssBridgeURL)
		if err == nil {
			return buildResult(feedURL, 2)
		}
	}

	// Stage 3
	geminiKey := os.Getenv("GEMINI_API_KEY")
	if rssBridgeURL != "" && geminiKey != "" {
		progress(3, "🔍 Stage 3: LLMでCSSセレクタを推論中...")
		feedURL, err = discoverFromLLM(rawURL, rssBridgeURL, geminiKey)
		if err == nil {
			return buildResult(feedURL, 3)
		}
	}

	return nil, fmt.Errorf("RSSフィードが見つかりませんでした")
}

func buildResult(feedURL string, stage int) (*DiscoverResult, error) {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL(feedURL)
	if err != nil {
		return nil, fmt.Errorf("フィードの検証に失敗: %w", err)
	}
	return &DiscoverResult{FeedURL: feedURL, Title: feed.Title, Stage: stage}, nil
}

// discoverFromHTML fetches the URL, checks if it's already RSS,
// looks for <link rel="alternate"> tags, and probes standard paths.
func discoverFromHTML(rawURL string) (string, error) {
	resp, err := httpClient.Get(rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")

	// If the URL itself is an RSS/Atom feed, return it directly
	if isXMLContentType(ct) {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		fp := gofeed.NewParser()
		if _, err := fp.ParseString(string(body)); err == nil {
			return rawURL, nil
		}
	}

	// Parse HTML and look for <link rel="alternate"> tags
	if isHTMLContentType(ct) {
		doc, err := goquery.NewDocumentFromReader(resp.Body)
		if err != nil {
			return "", err
		}

		base, _ := url.Parse(rawURL)

		var feedURL string
		doc.Find(`link[rel="alternate"]`).Each(func(i int, s *goquery.Selection) {
			if feedURL != "" {
				return
			}
			t, _ := s.Attr("type")
			if t == "application/rss+xml" || t == "application/atom+xml" {
				href, exists := s.Attr("href")
				if exists && href != "" {
					feedURL = resolveURL(base, href)
				}
			}
		})

		if feedURL != "" {
			fp := gofeed.NewParser()
			if _, err := fp.ParseURL(feedURL); err == nil {
				return feedURL, nil
			}
		}
	}

	// Probe standard paths
	base, _ := url.Parse(rawURL)
	for _, path := range standardPaths {
		candidate := base.Scheme + "://" + base.Host + path
		fp := gofeed.NewParser()
		if _, err := fp.ParseURL(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("no RSS feed found")
}

func resolveURL(base *url.URL, href string) string {
	ref, err := url.Parse(href)
	if err != nil {
		return href
	}
	return base.ResolveReference(ref).String()
}

func isXMLContentType(ct string) bool {
	return strings.Contains(ct, "xml") || strings.Contains(ct, "rss") || strings.Contains(ct, "atom")
}

func isHTMLContentType(ct string) bool {
	return strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml")
}

// discoverFromRSSBridge queries an RSS-Bridge instance's findfeed API.
func discoverFromRSSBridge(rawURL, rssBridgeURL string) (string, error) {
	endpoint := fmt.Sprintf("%s/?action=findfeed&url=%s&format=Atom",
		strings.TrimRight(rssBridgeURL, "/"),
		url.QueryEscape(rawURL))

	resp, err := httpClient.Get(endpoint)
	if err != nil {
		return "", fmt.Errorf("RSS-Bridge request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("RSS-Bridge returned status %d", resp.StatusCode)
	}

	var feeds []struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&feeds); err != nil {
		return "", fmt.Errorf("RSS-Bridge response parse failed: %w", err)
	}

	if len(feeds) == 0 || feeds[0].URL == "" {
		return "", fmt.Errorf("RSS-Bridge found no feeds")
	}

	fp := gofeed.NewParser()
	if _, err := fp.ParseURL(feeds[0].URL); err != nil {
		return "", fmt.Errorf("RSS-Bridge feed validation failed: %w", err)
	}

	return feeds[0].URL, nil
}

var geminiEndpoint = "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent?key="

// discoverFromLLM uses Gemini to infer CSS selectors, then builds an RSS-Bridge CssSelectorBridge URL.
func discoverFromLLM(rawURL, rssBridgeURL, geminiKey string) (string, error) {
	return "", fmt.Errorf("not implemented")
}
