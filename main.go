package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
)

func main() {
	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "DISCORD_TOKEN is required")
		os.Exit(1)
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./data.db"
	}

	db, err := OpenDB(dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatalf("create session: %v", err)
	}

	resetInterval := make(chan time.Duration, 1)
	handler := NewHandler(db, resetInterval)

	dg.AddHandler(handler.Handle)
	dg.Identify.Intents = discordgo.IntentsGuilds

	if err := dg.Open(); err != nil {
		log.Fatalf("open session: %v", err)
	}
	defer dg.Close()

	RegisterCommands(dg, handler)
	log.Println("micro-rss-bot is running")

	intervalStr, _ := GetConfig(db, "interval_minutes")
	minutes, err := strconv.Atoi(intervalStr)
	if err != nil || minutes < 1 {
		minutes = 5
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	getDefaultChannel := func() string {
		chID, _ := GetConfig(db, "channel_id")
		return chID
	}

	poster := func(a Article) error {
		if a.ChannelID == "" {
			return fmt.Errorf("channel not set")
		}

		// Text channel posting
		var msg string
		switch a.FeedFormat {
		case "release":
			msg = formatRelease(a)
		default:
			msg = fmt.Sprintf("📰 %s\n%s", a.Title, a.Link)
		}
		if _, err := dg.ChannelMessageSend(a.ChannelID, msg); err != nil {
			log.Printf("text post error: %v", err)
		}

		// Forum posting (if configured)
		forumID, _ := GetConfig(db, "forum_channel_id")
		if forumID != "" {
			fp := NewForumPoster(dg, db, forumID)
			if err := fp.Post(a); err != nil {
				log.Printf("forum post error: %v", err)
			}
		}

		return nil
	}

	go StartFetcher(ctx, db, getDefaultChannel, poster, time.Duration(minutes)*time.Minute, resetInterval)

	// Start REST API server if configured
	if apiAddr := os.Getenv("API_ADDR"); apiAddr != "" {
		apiServer := NewAPIServer(db)
		go func() {
			if err := apiServer.Start(apiAddr); err != nil {
				log.Printf("API server error: %v", err)
			}
		}()
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down...")
	cancel()
}
