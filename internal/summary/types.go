package summary

import "context"

// Request is the stable input contract for watchlist summaries.
type Request struct {
	WatchID    string `json:"watch_id"`
	Platform   string `json:"platform"`
	SourceName string `json:"source_name,omitempty"`
	RunTime    string `json:"run_time"`
	Items      []Item `json:"items"`
}

type Item struct {
	ItemID      string         `json:"item_id,omitempty"`
	URL         string         `json:"url"`
	Title       string         `json:"title,omitempty"`
	PublishedAt string         `json:"published_at,omitempty"`
	Text        string         `json:"text,omitempty"`
	ASRText     string         `json:"asr_text,omitempty"`
	OCRText     string         `json:"ocr_text,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// Result is the stable output contract consumed by notification renderers.
type Result struct {
	DigestTitle       string       `json:"digest_title"`
	OverallSummary    string       `json:"overall_summary"`
	Items             []ResultItem `json:"items"`
	RecommendedAction string       `json:"recommended_action,omitempty"`
	Provider          string       `json:"summary_provider,omitempty"`
	Error             string       `json:"summary_error,omitempty"`
}

type ResultItem struct {
	ItemID     string   `json:"item_id,omitempty"`
	Title      string   `json:"title,omitempty"`
	URL        string   `json:"url,omitempty"`
	Summary    string   `json:"summary,omitempty"`
	Highlights []string `json:"highlights,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Importance string   `json:"importance,omitempty"`
	Reason     string   `json:"reason,omitempty"`
}

type Provider interface {
	Summarize(ctx context.Context, req Request) (*Result, error)
}

type InputBudget struct {
	TextChars     int
	ASRChars      int
	OCRChars      int
	MetadataChars int
	MaxInputChars int
}
