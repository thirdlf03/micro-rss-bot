package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testHTMLWithRSSLink = `<!DOCTYPE html>
<html>
<head>
  <link rel="alternate" type="application/rss+xml" title="Blog" href="/feed.xml">
</head>
<body><p>Hello</p></body>
</html>`

const testHTMLWithAtomLink = `<!DOCTYPE html>
<html>
<head>
  <link rel="alternate" type="application/atom+xml" title="Blog" href="/atom.xml">
</head>
<body><p>Hello</p></body>
</html>`

const testHTMLNoFeed = `<!DOCTYPE html>
<html><head><title>No feed</title></head><body><p>Hello</p></body></html>`

func TestDiscoverFromHTML_LinkTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(testHTMLWithRSSLink))
		case "/feed.xml":
			w.Header().Set("Content-Type", "application/xml")
			w.Write([]byte(testRSS))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	result, err := discoverFromHTML(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if result != srv.URL+"/feed.xml" {
		t.Errorf("expected %s/feed.xml, got %s", srv.URL, result)
	}
}

func TestDiscoverFromHTML_AtomLinkTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(testHTMLWithAtomLink))
		case "/atom.xml":
			w.Header().Set("Content-Type", "application/xml")
			w.Write([]byte(testRSS))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	result, err := discoverFromHTML(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if result != srv.URL+"/atom.xml" {
		t.Errorf("expected %s/atom.xml, got %s", srv.URL, result)
	}
}

func TestDiscoverFromHTML_StandardPaths(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(testHTMLNoFeed))
		case "/feed":
			w.Header().Set("Content-Type", "application/xml")
			w.Write([]byte(testRSS))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	result, err := discoverFromHTML(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if result != srv.URL+"/feed" {
		t.Errorf("expected %s/feed, got %s", srv.URL, result)
	}
}

func TestDiscoverFromHTML_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(testHTMLNoFeed))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	result, err := discoverFromHTML(srv.URL)
	if err == nil {
		t.Errorf("expected error, got result: %s", result)
	}
}

func TestDiscoverFromHTML_DirectRSSURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(testRSS))
	}))
	defer srv.Close()

	result, err := discoverFromHTML(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if result != srv.URL {
		t.Errorf("expected %s, got %s", srv.URL, result)
	}
}

func TestDiscoverFromRSSBridge_FullFlow(t *testing.T) {
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(testRSS))
	}))
	defer feedSrv.Close()

	bridgeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fmt.Sprintf(`[{"url":"%s"}]`, feedSrv.URL)))
	}))
	defer bridgeSrv.Close()

	result, err := discoverFromRSSBridge("https://example.com/blog", bridgeSrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if result != feedSrv.URL {
		t.Errorf("expected %s, got %s", feedSrv.URL, result)
	}
}

func TestDiscoverFromRSSBridge_NoResult(t *testing.T) {
	bridgeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer bridgeSrv.Close()

	_, err := discoverFromRSSBridge("https://example.com/blog", bridgeSrv.URL)
	if err == nil {
		t.Error("expected error for empty result")
	}
}

func TestExtractAnchorSnippets(t *testing.T) {
	html := `<html><body>
		<script>var x = 1;</script>
		<nav><a href="/about">About</a></nav>
		<div class="posts">
			<article><a href="/posts/1" class="post-link">First Post</a></article>
			<article><a href="/posts/2" class="post-link">Second Post</a></article>
		</div>
		<a href="#">Skip</a>
		<a href="javascript:void(0)">JS</a>
	</body></html>`

	snippets := extractAnchorSnippets(html)
	if len(snippets) != 3 {
		t.Errorf("expected 3 snippets, got %d: %v", len(snippets), snippets)
	}
}

func TestBuildCssSelectorBridgeURL(t *testing.T) {
	result := buildCssSelectorBridgeURL(
		"https://example.com/blog",
		"a.post-link",
		"h2.title",
		"",
		"http://localhost:3000",
	)
	if !strings.Contains(result, "bridge=CssSelectorBridge") {
		t.Error("expected CssSelectorBridge in URL")
	}
	if !strings.Contains(result, "home_page=") {
		t.Error("expected home_page in URL")
	}
	if !strings.Contains(result, "url_selector=") {
		t.Error("expected url_selector in URL")
	}
}

func TestDiscoverFromLLM(t *testing.T) {
	geminiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"candidates": [{
				"content": {
					"parts": [{
						"text": "{\"url_selector\": \"a.post-link\", \"title_selector\": null, \"content_selector\": null}"
					}]
				}
			}]
		}`))
	}))
	defer geminiSrv.Close()

	bridgeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("bridge") == "CssSelectorBridge" {
			w.Header().Set("Content-Type", "application/xml")
			w.Write([]byte(testRSS))
			return
		}
		http.NotFound(w, r)
	}))
	defer bridgeSrv.Close()

	blogSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body>
			<a href="/posts/1" class="post-link">Post 1</a>
			<a href="/posts/2" class="post-link">Post 2</a>
		</body></html>`))
	}))
	defer blogSrv.Close()

	origEndpoint := geminiEndpoint
	geminiEndpoint = geminiSrv.URL + "/"
	defer func() { geminiEndpoint = origEndpoint }()

	result, err := discoverFromLLM(blogSrv.URL, bridgeSrv.URL, "test-api-key")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "CssSelectorBridge") {
		t.Errorf("expected CssSelectorBridge URL, got %s", result)
	}
}

func TestDiscoverFeed_FullPipeline(t *testing.T) {
	blogSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(testHTMLNoFeed))
	}))
	defer blogSrv.Close()

	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(testRSS))
	}))
	defer feedSrv.Close()

	bridgeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fmt.Sprintf(`[{"url":"%s"}]`, feedSrv.URL)))
	}))
	defer bridgeSrv.Close()

	t.Setenv("RSS_BRIDGE_URL", bridgeSrv.URL)
	t.Setenv("GEMINI_API_KEY", "")

	var stages []int
	progress := func(stage int, msg string) {
		stages = append(stages, stage)
	}

	result, err := DiscoverFeed(blogSrv.URL, progress)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stage != 2 {
		t.Errorf("expected Stage 2, got Stage %d", result.Stage)
	}
	if result.Title != "Test Feed" {
		t.Errorf("expected title 'Test Feed', got '%s'", result.Title)
	}
	if len(stages) < 2 || stages[0] != 1 || stages[1] != 2 {
		t.Errorf("expected stages [1,2], got %v", stages)
	}
}

func TestDiscoverFeed_DirectRSSURL(t *testing.T) {
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(testRSS))
	}))
	defer feedSrv.Close()

	progress := func(stage int, msg string) {}

	result, err := DiscoverFeed(feedSrv.URL, progress)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stage != 1 {
		t.Errorf("expected Stage 1, got Stage %d", result.Stage)
	}
}
