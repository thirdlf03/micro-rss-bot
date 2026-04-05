package main

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/mmcdole/gofeed"
)

var commands = []*discordgo.ApplicationCommand{
	{
		Name:        "forum",
		Description: "Forum channel management",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "set",
				Description: "Set forum channel for cross-posting",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:        "channel",
						Description: "Forum channel",
						Type:        discordgo.ApplicationCommandOptionChannel,
						Required:    true,
					},
				},
			},
			{
				Name:        "remove",
				Description: "Remove forum channel",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "status",
				Description: "Show current forum channel",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
		},
	},
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
						Description: "Feed URLs (space-separated for multiple)",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
					},
					{
						Name:        "channel",
						Description: "Post to this channel (default: global channel)",
						Type:        discordgo.ApplicationCommandOptionChannel,
						Required:    false,
					},
					{
						Name:        "format",
						Description: "Feed format: default or release",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    false,
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
			{
				Name:        "format",
				Description: "Set feed display format",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:        "id",
						Description: "Feed ID",
						Type:        discordgo.ApplicationCommandOptionInteger,
						Required:    true,
					},
					{
						Name:        "type",
						Description: "Format type: default or release",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
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
	if len(data.Options) == 0 {
		return
	}

	// Handle /forum commands
	if data.Name == "forum" {
		sub := data.Options[0]
		var content string
		switch sub.Name {
		case "set":
			ch := sub.Options[0].ChannelValue(nil)
			if err := SetConfig(h.db, "forum_channel_id", ch.ID); err != nil {
				content = fmt.Sprintf("❌ %v", err)
			} else {
				content = fmt.Sprintf("📋 フォーラム投稿先を <#%s> に設定しました", ch.ID)
			}
		case "remove":
			SetConfig(h.db, "forum_channel_id", "")
			content = "📋 フォーラム投稿を無効化しました"
		case "status":
			forumID, _ := GetConfig(h.db, "forum_channel_id")
			if forumID == "" {
				content = "📋 フォーラム投稿: 未設定"
			} else {
				content = fmt.Sprintf("📋 フォーラム投稿先: <#%s>", forumID)
			}
		}
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: content},
		})
		return
	}

	if data.Name != "rss" {
		return
	}

	sub := data.Options[0]

	// "add" uses deferred response for progress display
	if sub.Name == "add" {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		})
		content := h.handleAdd(sub, func(msg string) {
			s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: &msg,
			})
		})
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: &content,
		})
		return
	}

	var content string
	switch sub.Name {
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
	case "format":
		content = h.handleFormat(sub)
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: content},
	})
}

func (h *Handler) handleAdd(sub *discordgo.ApplicationCommandInteractionDataOption, updateProgress func(string)) string {
	urls := strings.Fields(sub.Options[0].StringValue())

	// Parse optional channel and format
	channelID := ""
	format := ""
	for _, opt := range sub.Options[1:] {
		switch opt.Name {
		case "channel":
			ch := opt.ChannelValue(nil)
			channelID = ch.ID
		case "format":
			format = opt.StringValue()
		}
	}

	if len(urls) == 1 {
		return h.addSingleFeed(urls[0], channelID, format, updateProgress)
	}

	var results []string
	for _, u := range urls {
		results = append(results, h.addSingleFeed(u, channelID, format, updateProgress))
	}
	return strings.Join(results, "\n")
}

func (h *Handler) addSingleFeed(rawURL, channelID, format string, updateProgress func(string)) string {
	progress := func(stage int, msg string) {
		updateProgress(fmt.Sprintf("`%s`\n%s", rawURL, msg))
	}

	result, err := DiscoverFeed(rawURL, progress)
	if err != nil {
		return fmt.Sprintf("❌ `%s`: %v", rawURL, err)
	}

	id, err := AddFeed(h.db, result.FeedURL, result.Title, channelID, format)
	if err != nil {
		return fmt.Sprintf("❌ `%s`: 追加に失敗しました: %v", result.Title, err)
	}

	// 既存記事を既読にして初回大量投稿を防ぐ
	fp := gofeed.NewParser()
	if feed, err := fp.ParseURL(result.FeedURL); err == nil {
		for _, item := range feed.Items {
			guid := item.GUID
			if guid == "" {
				guid = item.Link
			}
			MarkArticleSeen(h.db, id, guid, item.Link, item.Title)
		}
	}

	stageInfo := ""
	if result.Stage > 0 {
		stageInfo = fmt.Sprintf(" (Stage %dで検出)", result.Stage)
	}
	chInfo := ""
	if channelID != "" {
		chInfo = fmt.Sprintf(" → <#%s>", channelID)
	}
	return fmt.Sprintf("✅ `%s` を追加しました (ID: %d)%s%s", result.Title, id, stageInfo, chInfo)
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
		line := fmt.Sprintf("#%d | %s | %s", f.ID, f.Title, f.URL)
		if f.ChannelID != "" {
			line += fmt.Sprintf(" → <#%s>", f.ChannelID)
		}
		if f.Format != "default" {
			line += fmt.Sprintf(" [%s]", f.Format)
		}
		msg += line + "\n"
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

func (h *Handler) handleFormat(sub *discordgo.ApplicationCommandInteractionDataOption) string {
	id := sub.Options[0].IntValue()
	formatType := sub.Options[1].StringValue()
	if formatType != "default" && formatType != "release" {
		return "❌ フォーマットは `default` または `release` を指定してください"
	}
	if err := SetFeedFormat(h.db, id, formatType); err != nil {
		return fmt.Sprintf("❌ %v", err)
	}
	return fmt.Sprintf("✅ フィード #%d のフォーマットを `%s` に変更しました", id, formatType)
}

func RegisterCommands(s *discordgo.Session, h *Handler) {
	for _, cmd := range commands {
		_, err := s.ApplicationCommandCreate(s.State.User.ID, "", cmd)
		if err != nil {
			log.Printf("register command %s: %v", cmd.Name, err)
		}
	}
}
