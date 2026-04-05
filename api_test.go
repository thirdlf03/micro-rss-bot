package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func setupAPIServer(t *testing.T) (*APIServer, *httptest.Server) {
	t.Helper()
	db := setupTestDB(t)
	api := NewAPIServer(db)
	srv := httptest.NewServer(api.mux)
	t.Cleanup(srv.Close)
	return api, srv
}

func TestAPIListFeedsEmpty(t *testing.T) {
	_, srv := setupAPIServer(t)

	resp, err := http.Get(srv.URL + "/api/feeds")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	feeds := result["feeds"]
	if feeds != nil {
		t.Errorf("expected nil feeds, got %v", feeds)
	}
}

func TestAPICRUDFeeds(t *testing.T) {
	_, srv := setupAPIServer(t)

	// POST - add feed
	body := `{"url":"https://example.com/feed","title":"Test Feed"}`
	resp, err := http.Post(srv.URL+"/api/feeds", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}

	// GET - list feeds
	resp, err = http.Get(srv.URL + "/api/feeds")
	if err != nil {
		t.Fatal(err)
	}
	var listResult map[string]any
	json.NewDecoder(resp.Body).Decode(&listResult)
	resp.Body.Close()
	feeds := listResult["feeds"].([]any)
	if len(feeds) != 1 {
		t.Errorf("expected 1 feed, got %d", len(feeds))
	}

	// GET - single feed
	resp, err = http.Get(srv.URL + "/api/feeds/1")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// PUT - edit feed
	editBody := `{"url":"https://example.com/feed2","title":"Updated"}`
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/feeds/1", bytes.NewBufferString(editBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// DELETE
	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/api/feeds/1", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Verify deleted
	resp, _ = http.Get(srv.URL + "/api/feeds")
	var afterDelete map[string]any
	json.NewDecoder(resp.Body).Decode(&afterDelete)
	resp.Body.Close()
	if afterDelete["feeds"] != nil {
		t.Errorf("expected no feeds after delete")
	}
}

func TestAPIArticleSearch(t *testing.T) {
	api, srv := setupAPIServer(t)

	AddFeed(api.db, "https://example.com/feed", "Test", "", "")
	MarkArticleSeen(api.db, 1, "guid-1", "https://example.com/1", "Go言語入門")
	MarkArticleSeen(api.db, 1, "guid-2", "https://example.com/2", "Rustプログラミング")
	MarkArticleSeen(api.db, 1, "guid-3", "https://example.com/3", "Go並行処理")

	// Search for "Go"
	resp, err := http.Get(srv.URL + "/api/articles/search?q=Go")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	total := int(result["total"].(float64))
	if total != 2 {
		t.Errorf("expected 2 results for 'Go', got %d", total)
	}
}

func TestAPIArticlesList(t *testing.T) {
	api, srv := setupAPIServer(t)

	AddFeed(api.db, "https://example.com/feed", "Test", "", "")
	MarkArticleSeen(api.db, 1, "guid-1", "https://example.com/1", "Article 1")
	MarkArticleSeen(api.db, 1, "guid-2", "https://example.com/2", "Article 2")

	resp, err := http.Get(srv.URL + "/api/articles?limit=1&offset=0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	articles := result["articles"].([]any)
	if len(articles) != 1 {
		t.Errorf("expected 1 article with limit=1, got %d", len(articles))
	}
	total := int(result["total"].(float64))
	if total != 2 {
		t.Errorf("expected total=2, got %d", total)
	}
}

func TestAPIAuth(t *testing.T) {
	t.Setenv("API_TOKEN", "secret123")

	_, srv := setupAPIServer(t)

	// Without token
	resp, err := http.Get(srv.URL + "/api/feeds")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}

	// With token
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/feeds", nil)
	req.Header.Set("Authorization", "Bearer secret123")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 with token, got %d", resp.StatusCode)
	}
}
