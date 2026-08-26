package runtime

import (
	"encoding/json"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/protocol"
)

const runtimeCallOverheadSeconds = 30

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
	skillASRSeconds := 1800
	directASRSeconds := 1800
	summarySeconds := 60
	ocrSeconds := config.OCRConfig{}.TimeoutSecOrDefault()
	maxOCRImages := config.OCRConfig{}.MaxImagesOrDefault()
	videoFrameSeconds := config.VideoFrameOCRConfig{}.TimeoutSecOrDefault()
	maxVideoFrames := config.VideoFrameOCRConfig{}.MaxTotalFramesOrDefault()
	videoFrameOCREnabled := false
	if cfg != nil {
		if cfg.LocalBrowser.TotalTimeoutSec > 0 {
			browserSeconds = cfg.LocalBrowser.TotalTimeoutSec
		}
		mediaSeconds = cfg.Skill.Media.TimeoutSecOrDefault()
		maxASRCandidates = cfg.Skill.Media.MaxASRCandidatesOrDefault()
		skillASRSeconds = cfg.Skill.ASR.MaxTimeoutSecOrDefault()
		directASRSeconds = cfg.ASR.MaxTimeoutSecOrDefault()
		summarySeconds = cfg.Skill.Summary.TimeoutSecOrDefault()
		ocrSeconds = cfg.Skill.OCR.TimeoutSecOrDefault()
		maxOCRImages = cfg.Skill.OCR.MaxImagesOrDefault()
		videoFrameSeconds = cfg.Skill.OCR.VideoFrames.TimeoutSecOrDefault()
		maxVideoFrames = cfg.Skill.OCR.VideoFrames.MaxTotalFramesOrDefault()
		videoFrameOCREnabled = cfg.Skill.OCR.EnabledOrDefault() && cfg.Skill.OCR.VideoFrames.EnabledOrDefault()
	}

	if call != nil && call.Tool == "asr.transcribe" {
		// Runtime's context begins before the ASR handler establishes its own
		// operation deadline. Reserve the same dispatch margin used by browser
		// calls so an operation that legitimately consumes its full ASR cap is
		// not rejected merely because the two clocks start a few milliseconds
		// apart.
		return time.Duration(directASRSeconds+runtimeCallOverheadSeconds) * time.Second
	}

	seconds := browserSeconds + runtimeCallOverheadSeconds
	var input struct {
		IncludeMedia   bool `json:"include_media"`
		IncludeOCR     bool `json:"include_ocr"`
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
		seconds += maxASRCandidates * (mediaSeconds + skillASRSeconds)
	} else if input.IncludeMedia {
		seconds += mediaSeconds
	}
	if input.IncludeSummary {
		seconds += summarySeconds
	}
	if input.IncludeOCR && cfg != nil && cfg.Skill.OCR.EnabledOrDefault() {
		// Image OCR is sequential and bounded by MaxImages. Video-frame OCR
		// additionally acquires one bounded local video candidate, extracts a
		// bounded frame set, then recognizes each frame with the same provider.
		seconds += maxOCRImages * ocrSeconds
		if videoFrameOCREnabled {
			seconds += mediaSeconds + videoFrameSeconds + maxVideoFrames*ocrSeconds
		}
	}
	return time.Duration(seconds) * time.Second
}
