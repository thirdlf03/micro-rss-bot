package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
