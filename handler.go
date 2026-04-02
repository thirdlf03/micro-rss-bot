package main

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/mmcdole/gofeed"
)

var commands = []*discordgo.ApplicationCommand{
	{
		Name:        "rss",
		Description: "RSS feed management",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "add",
				Description: "Add a new RSS feed",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:        "url",
						Description: "Feed URL",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
					},
				},
			},
			{
				Name:        "list",
				Description: "List all feeds",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "edit",
				Description: "Edit a feed URL",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:        "id",
						Description: "Feed ID",
						Type:        discordgo.ApplicationCommandOptionInteger,
						Required:    true,
					},
					{
						Name:        "url",
						Description: "New feed URL",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
					},
				},
			},
			{
				Name:        "delete",
				Description: "Delete a feed",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:        "id",
						Description: "Feed ID",
						Type:        discordgo.ApplicationCommandOptionInteger,
						Required:    true,
					},
				},
			},
			{
				Name:        "channel",
				Description: "Set the posting channel",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:        "target",
						Description: "Channel to post to",
						Type:        discordgo.ApplicationCommandOptionChannel,
						Required:    true,
					},
				},
			},
			{
				Name:        "interval",
				Description: "Set polling interval in minutes",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:        "minutes",
						Description: "Interval (1-1440)",
						Type:        discordgo.ApplicationCommandOptionInteger,
						Required:    true,
						MinValue:    floatPtr(1),
						MaxValue:    1440,
					},
				},
			},
		},
	},
}

func floatPtr(f float64) *float64 { return &f }

type Handler struct {
	db            *sql.DB
	resetInterval chan time.Duration
}

func NewHandler(db *sql.DB, resetInterval chan time.Duration) *Handler {
	return &Handler{db: db, resetInterval: resetInterval}
}

func (h *Handler) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	data := i.ApplicationCommandData()
	if data.Name != "rss" || len(data.Options) == 0 {
		return
	}

	sub := data.Options[0]
	var content string

	switch sub.Name {
	case "add":
		content = h.handleAdd(sub)
	case "list":
		content = h.handleList()
	case "edit":
		content = h.handleEdit(sub)
	case "delete":
		content = h.handleDelete(sub)
	case "channel":
		content = h.handleChannel(sub)
	case "interval":
		content = h.handleInterval(sub)
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: content},
	})
}

func (h *Handler) handleAdd(sub *discordgo.ApplicationCommandInteractionDataOption) string {
	url := sub.Options[0].StringValue()

	fp := gofeed.NewParser()
	feed, err := fp.ParseURL(url)
	if err != nil {
		return fmt.Sprintf("❌ フィードを取得できませんでした: %v", err)
	}

	id, err := AddFeed(h.db, url, feed.Title)
	if err != nil {
		return fmt.Sprintf("❌ 追加に失敗しました: %v", err)
	}

	// 既存記事を既読にして初回大量投稿を防ぐ
	for _, item := range feed.Items {
		guid := item.GUID
		if guid == "" {
			guid = item.Link
		}
		MarkArticleSeen(h.db, id, guid)
	}

	return fmt.Sprintf("✅ `%s` を追加しました (ID: %d)", feed.Title, id)
}

func (h *Handler) handleList() string {
	feeds, err := ListFeeds(h.db)
	if err != nil {
		return fmt.Sprintf("❌ エラー: %v", err)
	}
	if len(feeds) == 0 {
		return "フィードが登録されていません"
	}
	msg := ""
	for _, f := range feeds {
		msg += fmt.Sprintf("#%d | %s | %s\n", f.ID, f.Title, f.URL)
	}
	return msg
}

func (h *Handler) handleEdit(sub *discordgo.ApplicationCommandInteractionDataOption) string {
	id := sub.Options[0].IntValue()
	url := sub.Options[1].StringValue()

	fp := gofeed.NewParser()
	feed, err := fp.ParseURL(url)
	if err != nil {
		return fmt.Sprintf("❌ フィードを取得できませんでした: %v", err)
	}

	if err := EditFeed(h.db, id, url, feed.Title); err != nil {
		return fmt.Sprintf("❌ 更新に失敗しました: %v", err)
	}
	return fmt.Sprintf("✅ フィード #%d を `%s` に更新しました", id, feed.Title)
}

func (h *Handler) handleDelete(sub *discordgo.ApplicationCommandInteractionDataOption) string {
	id := sub.Options[0].IntValue()
	if err := DeleteFeed(h.db, id); err != nil {
		return fmt.Sprintf("❌ %v", err)
	}
	return fmt.Sprintf("🗑 フィード #%d を削除しました", id)
}

func (h *Handler) handleChannel(sub *discordgo.ApplicationCommandInteractionDataOption) string {
	ch := sub.Options[0].ChannelValue(nil)
	if err := SetConfig(h.db, "channel_id", ch.ID); err != nil {
		return fmt.Sprintf("❌ %v", err)
	}
	return fmt.Sprintf("📢 投稿先を <#%s> に設定しました", ch.ID)
}

func (h *Handler) handleInterval(sub *discordgo.ApplicationCommandInteractionDataOption) string {
	minutes := sub.Options[0].IntValue()
	if err := SetConfig(h.db, "interval_minutes", strconv.FormatInt(minutes, 10)); err != nil {
		return fmt.Sprintf("❌ %v", err)
	}
	h.resetInterval <- time.Duration(minutes) * time.Minute
	return fmt.Sprintf("⏱ ポーリング間隔を %d分 に変更しました", minutes)
}

func RegisterCommands(s *discordgo.Session, h *Handler) {
	for _, cmd := range commands {
		_, err := s.ApplicationCommandCreate(s.State.User.ID, "", cmd)
		if err != nil {
			log.Printf("register command %s: %v", cmd.Name, err)
		}
	}
}
