package asrprov

import (
	"context"
	"fmt"

	"github.com/originaleric/digcrawl/config"
	internalasr "github.com/originaleric/digcrawl/internal/asr"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/executor"
	"github.com/originaleric/digcrawl/runtime/registry"
)

const (
	ToolEnableSkillASR      = "skill.asr.enable"
	ToolConfigureSkillMedia = "skill.media.configure"
)

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
		Handler: func(ctx context.Context, _ *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			if !config.IsDefaultSkillProfile(config.Get().SourcePath) {
				return nil, nil, fmt.Errorf("skill ASR can only be enabled from the standard user Skill profile")
			}
			if err := internalasr.CheckAvailable(config.Skill().ASR); err != nil {
				return nil, nil, fmt.Errorf("configured ASR is not ready; install or configure its local dependencies before enabling: %w", err)
			}
			if _, err := config.EnableDefaultSkillASRContext(ctx); err != nil {
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

type configureSkillMediaInput struct {
	RetainDownloadedVideo *bool  `json:"retain_downloaded_video,omitempty"`
	MaxBytes              *int64 `json:"max_bytes,omitempty"`
}

// ConfigureSkillMediaEntry lets a host Agent apply an explicit user request
// to retain provider-downloaded videos or change the existing per-download
// size limit. It is deliberately limited to the standard user Skill profile
// and the existing Skill.Media fields; it neither enables ASR nor chooses a
// downloader, provider, or storage lifecycle.
func ConfigureSkillMediaEntry() registry.Entry {
	return registry.Entry{
		Descriptor: protocol.ToolDescriptor{
			Name:         ToolConfigureSkillMedia,
			Description:  "Update the standard DigCrawl Skill media retention setting or per-download byte limit after an explicit user request.",
			InputSchema:  registry.MustSchema(configureSkillMediaInputSchema()),
			OutputSchema: registry.MustSchema(configureSkillMediaOutputSchema()),
			Capabilities: []string{"config.local", "filesystem.write", "media"},
			Risk:         "local_config",
		},
		Handler: func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			if !config.IsDefaultSkillProfile(config.Get().SourcePath) {
				return nil, nil, fmt.Errorf("Skill media can only be configured from the standard user Skill profile")
			}
			in, err := executor.DecodeInput[configureSkillMediaInput](call)
			if err != nil {
				return nil, nil, err
			}
			updated, err := config.UpdateDefaultSkillMediaContext(ctx, in.RetainDownloadedVideo, in.MaxBytes)
			if err != nil {
				return nil, nil, fmt.Errorf("configure Skill media: %w", err)
			}
			media := updated.Skill.Media
			return map[string]any{
				"media_dir":               media.Dir,
				"media_download_enabled":  media.DownloadMedia,
				"retain_downloaded_video": media.RetainDownloadedVideo,
				"max_bytes":               media.MaxBytes,
				"message":                 skillMediaConfigurationMessage(media),
			}, nil, nil
		},
	}
}

func configureSkillMediaInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"retain_downloaded_video": map[string]string{"type": "boolean"},
			"max_bytes":               map[string]any{"type": "integer", "minimum": 1},
		},
		"additionalProperties": false,
	}
}

func configureSkillMediaOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"media_dir":               map[string]string{"type": "string"},
			"media_download_enabled":  map[string]string{"type": "boolean"},
			"retain_downloaded_video": map[string]string{"type": "boolean"},
			"max_bytes":               map[string]string{"type": "integer"},
			"message":                 map[string]string{"type": "string"},
		},
	}
}

func skillMediaConfigurationMessage(media config.SkillMediaConfig) string {
	if media.DownloadMedia {
		return "Skill media settings were saved for this MCP session and future sessions. They apply when a provider downloads a video."
	}
	return "Skill media settings were saved for this MCP session and future sessions. Enable local video ASR separately before requesting video transcription."
}
