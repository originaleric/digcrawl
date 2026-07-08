package asrprov

import (
	"context"
	"encoding/json"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/asr"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/executor"
	"github.com/originaleric/digcrawl/runtime/registry"
)

const ToolTranscribe = "asr.transcribe"

type transcribeInput struct {
	Path           string `json:"path"`
	Provider       string `json:"provider,omitempty"`
	Model          string `json:"model,omitempty"`
	Language       string `json:"language,omitempty"`
	Prompt         string `json:"prompt,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
}

// TranscribeEntry returns registry entry for asr.transcribe.
func TranscribeEntry(cfg config.ASRConfig, allowedReadPaths []string) registry.Entry {
	return registry.Entry{
		Descriptor: protocol.ToolDescriptor{
			Name:                 ToolTranscribe,
			Description:          "Extract audio with ffmpeg and transcribe local media using API or local ASR.",
			InputSchema:          registry.MustSchema(transcribeInputSchema()),
			OutputSchema:         registry.MustSchema(transcribeOutputSchema()),
			Capabilities:         []string{"media.ffmpeg", "asr", "file.read"},
			Risk:                 "local_file",
			RequiresUserApproval: true,
		},
		Handler: func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			in, err := executor.DecodeInput[transcribeInput](call)
			if err != nil {
				return nil, nil, err
			}
			if err := executor.ValidateReadPath(in.Path, allowedReadPaths); err != nil {
				return nil, nil, err
			}
			result, err := asr.Transcribe(ctx, in.Path, cfg, asr.Options{
				Provider:       in.Provider,
				Model:          in.Model,
				Language:       in.Language,
				Prompt:         in.Prompt,
				ResponseFormat: in.ResponseFormat,
			})
			if err != nil {
				return nil, nil, err
			}
			var out map[string]any
			data, _ := json.Marshal(result)
			_ = json.Unmarshal(data, &out)
			return out, nil, nil
		},
	}
}

func transcribeInputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"path"},
		"properties": map[string]any{
			"path":            map[string]string{"type": "string"},
			"provider":        map[string]any{"type": "string", "enum": []string{"api", "local", "stub"}},
			"model":           map[string]string{"type": "string"},
			"language":        map[string]string{"type": "string"},
			"prompt":          map[string]string{"type": "string"},
			"response_format": map[string]any{"type": "string", "enum": []string{"verbose_json", "json", "text", "srt", "vtt"}},
		},
	}
}

func transcribeOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text":        map[string]string{"type": "string"},
			"segments":    map[string]any{"type": "array"},
			"language":    map[string]string{"type": "string"},
			"duration":    map[string]string{"type": "number"},
			"provider":    map[string]string{"type": "string"},
			"model":       map[string]string{"type": "string"},
			"audio":       map[string]string{"type": "object"},
			"captured_at": map[string]string{"type": "string"},
		},
	}
}
