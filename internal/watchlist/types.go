package watchlist

import (
	"context"
	"time"

	"github.com/originaleric/digcrawl/internal/summary"
)

const (
	StatusDiscovered      = "discovered"
	StatusCollected       = "collected"
	StatusSummarized      = "summarized"
	StatusNotifyPending   = "notify_pending"
	StatusNotified        = "notified"
	StatusCollectFailed   = "collect_failed"
	StatusSummaryFailed   = "summary_failed"
	StatusNotifyFailed    = "notify_failed"
	StatusNotifyFinalFail = "notify_failed_final"
	StatusSkippedLocked   = "skipped_locked"
	StatusSkippedBaseline = "skipped_baseline"
)

type WatchItem struct {
	ID           string
	Platform     string
	Type         string
	URL          string
	Name         string
	Interval     time.Duration
	ScheduleMode string
	ScheduleAt   string
	NotifyTo     string
	Enabled      bool
	Options      map[string]any
}

type WatchSource struct {
	ID           string
	Name         string
	Platform     string
	Type         string
	URL          string
	Enabled      bool
	ScheduleMode string
	Interval     time.Duration
	ScheduleAt   string
	NotifyTo     string
	Options      map[string]any
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type FeedItem struct {
	ID           string
	ContentKey   string
	URL          string
	CanonicalURL string
	Title        string
	Author       string
	PublishedAt  time.Time
	ContentType  string
	Raw          map[string]any
}

type FeedReader interface {
	ListLatest(ctx context.Context, item WatchItem) ([]FeedItem, error)
}

type RunSummary struct {
	RunID                    string
	WatchID                  string
	Discovered               int
	NewItems                 int
	Collected                int
	Summarized               int
	Notified                 int
	SkippedLocked            bool
	NoUpdateSuppressed       bool
	NoUpdateSuppressedReason string
	Errors                   []string
}

type TaskDefinition struct {
	ID             string
	Type           string
	Enabled        bool
	ScheduleMode   string
	ScheduleEvery  time.Duration
	ScheduleAt     string
	SourcePlatform string
	SourceKind     string
	SourceURL      string
	NotifyChannel  string
	ConfigHash     string
}

type CollectedItem struct {
	Feed        FeedItem
	Title       string
	PublishedAt string
	ContentType string
	Text        string
	ASRText     string
	OCRText     string
	Metadata    map[string]any
	Summary     *summary.Result
}

const (
	RecoveryDiscoverSource = "discover_source"
	RecoveryCollectItem    = "collect_item"
)
