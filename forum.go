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
	"Programming": {
		"go", "golang", "rust", "python", "javascript", "typescript",
		"ruby", "java", "kotlin", "swift", "c++", "cpp", "c#", "csharp",
		"haskell", "elixir", "gleam", "zig", "dart", "julia", "scala",
		"php", "perl", "lua", "ocaml", "clojure", "erlang", "nim",
		"programming", "プログラミング", "言語",
	},
	"AI": {
		"ai", "artificial intelligence", "machine learning", "deep learning",
		"llm", "gpt", "claude", "gemini", "openai", "anthropic",
		"copilot", "cursor", "windsurf", "devin", "openclaw",
		"claude code", "codex", "cline", "aider", "continue.dev",
		"neural", "transformer", "diffusion", "rag",
		"langchain", "llamaindex", "hugging face", "huggingface",
		"tensorflow", "pytorch", "onnx", "mlops",
		"stable diffusion", "midjourney", "dall-e",
		"agent", "agentic", "mcp", "tool use",
		"人工知能", "機械学習", "生成ai",
	},
	"Security": {
		"security", "cve", "vulnerability", "exploit", "patch",
		"malware", "ransomware", "phishing", "zero-day", "0day",
		"authentication", "authorization", "oauth", "jwt",
		"encryption", "tls", "ssl", "certificate",
		"脆弱性", "セキュリティ", "攻撃", "不正アクセス",
	},
	"Infrastructure": {
		"linux", "ubuntu", "debian", "fedora", "arch",
		"docker", "kubernetes", "k8s", "podman", "containerd",
		"nginx", "caddy", "traefik", "envoy",
		"terraform", "ansible", "pulumi", "crossplane",
		"systemd", "kernel", "ebpf", "wasm", "webassembly",
		"インフラ", "サーバー構築",
	},
	"Cloud": {
		"cloud", "aws", "gcp", "azure", "oracle cloud",
		"vercel", "cloudflare", "netlify", "fly.io", "railway",
		"lambda", "serverless", "edge computing", "cdn",
		"s3", "ec2", "fargate", "cloud run", "app engine",
		"クラウド",
	},
	"Release": {
		"release", "released", "リリース", "アップデート",
		"update", "upgrade", "version", "changelog",
		"breaking change", "migration", "deprecated",
		"新機能", "バージョン",
	},
	"Database": {
		"database", "db", "sql",
		"postgresql", "postgres", "mysql", "mariadb", "sqlite",
		"redis", "mongodb", "dynamodb", "cassandra",
		"elasticsearch", "opensearch", "clickhouse",
		"supabase", "planetscale", "neon", "turso",
		"データベース",
	},
	"Frontend": {
		"react", "vue", "svelte", "angular", "solid", "qwik", "htmx",
		"next.js", "nextjs", "nuxt", "remix", "astro", "gatsby",
		"tailwind", "css", "html", "dom", "browser",
		"frontend", "フロントエンド", "ui", "ux",
	},
	"Backend": {
		"api", "graphql", "grpc", "rest", "openapi",
		"fastapi", "express", "gin", "echo", "fiber", "hono",
		"django", "rails", "spring", "laravel", "phoenix",
		"microservice", "backend", "バックエンド",
	},
	"DevOps": {
		"ci/cd", "cicd", "github actions", "jenkins", "circleci",
		"devops", "sre", "monitoring", "observability",
		"prometheus", "grafana", "datadog", "sentry",
		"gitops", "argocd", "flux",
	},
	"Mobile": {
		"ios", "android", "flutter", "react native", "expo",
		"swiftui", "jetpack compose", "kotlin multiplatform", "kmp",
		"モバイル", "アプリ開発",
	},
	"Web3": {
		"blockchain", "ethereum", "solana", "web3",
		"smart contract", "solidity", "defi", "nft",
		"ブロックチェーン",
	},
	"OS": {
		"windows", "macos", "freebsd", "nixos", "nix",
		"os", "operating system", "オペレーティングシステム",
	},
	"Game": {
		"unity", "unreal", "godot", "game dev", "gamedev",
		"ゲーム開発", "ゲームエンジン",
	},
}

// containsKeyword checks if text contains a keyword, using word boundary
// matching for short keywords (<=3 chars) to avoid false positives like
// "random" matching "dom" or "postgresql" matching "os".
func containsKeyword(text, kw string) bool {
	idx := strings.Index(text, kw)
	if idx < 0 {
		return false
	}
	// Short keywords need word boundary check
	if len(kw) <= 3 {
		before := idx == 0 || !isAlphaNum(text[idx-1])
		after := idx+len(kw) >= len(text) || !isAlphaNum(text[idx+len(kw)])
		return before && after
	}
	return true
}

func isAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// normalizeToGroup checks if a text contains keywords that map to a group tag.
func normalizeToGroup(text string) []string {
	lower := strings.ToLower(text)
	seen := make(map[string]bool)
	var groups []string
	for group, keywords := range tagGroups {
		for _, kw := range keywords {
			if containsKeyword(lower, kw) {
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
