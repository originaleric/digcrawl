package asrprov

import (
	"context"
	"fmt"

	"github.com/originaleric/digcrawl/config"
	internalasr "github.com/originaleric/digcrawl/internal/asr"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/registry"
)

const ToolEnableSkillASR = "skill.asr.enable"

// EnableSkillASREntry enables the existing local video-ASR opt-in in the
// standard user Skill profile. It intentionally does not select an ASR
// provider or write any credential.
func EnableSkillASREntry() registry.Entry {
	return registry.Entry{
		Descriptor: protocol.ToolDescriptor{
			Name:         ToolEnableSkillASR,
			Description:  "Enable local media download and the configured ASR provider for video transcription in the standard DigCrawl Skill profile.",
			InputSchema:  registry.MustSchema(enableSkillASRInputSchema()),
			OutputSchema: registry.MustSchema(enableSkillASROutputSchema()),
			Capabilities: []string{"config.local", "filesystem.write", "asr"},
			Risk:         "local_config",
		},
		Handler: func(_ context.Context, _ *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			if !config.IsDefaultSkillProfile(config.Get().SourcePath) {
				return nil, nil, fmt.Errorf("skill ASR can only be enabled from the standard user Skill profile")
			}
			if err := internalasr.CheckAvailable(config.Skill().ASR); err != nil {
				return nil, nil, fmt.Errorf("configured ASR is not ready; install or configure its local dependencies before enabling: %w", err)
			}
			if _, err := config.EnableDefaultSkillASR(); err != nil {
				return nil, nil, fmt.Errorf("enable skill ASR: %w", err)
			}
			return map[string]any{
				"media_download_enabled": true,
				"asr_enabled":            true,
				"message":                "Local video ASR is enabled for this MCP session. Retry the original video request.",
			}, nil, nil
		},
	}
}

func enableSkillASRInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func enableSkillASROutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"media_download_enabled": map[string]string{"type": "boolean"},
			"asr_enabled":            map[string]string{"type": "boolean"},
			"message":                map[string]string{"type": "string"},
		},
	}
}
