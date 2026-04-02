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

var geminiEndpoint = "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash-lite:generateContent?key="

const cssSelectorSystemPrompt = `You extract CSS selectors from HTML to build an RSS feed. Respond with ONLY a JSON object. No markdown, no explanation.`

func buildSelectorUserPrompt(pageURL, anchorData string) string {
	return fmt.Sprintf(`Find CSS selectors to extract articles from this page. I need up to 3 selectors:

1. **url_selector** (required): CSS selector for <a> elements linking to individual blog posts/articles.
2. **title_selector** (optional): CSS selector for elements whose text content is the article title.
3. **content_selector** (optional): CSS selector for elements containing the article summary/description.

Rules:
- For url_selector: select <a> elements that link to individual article pages
- For title_selector: only needed if url_selector links have generic text like "Read more"
- Do NOT select navigation, footer, social media, or category listing links
- Look for patterns: repeated similar structures with href paths like /blog/*, /posts/*, /articles/*
- For href attribute selectors, use *= (contains) NOT ^= (starts-with)

URL: %s

Links on page:
%s

Respond with ONLY JSON: {"url_selector": "...", "title_selector": "..." or null, "content_selector": "..." or null}`, pageURL, anchorData)
}

// extractAnchorSnippets extracts anchor tag info from HTML for LLM analysis.
func extractAnchorSnippets(htmlStr string) []string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		return nil
	}

	doc.Find("script, style, svg, noscript").Remove()

	var snippets []string
	doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		if href == "" || href == "#" || strings.HasPrefix(href, "javascript:") || strings.HasPrefix(href, "mailto:") {
			return
		}

		var ancestry []string
		parent := s.Parent()
		for j := 0; j < 5 && parent.Length() > 0; j++ {
			node := parent.Get(0)
			if node.Data == "body" || node.Data == "html" {
				break
			}
			desc := node.Data
			if id, exists := parent.Attr("id"); exists {
				desc += "#" + id
			}
			if class, exists := parent.Attr("class"); exists && class != "" {
				desc += "." + strings.Join(strings.Fields(class), ".")
			}
			ancestry = append([]string{desc}, ancestry...)
			parent = parent.Parent()
		}

		aDesc := "a"
		if class, exists := s.Attr("class"); exists && class != "" {
			aDesc += "." + strings.Join(strings.Fields(class), ".")
		}

		text := strings.TrimSpace(s.Text())
		if len(text) > 80 {
			text = text[:80]
		}

		line := fmt.Sprintf("%s > %s href=\"%s\" \"%s\"",
			strings.Join(ancestry, " > "), aDesc, href, text)
		snippets = append(snippets, line)
	})

	return snippets
}

// buildCssSelectorBridgeURL constructs an RSS-Bridge CssSelectorBridge URL.
func buildCssSelectorBridgeURL(homePage, urlSelector, titleSelector, contentSelector, rssBridgeURL string) string {
	params := url.Values{}
	params.Set("action", "display")
	params.Set("bridge", "CssSelectorBridge")
	params.Set("home_page", homePage)
	params.Set("url_selector", urlSelector)
	params.Set("format", "Atom")
	if titleSelector != "" {
		params.Set("title_selector", titleSelector)
	}
	if contentSelector != "" {
		params.Set("content_selector", contentSelector)
	}
	return fmt.Sprintf("%s/?%s", strings.TrimRight(rssBridgeURL, "/"), params.Encode())
}

type geminiRequest struct {
	SystemInstruction *geminiContent  `json:"system_instruction,omitempty"`
	Contents          []geminiContent `json:"contents"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// callGemini sends a prompt to the Gemini API and returns the response text.
func callGemini(apiKey, systemPrompt, userPrompt string) (string, error) {
	reqBody := geminiRequest{
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: systemPrompt}},
		},
		Contents: []geminiContent{
			{Parts: []geminiPart{{Text: userPrompt}}},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	resp, err := httpClient.Post(
		geminiEndpoint+apiKey,
		"application/json",
		strings.NewReader(string(bodyBytes)),
	)
	if err != nil {
		return "", fmt.Errorf("Gemini API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Gemini API returned %d: %s", resp.StatusCode, string(body))
	}

	var geminiResp geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return "", fmt.Errorf("Gemini response parse failed: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("Gemini returned empty response")
	}

	return geminiResp.Candidates[0].Content.Parts[0].Text, nil
}

type cssSelectorResult struct {
	URLSelector     string  `json:"url_selector"`
	TitleSelector   *string `json:"title_selector"`
	ContentSelector *string `json:"content_selector"`
}

// discoverFromLLM uses Gemini to infer CSS selectors, then builds an RSS-Bridge CssSelectorBridge URL.
func discoverFromLLM(rawURL, rssBridgeURL, geminiKey string) (string, error) {
	resp, err := httpClient.Get(rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	snippets := extractAnchorSnippets(string(body))
	if len(snippets) == 0 {
		return "", fmt.Errorf("no links found on page")
	}

	if len(snippets) > 200 {
		snippets = snippets[:200]
	}
	anchorData := strings.Join(snippets, "\n")

	llmResult, err := callGemini(geminiKey, cssSelectorSystemPrompt, buildSelectorUserPrompt(rawURL, anchorData))
	if err != nil {
		return "", fmt.Errorf("LLM inference failed: %w", err)
	}

	jsonStart := strings.Index(llmResult, "{")
	jsonEnd := strings.LastIndex(llmResult, "}")
	if jsonStart < 0 || jsonEnd < 0 || jsonEnd <= jsonStart {
		return "", fmt.Errorf("LLM returned no valid JSON")
	}

	var parsed cssSelectorResult
	if err := json.Unmarshal([]byte(llmResult[jsonStart:jsonEnd+1]), &parsed); err != nil {
		return "", fmt.Errorf("LLM response JSON parse failed: %w", err)
	}

	if parsed.URLSelector == "" {
		return "", fmt.Errorf("LLM returned empty url_selector")
	}

	fixSelector := func(s string) string {
		return strings.ReplaceAll(s, "^=", "*=")
	}
	urlSel := fixSelector(parsed.URLSelector)
	titleSel := ""
	if parsed.TitleSelector != nil {
		titleSel = fixSelector(*parsed.TitleSelector)
	}
	contentSel := ""
	if parsed.ContentSelector != nil {
		contentSel = fixSelector(*parsed.ContentSelector)
	}

	bridgeURL := buildCssSelectorBridgeURL(rawURL, urlSel, titleSel, contentSel, rssBridgeURL)

	fp := gofeed.NewParser()
	if _, err := fp.ParseURL(bridgeURL); err != nil {
		return "", fmt.Errorf("CssSelectorBridge feed validation failed: %w", err)
	}

	return bridgeURL, nil
}
