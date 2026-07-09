package summary

import (
	"context"
	"fmt"
	"strings"
)

type ExtractiveProvider struct {
	MaxSummaryChars int
}

func (p ExtractiveProvider) Summarize(ctx context.Context, req Request) (*Result, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	max := p.MaxSummaryChars
	if max <= 0 {
		max = 320
	}
	res := &Result{
		DigestTitle:    fmt.Sprintf("%s新增 %d 条内容", displaySource(req), len(req.Items)),
		OverallSummary: fmt.Sprintf("本轮监控发现 %d 条新增内容。", len(req.Items)),
		Provider:       "extractive",
	}
	for _, item := range req.Items {
		text := firstNonEmpty(item.Text, item.ASRText, item.OCRText, item.Title)
		ri := ResultItem{
			ItemID:     item.ItemID,
			Title:      item.Title,
			URL:        item.URL,
			Summary:    smartTrim(text, max),
			Importance: "medium",
			Reason:     "规则摘要兜底结果，未使用 LLM 判断。",
		}
		if ri.Summary == "" {
			ri.Summary = item.URL
		}
		res.Items = append(res.Items, ri)
	}
	return res, nil
}

func displaySource(req Request) string {
	if strings.TrimSpace(req.SourceName) != "" {
		return strings.TrimSpace(req.SourceName)
	}
	if strings.TrimSpace(req.Platform) != "" {
		return strings.TrimSpace(req.Platform)
	}
	return "Watchlist"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
