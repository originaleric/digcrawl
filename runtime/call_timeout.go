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
	mediaSeconds := config.SkillMediaConfig{}.TimeoutSecOrDefault()
	maxASRCandidates := config.SkillMediaConfig{}.MaxASRCandidatesOrDefault()
	asrSeconds := 300
	summarySeconds := 60
	if cfg != nil {
		if cfg.LocalBrowser.TotalTimeoutSec > 0 {
			browserSeconds = cfg.LocalBrowser.TotalTimeoutSec
		}
		mediaSeconds = cfg.Skill.Media.TimeoutSecOrDefault()
		maxASRCandidates = cfg.Skill.Media.MaxASRCandidatesOrDefault()
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
	// Runtime sets this deadline before page extraction tells us whether a
	// summary request is for video. IncludeSummary therefore reserves the same
	// finite ASR candidate budget as IncludeASR; text-only summaries return
	// sooner because they never enter that phase.
	if input.IncludeASR || input.IncludeSummary {
		seconds += maxASRCandidates * (mediaSeconds + asrSeconds)
	}
	if input.IncludeSummary {
		seconds += summarySeconds
	}
	return time.Duration(seconds) * time.Second
}
