package platformprov

import (
	"context"
	"errors"

	iplatform "github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/executor"
	"github.com/originaleric/digcrawl/runtime/policy"
	"github.com/originaleric/digcrawl/runtime/registry"
)

type platformReadInput struct {
	URL               string   `json:"url"`
	Format            []string `json:"format,omitempty"`
	IncludeMedia      bool     `json:"include_media,omitempty"`
	IncludeScreenshot bool     `json:"include_screenshot,omitempty"`
	WaitSelector      string   `json:"wait_selector,omitempty"`
	UseCookieDomain   string   `json:"use_cookie_domain,omitempty"`
}

func (in platformReadInput) toPlatformReadInput() iplatform.ReadInput {
	return iplatform.ReadInput{
		URL:               in.URL,
		Format:            in.Format,
		IncludeMedia:      in.IncludeMedia,
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
				return iplatform.ErrorOutput(re), nil, nil
			}
			return nil, nil, err
		}
		out := iplatform.ApplyFormats(content, in.Format)
		var artifacts []protocol.Artifact
		if content.ScreenshotBase64 != "" {
			artID := call.ID + "_screenshot"
			if artStore != nil {
				art, err := artifact.PutBase64PNG(ctx, artStore, artID, content.ScreenshotBase64)
				if err == nil {
					artifacts = append(artifacts, art)
					out["screenshot_artifact_id"] = art.ID
				}
			} else {
				out["screenshot_artifact_id"] = artID
			}
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
