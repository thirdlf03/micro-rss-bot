package main

import (
	"database/sql"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

type ForumPoster struct {
	session   *discordgo.Session
	db        *sql.DB
	channelID string

	mu        sync.Mutex
	cachedCh  *discordgo.Channel
	cacheTime time.Time
}

func NewForumPoster(s *discordgo.Session, db *sql.DB, channelID string) *ForumPoster {
	return &ForumPoster{session: s, db: db, channelID: channelID}
}

func (fp *ForumPoster) getChannel() (*discordgo.Channel, error) {
	fp.mu.Lock()
	defer fp.mu.Unlock()

	if fp.cachedCh != nil && time.Since(fp.cacheTime) < 5*time.Minute {
		return fp.cachedCh, nil
	}

	ch, err := fp.session.Channel(fp.channelID)
	if err != nil {
		return nil, err
	}
	fp.cachedCh = ch
	fp.cacheTime = time.Now()
	return ch, nil
}

func (fp *ForumPoster) Post(a Article) error {
	ch, err := fp.getChannel()
	if err != nil {
		return err
	}

	tagIDs := fp.resolveTagIDs(a, ch)

	threadName := a.Title
	if len(threadName) > 100 {
		threadName = threadName[:97] + "..."
	}

	content := a.Link
	if a.FeedFormat == "release" {
		content = formatRelease(a)
	}

	_, err = fp.session.ForumThreadStartComplex(fp.channelID, &discordgo.ThreadStart{
		Name:                threadName,
		AutoArchiveDuration: 1440,
		AppliedTags:         tagIDs,
	}, &discordgo.MessageSend{
		Content: content,
	})
	return err
}

// tagGroups maps keywords to broader group tag names.
// When a category or title contains a keyword, the group name is used instead.
var tagGroups = map[string][]string{
	"Programming": {"go", "golang", "rust", "python", "javascript", "typescript", "ruby", "java", "kotlin", "swift", "c++", "cpp", "haskell", "elixir", "zig", "programming"},
	"AI":            {"ai", "machine learning", "llm", "gpt", "claude", "gemini", "openai", "anthropic", "deep learning", "neural", "transformer"},
	"Security":      {"security", "cve", "vulnerability", "exploit", "patch", "脆弱性", "セキュリティ"},
	"Infrastructure": {"linux", "docker", "kubernetes", "k8s", "nginx", "terraform", "ansible", "インフラ"},
	"Cloud":         {"cloud", "aws", "gcp", "azure", "vercel", "cloudflare", "クラウド"},
	"Release":       {"release", "released", "リリース", "アップデート", "update", "version"},
	"Database":      {"database", "postgresql", "postgres", "mysql", "sqlite", "redis", "mongodb", "データベース", "db"},
	"Frontend":      {"react", "vue", "svelte", "next.js", "nextjs", "nuxt", "css", "html", "frontend", "フロントエンド"},
	"Backend":       {"api", "graphql", "grpc", "rest", "backend", "バックエンド", "サーバー"},
	"DevOps":        {"ci/cd", "cicd", "github actions", "jenkins", "devops", "monitoring", "observability"},
}

// normalizeToGroup checks if a text contains keywords that map to a group tag.
func normalizeToGroup(text string) []string {
	lower := strings.ToLower(text)
	seen := make(map[string]bool)
	var groups []string
	for group, keywords := range tagGroups {
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				if !seen[group] {
					seen[group] = true
					groups = append(groups, group)
				}
				break
			}
		}
	}
	return groups
}

// matchTagResult holds the result of tag matching (before Discord API calls).
type matchTagResult struct {
	MatchedIDs  []string
	NewTagNames []string
}

// matchTags determines which existing tags match and which new tags are needed.
// This is the pure logic portion, separated from Discord API calls for testability.
func matchTags(a Article, existingTags map[string]string) matchTagResult {
	var tagIDs []string
	var newTagNames []string
	seen := make(map[string]bool)

	addTag := func(name string) {
		lower := strings.ToLower(name)
		if seen[lower] {
			return
		}
		seen[lower] = true
		if id, ok := existingTags[lower]; ok {
			tagIDs = append(tagIDs, id)
		} else {
			newTagNames = append(newTagNames, name)
		}
	}

	// 1. RSS categories → normalize to groups
	if len(a.Categories) > 0 {
		for _, cat := range a.Categories {
			groups := normalizeToGroup(cat)
			if len(groups) > 0 {
				for _, g := range groups {
					addTag(g)
				}
			} else {
				addTag(cat)
			}
		}
	}

	// 2. Title keyword grouping (always, as supplemental)
	titleGroups := normalizeToGroup(a.Title)
	for _, g := range titleGroups {
		addTag(g)
	}

	// 3. Fallback: feed title grouping, then feed title as-is
	if len(tagIDs) == 0 && len(newTagNames) == 0 && a.FeedTitle != "" {
		feedGroups := normalizeToGroup(a.FeedTitle)
		if len(feedGroups) > 0 {
			for _, g := range feedGroups {
				addTag(g)
			}
		} else {
			addTag(a.FeedTitle)
		}
	}

	return matchTagResult{MatchedIDs: tagIDs, NewTagNames: newTagNames}
}

func (fp *ForumPoster) resolveTagIDs(a Article, ch *discordgo.Channel) []string {
	existingTags := make(map[string]string)
	for _, t := range ch.AvailableTags {
		existingTags[strings.ToLower(t.Name)] = t.ID
	}

	result := matchTags(a, existingTags)
	tagIDs := result.MatchedIDs

	// Auto-create missing tags via Discord API (up to 20 per forum)
	if len(result.NewTagNames) > 0 && len(ch.AvailableTags)+len(result.NewTagNames) <= 20 {
		updatedTags := ch.AvailableTags
		for _, name := range result.NewTagNames {
			updatedTags = append(updatedTags, discordgo.ForumTag{Name: name})
		}
		updated, err := fp.session.ChannelEdit(fp.channelID, &discordgo.ChannelEdit{
			AvailableTags: &updatedTags,
		})
		if err == nil {
			fp.mu.Lock()
			fp.cachedCh = updated
			fp.cacheTime = time.Now()
			fp.mu.Unlock()

			newExisting := make(map[string]string)
			for _, t := range updated.AvailableTags {
				newExisting[strings.ToLower(t.Name)] = t.ID
			}
			for _, name := range result.NewTagNames {
				if id, ok := newExisting[strings.ToLower(name)]; ok {
					tagIDs = append(tagIDs, id)
				}
			}
		} else {
			log.Printf("failed to create forum tags: %v", err)
		}
	}

	// Discord limit: max 5 tags per thread
	if len(tagIDs) > 5 {
		tagIDs = tagIDs[:5]
	}
	return tagIDs
}
