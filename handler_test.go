package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

// mockDeleteOption creates a mock sub-option for /rss delete <id>
func mockDeleteOption(id int64) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{
		Name: "delete",
		Options: []*discordgo.ApplicationCommandInteractionDataOption{
			{
				Name:  "id",
				Type:  discordgo.ApplicationCommandOptionInteger,
				Value: float64(id),
			},
		},
	}
}

// mockIntervalOption creates a mock sub-option for /rss interval <minutes>
func mockIntervalOption(minutes int64) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{
		Name: "interval",
		Options: []*discordgo.ApplicationCommandInteractionDataOption{
			{
				Name:  "minutes",
				Type:  discordgo.ApplicationCommandOptionInteger,
				Value: float64(minutes),
			},
		},
	}
}

func TestAddSingleFeed_Success(t *testing.T) {
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(testRSS))
	}))
	defer feedSrv.Close()

	db := setupTestDB(t)
	h := NewHandler(db, make(chan time.Duration, 1))

	var progressMsgs []string
	result := h.addSingleFeed(feedSrv.URL, func(msg string) {
		progressMsgs = append(progressMsgs, msg)
	})

	if !strings.Contains(result, "✅") {
		t.Errorf("expected success, got: %s", result)
	}
	if !strings.Contains(result, "Test Feed") {
		t.Errorf("expected feed title in result, got: %s", result)
	}

	// Verify feed was added to DB
	feeds, _ := ListFeeds(db)
	if len(feeds) != 1 {
		t.Errorf("expected 1 feed in DB, got %d", len(feeds))
	}

	// Verify articles were marked as seen
	seen, _ := IsArticleSeen(db, feeds[0].ID, "guid-1")
	if !seen {
		t.Error("expected article guid-1 to be marked as seen")
	}
}

func TestAddSingleFeed_InvalidURL(t *testing.T) {
	db := setupTestDB(t)
	h := NewHandler(db, make(chan time.Duration, 1))

	result := h.addSingleFeed("http://127.0.0.1:1/nonexistent", func(msg string) {})

	if !strings.Contains(result, "❌") {
		t.Errorf("expected error, got: %s", result)
	}
}

func TestAddSingleFeed_DuplicateFeed(t *testing.T) {
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(testRSS))
	}))
	defer feedSrv.Close()

	db := setupTestDB(t)
	h := NewHandler(db, make(chan time.Duration, 1))

	// Add first time
	h.addSingleFeed(feedSrv.URL, func(msg string) {})

	// Add again — should fail with duplicate error
	result := h.addSingleFeed(feedSrv.URL, func(msg string) {})
	if !strings.Contains(result, "❌") {
		t.Errorf("expected error on duplicate, got: %s", result)
	}
}

func TestHandleList_Empty(t *testing.T) {
	db := setupTestDB(t)
	h := NewHandler(db, make(chan time.Duration, 1))

	result := h.handleList()
	if result != "フィードが登録されていません" {
		t.Errorf("expected empty message, got: %s", result)
	}
}

func TestHandleList_WithFeeds(t *testing.T) {
	db := setupTestDB(t)
	h := NewHandler(db, make(chan time.Duration, 1))

	AddFeed(db, "https://example.com/feed", "Example")
	AddFeed(db, "https://other.com/rss", "Other")

	result := h.handleList()
	if !strings.Contains(result, "Example") || !strings.Contains(result, "Other") {
		t.Errorf("expected both feeds listed, got: %s", result)
	}
}

func TestHandleInterval(t *testing.T) {
	db := setupTestDB(t)
	resetCh := make(chan time.Duration, 1)
	h := NewHandler(db, resetCh)

	sub := mockIntervalOption(10)
	result := h.handleInterval(sub)

	if !strings.Contains(result, "10分") {
		t.Errorf("expected 10 minutes in result, got: %s", result)
	}

	select {
	case d := <-resetCh:
		if d != 10*time.Minute {
			t.Errorf("expected 10m duration, got %v", d)
		}
	default:
		t.Error("expected duration sent to channel")
	}
}

func TestHandleDelete_NotFound(t *testing.T) {
	db := setupTestDB(t)
	h := NewHandler(db, make(chan time.Duration, 1))

	sub := mockDeleteOption(999)
	result := h.handleDelete(sub)

	if !strings.Contains(result, "❌") {
		t.Errorf("expected error, got: %s", result)
	}
}

func TestHandleDelete_Success(t *testing.T) {
	db := setupTestDB(t)
	h := NewHandler(db, make(chan time.Duration, 1))
	AddFeed(db, "https://example.com/feed", "Example")

	sub := mockDeleteOption(1)
	result := h.handleDelete(sub)

	if !strings.Contains(result, "🗑") {
		t.Errorf("expected delete confirmation, got: %s", result)
	}

	feeds, _ := ListFeeds(db)
	if len(feeds) != 0 {
		t.Errorf("expected 0 feeds after delete, got %d", len(feeds))
	}
}
