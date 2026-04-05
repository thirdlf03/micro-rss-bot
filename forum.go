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

	// 1. RSS categories (highest priority)
	if len(a.Categories) > 0 {
		for _, cat := range a.Categories {
			if id, ok := existingTags[strings.ToLower(cat)]; ok {
				tagIDs = append(tagIDs, id)
			} else {
				newTagNames = append(newTagNames, cat)
			}
		}
	} else {
		// 2. Match title keywords against existing tags
		titleLower := strings.ToLower(a.Title)
		for name, id := range existingTags {
			if strings.Contains(titleLower, name) {
				tagIDs = append(tagIDs, id)
			}
		}
		// 3. Fallback: use feed title as tag
		if len(tagIDs) == 0 && a.FeedTitle != "" {
			if id, ok := existingTags[strings.ToLower(a.FeedTitle)]; ok {
				tagIDs = append(tagIDs, id)
			} else {
				newTagNames = append(newTagNames, a.FeedTitle)
			}
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
