package runtime

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform/wechatchannels"
	"github.com/originaleric/digcrawl/protocol"
	browserprov "github.com/originaleric/digcrawl/provider/browser"
	platformprov "github.com/originaleric/digcrawl/provider/platform"
	"github.com/originaleric/digcrawl/skill"
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
	var skillCfg config.SkillConfig
	var rootOCR config.OCRConfig
	videoDownloadSeconds := 0
	imageDownloadSeconds := 0
	if cfg != nil {
		var videoDownloadCfg config.VideoDownloadConfig
		var imageDownloadCfg config.ImageDownloadConfig
		if cfg == config.Get() {
			// The standard Skill profile can be changed by a local configuration
			// tool while MCP workers are being dispatched. Read its mutable
			// section through the synchronized snapshot accessor so the timeout
			// matches one coherent media configuration.
			skillCfg, videoDownloadCfg, imageDownloadCfg = config.SkillMediaSettings()
		} else {
			// Collect and focused tests inject an execution-local config that is
			// immutable for that execution.
			skillCfg = cfg.Skill
			videoDownloadCfg = cfg.VideoDownload
			imageDownloadCfg = cfg.ImageDownload
		}
		if cfg.LocalBrowser.TotalTimeoutSec > 0 {
			browserSeconds = cfg.LocalBrowser.TotalTimeoutSec
		}
		mediaSeconds = skillCfg.Media.TimeoutSecOrDefault()
		maxASRCandidates = skillCfg.Media.MaxASRCandidatesOrDefault()
		skillASRSeconds = skillCfg.ASR.MaxTimeoutSecOrDefault()
		directASRSeconds = cfg.ASR.MaxTimeoutSecOrDefault()
		summarySeconds = skillCfg.Summary.TimeoutSecOrDefault()
		rootOCR = cfg.OCR
		if call != nil {
			provider, _, hasProvider := strings.Cut(strings.ToLower(strings.TrimSpace(call.Tool)), ".")
			if videoProvider := platformprov.VideoDownloadProviderForTool(call.Tool); videoDownloadCfg.EnabledFor(videoProvider) {
				videoDownloadSeconds = videoDownloadCfg.TimeoutSecOrDefault()
			}
			if call.Tool == browserprov.ToolBrowserBrowse {
				provider = "web"
			}
			if hasProvider && config.IsSupportedImageDownloadProvider(provider) && imageDownloadCfg.EnabledFor(provider) {
				imageDownloadSeconds = imageDownloadCfg.TimeoutSecOrDefault()
			}
		}
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
	// Native resolution precedes the optional screenshot and media download.
	// It is independent of the browser invocation's own deadline.
	if call != nil && call.Tool == platformprov.ToolWechatChannelsRead {
		seconds += int(wechatchannels.ShareVideoResolveTimeout / time.Second)
	}
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
	// Direct readers apply the profile's OCR default even without an input flag.
	// Other tools keep their explicit-input budget. Collect can set its own
	// timeout, which Runtime uses before consulting this recommendation.
	ocrCfg, skillOCR := skillCfg.OCR, true
	ocrRequested := input.IncludeOCR
	if call != nil && call.Tool == browserprov.ToolBrowserBrowse {
		ocrCfg, skillOCR = browserprov.DirectOCRConfig(skillCfg, rootOCR, input.IncludeOCR)
		ocrRequested = skillOCR || ocrCfg.EnabledOrDefault()
	} else if call != nil && strings.HasSuffix(call.Tool, ".read") {
		ocrRequested = skill.OCRRequested(skillCfg, input.IncludeOCR)
	}
	if ocrRequested && ocrCfg.EnabledOrDefault() {
		// Image OCR is sequential and bounded by MaxImages.
		ocrSeconds := ocrCfg.TimeoutSecOrDefault()
		seconds += ocrCfg.MaxImagesOrDefault() * ocrSeconds
		// Only Skill enhancement extracts frames from a local video.
		if frames := ocrCfg.VideoFrames; skillOCR && frames.EnabledOrDefault() {
			seconds += mediaSeconds + frames.TimeoutSecOrDefault() + frames.MaxTotalFramesOrDefault()*ocrSeconds
		}
	}
	// Provider retention happens after page extraction and before any optional
	// local enrichment. It has its own user-configured operation deadline, so
	// reserve that complete phase even when include_media was omitted: the
	// retention opt-in itself requests media internally.
	seconds += videoDownloadSeconds + imageDownloadSeconds
	return time.Duration(seconds) * time.Second
}
