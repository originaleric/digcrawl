package platformprov

import (
	"encoding/json"
	"testing"

	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/executor"
)

func TestRedditPostInputPreservesSkillEnhancementFlags(t *testing.T) {
	input, err := json.Marshal(map[string]any{
		"url":             "https://www.reddit.com/r/golang/comments/abc123/post/",
		"include_ocr":     true,
		"include_asr":     true,
		"include_summary": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	in, err := executor.DecodeInput[redditPostReadInput](&protocol.ToolCall{Input: input})
	if err != nil {
		t.Fatal(err)
	}
	if !in.IncludeOCR || !in.IncludeASR || !in.IncludeSummary {
		t.Fatalf("skill enhancement flags were dropped: %+v", in)
	}
}
