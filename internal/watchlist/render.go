package watchlist

import (
	"fmt"
	"strings"

	"github.com/originaleric/digcrawl/internal/notify"
	"github.com/originaleric/digcrawl/internal/summary"
)

func RenderNotification(res *summary.Result) notify.Message {
	if res == nil {
		return notify.Message{Title: "DigCrawl Watchlist 更新", Text: "发现新内容。"}
	}
	var b strings.Builder
	title := strings.TrimSpace(res.DigestTitle)
	if title == "" {
		title = "DigCrawl Watchlist 更新"
	}
	b.WriteString(title)
	b.WriteString("\n\n")
	if strings.TrimSpace(res.OverallSummary) != "" {
		b.WriteString("总体摘要：\n")
		b.WriteString(strings.TrimSpace(res.OverallSummary))
		b.WriteString("\n\n")
	}
	for i, item := range res.Items {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, fallback(item.Title, item.URL)))
		if item.Importance != "" {
			b.WriteString("重要性：")
			b.WriteString(item.Importance)
			b.WriteString("\n")
		}
		if item.Summary != "" {
			b.WriteString("摘要：")
			b.WriteString(item.Summary)
			b.WriteString("\n")
		}
		if len(item.Highlights) > 0 {
			b.WriteString("亮点：\n")
			for _, h := range item.Highlights {
				if strings.TrimSpace(h) != "" {
					b.WriteString("- ")
					b.WriteString(strings.TrimSpace(h))
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
		b.WriteString(res.RecommendedAction)
		b.WriteString("\n")
	}
	if res.Error != "" {
		b.WriteString("\n诊断：已使用兜底摘要，原因：")
		b.WriteString(res.Error)
		b.WriteString("\n")
	}
	return notify.Message{Title: title, Text: strings.TrimSpace(b.String())}
}

func fallback(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return "(untitled)"
}
