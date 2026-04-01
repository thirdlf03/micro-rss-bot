package main

import (
	"fmt"
	"os"
)

func main() {
	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "DISCORD_TOKEN is required")
		os.Exit(1)
	}
	fmt.Println("micro-rss-bot starting...")
}
