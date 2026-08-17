package runtime

import (
	"encoding/json"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/protocol"
)

// RecommendedCallTimeout returns the operational budget for a browser-backed
// tool call. Explicit video enrichment needs more than Runtime's generic
// request budget: page readiness, media download, and local ASR are sequential
// phases with independently configured limits.
//
// Every transport uses Runtime.Execute, so this policy belongs here rather
// than only in MCP. Call.Policy.TimeoutMs still takes precedence when a host
// deliberately supplies its own deadline.
func RecommendedCallTimeout(cfg *config.Config, call *protocol.ToolCall) time.Duration {
	browserSeconds := 90
	mediaSeconds := 30
	asrSeconds := 300
	summarySeconds := 60
	if cfg != nil {
		if cfg.LocalBrowser.TotalTimeoutSec > 0 {
			browserSeconds = cfg.LocalBrowser.TotalTimeoutSec
		}
		if cfg.Skill.Media.TimeoutSec > 0 {
			mediaSeconds = cfg.Skill.Media.TimeoutSec
		}
		asrSeconds = cfg.Skill.ASR.TimeoutSecOrDefault()
		summarySeconds = cfg.Skill.Summary.TimeoutSecOrDefault()
	}

	seconds := browserSeconds + 30
	var input struct {
		IncludeASR     bool `json:"include_asr"`
		IncludeSummary bool `json:"include_summary"`
	}
	if call != nil && len(call.Input) > 0 {
		_ = json.Unmarshal(call.Input, &input)
	}
	// Download and ASR are an explicit sequential phase. A text-only summary
	// must not inherit the local-model budget merely because it is another
	// optional enhancement.
	if input.IncludeASR {
		seconds += mediaSeconds + asrSeconds
	}
	if input.IncludeSummary {
		seconds += summarySeconds
	}
	return time.Duration(seconds) * time.Second
}
