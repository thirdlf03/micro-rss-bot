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

	poster := func(title, link string) error {
		chID, _ := GetConfig(db, "channel_id")
		if chID == "" {
			return fmt.Errorf("channel not set")
		}
		msg := fmt.Sprintf("📰 %s\n%s", title, link)
		_, err := dg.ChannelMessageSend(chID, msg)
		return err
	}

	go StartFetcher(ctx, db, poster, time.Duration(minutes)*time.Minute, resetInterval)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down...")
	cancel()
}
