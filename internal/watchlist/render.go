package watchlist

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/originaleric/digcrawl/internal/notify"
	"github.com/originaleric/digcrawl/internal/summary"
)

const (
	maxNotificationTitleRunes       = 80
	maxNotificationOverallRunes     = 240
	maxNotificationItemSummaryRunes = 360
	maxNotificationHighlightRunes   = 80
	maxNotificationHighlights       = 3
	maxNotificationActionRunes      = 160
)

func RenderNotification(res *summary.Result) notify.Message {
	if res == nil {
		return notify.Message{Title: "DigCrawl Watchlist 更新", Text: "发现新内容。"}
	}
	var b strings.Builder
	title := trimNotificationText(res.DigestTitle, maxNotificationTitleRunes)
	if title == "" {
		title = "DigCrawl Watchlist 更新"
	}
	b.WriteString(title)
	b.WriteString("\n\n")
	if strings.TrimSpace(res.OverallSummary) != "" {
		b.WriteString("总体摘要：\n")
		b.WriteString(trimNotificationText(res.OverallSummary, maxNotificationOverallRunes))
		b.WriteString("\n\n")
	}
	for i, item := range res.Items {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, trimNotificationText(fallback(item.Title, item.URL), maxNotificationTitleRunes)))
		if item.Importance != "" {
			b.WriteString("重要性：")
			b.WriteString(item.Importance)
			b.WriteString("\n")
		}
		if item.Summary != "" {
			b.WriteString("摘要：")
			b.WriteString(trimNotificationText(item.Summary, maxNotificationItemSummaryRunes))
			b.WriteString("\n")
		}
		if len(item.Highlights) > 0 {
			b.WriteString("亮点：\n")
			limit := len(item.Highlights)
			if limit > maxNotificationHighlights {
				limit = maxNotificationHighlights
			}
			for _, h := range item.Highlights[:limit] {
				if strings.TrimSpace(h) != "" {
					b.WriteString("- ")
					b.WriteString(trimNotificationText(h, maxNotificationHighlightRunes))
					b.WriteString("\n")
				}
			}
		}
		if item.URL != "" {
			b.WriteString("链接：")
			b.WriteString(item.URL)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if res.RecommendedAction != "" {
		b.WriteString("建议：")
		b.WriteString(trimNotificationText(res.RecommendedAction, maxNotificationActionRunes))
		b.WriteString("\n")
	}
	if res.Error != "" {
		b.WriteString("\n诊断：已使用兜底摘要，原因：")
		b.WriteString(res.Error)
		b.WriteString("\n")
	}
	return notify.Message{Title: title, Text: strings.TrimSpace(b.String())}
}

func trimNotificationText(s string, max int) string {
	s = compactNotificationWhitespace(strings.TrimSpace(s))
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	if max < 20 {
		return string(r[:max])
	}
	return strings.TrimSpace(string(r[:max-8])) + "...(截断)"
}

func compactNotificationWhitespace(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return strings.Join(fields, " ")
}

func fallback(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return "(untitled)"
}
