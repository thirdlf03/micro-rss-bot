package main

import (
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func formatRelease(a Article) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🚀 **%s** リリース\n", a.Title))
	sb.WriteString(a.Link)
	sb.WriteString("\n")

	if strings.Contains(strings.ToLower(a.Content), "breaking") {
		sb.WriteString("⚠️ **Breaking Changes あり**\n")
	}

	summary := stripHTML(a.Content)
	if len(summary) > 300 {
		summary = summary[:300] + "..."
	}
	if summary != "" {
		sb.WriteString("```\n" + summary + "\n```")
	}

	return sb.String()
}

func stripHTML(s string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(s))
	if err != nil {
		return s
	}
	return strings.TrimSpace(doc.Text())
}
