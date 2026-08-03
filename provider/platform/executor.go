package platformprov

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/ocr"
	iplatform "github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/executor"
	"github.com/originaleric/digcrawl/runtime/policy"
	"github.com/originaleric/digcrawl/runtime/registry"
	"github.com/originaleric/digcrawl/skill"
)

type platformReadInput struct {
	URL               string   `json:"url"`
	Format            []string `json:"format,omitempty"`
	IncludeMedia      bool     `json:"include_media,omitempty"`
	IncludeScreenshot bool     `json:"include_screenshot,omitempty"`
	IncludeOCR        bool     `json:"include_ocr,omitempty"`
	IncludeASR        bool     `json:"include_asr,omitempty"`
	IncludeSummary    bool     `json:"include_summary,omitempty"`
	WaitSelector      string   `json:"wait_selector,omitempty"`
	UseCookieDomain   string   `json:"use_cookie_domain,omitempty"`
}

func (in platformReadInput) toPlatformReadInput() iplatform.ReadInput {
	return iplatform.ReadInput{
		URL:               in.URL,
		Format:            in.Format,
		IncludeMedia:      in.IncludeMedia || in.IncludeOCR || in.IncludeASR || in.IncludeSummary,
		IncludeScreenshot: in.IncludeScreenshot,
		WaitSelector:      in.WaitSelector,
		UseCookieDomain:   in.UseCookieDomain,
	}
}

func platformOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"platform":               map[string]string{"type": "string"},
			"content_type":           map[string]string{"type": "string"},
			"source_url":             map[string]string{"type": "string"},
			"canonical_url":          map[string]string{"type": "string"},
			"title":                  map[string]string{"type": "string"},
			"text":                   map[string]string{"type": "string"},
			"markdown":               map[string]string{"type": "string"},
			"author":                 map[string]string{"type": "object"},
			"published_at":           map[string]string{"type": "string"},
			"captured_at":            map[string]string{"type": "string"},
			"media":                  map[string]any{"type": "array"},
			"engagement":             map[string]string{"type": "object"},
			"tags":                   map[string]any{"type": "array"},
			"platform_metadata":      map[string]string{"type": "object"},
			"screenshot_artifact_id": map[string]string{"type": "string"},
			"asr_text":               map[string]string{"type": "string"},
			"summary":                map[string]string{"type": "object"},
		},
	}
}

func platformInputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"url"},
		"properties": map[string]any{
			"url":                map[string]string{"type": "string"},
			"format":             map[string]any{"type": "array", "items": map[string]string{"type": "string"}},
			"include_media":      map[string]any{"type": "boolean"},
			"include_screenshot": map[string]any{"type": "boolean"},
			"include_ocr":        map[string]any{"type": "boolean"},
			"include_asr":        map[string]any{"type": "boolean"},
			"include_summary":    map[string]any{"type": "boolean"},
			"wait_selector":      map[string]string{"type": "string"},
			"use_cookie_domain":  map[string]string{"type": "string"},
		},
	}
}

type platformReader func(ctx context.Context, in iplatform.ReadInput) (*iplatform.Content, error)

func platformReadEntry(
	name, description string,
	capabilities []string,
	defaultDomains []string,
	configDomains []string,
	artStore artifact.Store,
	read platformReader,
) registry.Entry {
	return registry.Entry{
		Descriptor: protocol.ToolDescriptor{
			Name:         name,
			Description:  description,
			InputSchema:  registry.MustSchema(platformInputSchema()),
			OutputSchema: registry.MustSchema(platformOutputSchema()),
			Capabilities: capabilities,
			Risk:         "network",
		},
		Handler: platformReadHandler(defaultDomains, configDomains, artStore, read),
	}
}

func platformReadHandler(
	defaultDomains, configDomains []string,
	artStore artifact.Store,
	read platformReader,
) func(context.Context, *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
	return func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
		in, err := executor.DecodeInput[platformReadInput](call)
		if err != nil {
			return nil, nil, err
		}
		if err := validatePlatformURL(in.URL, call, configDomains, defaultDomains); err != nil {
			return nil, nil, err
		}
		if err := executor.ValidateCookieDomain(in.UseCookieDomain, call, configDomains); err != nil {
			return nil, nil, err
		}

		content, err := read(ctx, in.toPlatformReadInput())
		if err != nil {
			var re *iplatform.ReadError
			if errors.As(err, &re) {
				return iplatform.ErrorOutput(re), nil, re
			}
			return nil, nil, err
		}
		skillEnhancement := in.IncludeOCR || in.IncludeASR || in.IncludeSummary
		if !strings.HasPrefix(call.ID, "collect_") && !skillEnhancement {
			opts := ocr.EnrichOptions{}
			if content.ScreenshotBase64 != "" {
				opts.ExtraImages = []ocr.ImageRef{ocr.PNGDataURLImageRef("screenshot", content.ScreenshotBase64)}
			}
			ocr.EnrichContentWithOptions(ctx, content, config.Get().OCR, opts)
		}
		out := iplatform.ApplyFormats(content, in.Format)
		if !strings.HasPrefix(call.ID, "collect_") && skillEnhancement {
			if err := skill.Enhance(ctx, out, config.Get().Skill, skill.EnhanceOptions{
				CallID: call.ID, IncludeOCR: in.IncludeOCR, IncludeASR: in.IncludeASR,
				IncludeSummary: in.IncludeSummary, SourceText: content.Text, ScreenshotBase64: content.ScreenshotBase64, ArtifactStore: artStore,
			}); err != nil {
				return out, nil, err
			}
		}
		var artifacts []protocol.Artifact
		if content.ScreenshotBase64 != "" {
			artID := call.ID + "_screenshot"
			if artStore == nil {
				return out, nil, fmt.Errorf("screenshot artifact store is unavailable")
			}
			art, err := artifact.PutBase64PNG(ctx, artStore, artID, content.ScreenshotBase64, config.Get().Gateway.ArtifactMaxInlineBytes)
			if err != nil {
				return out, nil, fmt.Errorf("persist screenshot artifact: %w", err)
			}
			artifacts = append(artifacts, art)
			out["screenshot_artifact_id"] = art.ID
		}
		return out, artifacts, nil
	}
}

func validatePlatformURL(rawURL string, call *protocol.ToolCall, configDomains, defaultDomains []string) error {
	domains := policy.MergeDomains(&call.Policy, configDomains)
	if len(domains) == 0 {
		domains = defaultDomains
	}
	return policy.ValidateURLDomain(rawURL, domains, nil)
}
