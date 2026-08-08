package platformprov

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/internal/platform/reddit"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/executor"
	"github.com/originaleric/digcrawl/runtime/registry"
	"github.com/originaleric/digcrawl/skill"
)

var redditDefaultDomains = []string{"reddit.com", "www.reddit.com", "old.reddit.com", "new.reddit.com", "redd.it"}

type redditPostReadInput struct {
	URL               string   `json:"url"`
	Format            []string `json:"format,omitempty"`
	IncludeMedia      bool     `json:"include_media,omitempty"`
	IncludeScreenshot bool     `json:"include_screenshot,omitempty"`
	IncludeOCR        bool     `json:"include_ocr,omitempty"`
	IncludeASR        bool     `json:"include_asr,omitempty"`
	IncludeSummary    bool     `json:"include_summary,omitempty"`
	WaitSelector      string   `json:"wait_selector,omitempty"`
	UseCookieDomain   string   `json:"use_cookie_domain,omitempty"`
	IncludeComments   *bool    `json:"include_comments,omitempty"`
	CommentLimit      int      `json:"comment_limit,omitempty"`
	CommentDepth      int      `json:"comment_depth,omitempty"`
	CommentSort       string   `json:"comment_sort,omitempty"`
	ExpandMore        *bool    `json:"expand_more,omitempty"`
	ExpandMoreLimit   int      `json:"expand_more_limit,omitempty"`
}

type redditListingInput struct {
	Subreddit string `json:"subreddit,omitempty"`
	Username  string `json:"username,omitempty"`
	URL       string `json:"url,omitempty"`
	Sort      string `json:"sort,omitempty"`
	TimeRange string `json:"time_range,omitempty"`
	Where     string `json:"where,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	After     string `json:"after,omitempty"`
}

func RedditPostReadEntry(configDomains []string, artStore artifact.Store) registry.Entry {
	return registry.Entry{
		Descriptor: protocol.ToolDescriptor{
			Name:         ToolRedditPostRead,
			Description:  "Read a Reddit post through the Reddit API with browser fallback for public pages.",
			InputSchema:  registry.MustSchema(redditPostInputSchema()),
			OutputSchema: registry.MustSchema(platformOutputSchema()),
			Capabilities: []string{"reddit", "platform.read", "browser"},
			Risk:         "network",
		},
		Handler: redditPostReadHandler(configDomains, artStore, reddit.ReadPost),
	}
}

func RedditSubredditReadEntry(configDomains []string) registry.Entry {
	return registry.Entry{
		Descriptor: protocol.ToolDescriptor{
			Name:         ToolRedditSubredditRead,
			Description:  "List Reddit posts from a subreddit.",
			InputSchema:  registry.MustSchema(redditListingInputSchema("subreddit")),
			OutputSchema: registry.MustSchema(redditListingOutputSchema()),
			Capabilities: []string{"reddit", "platform.list"},
			Risk:         "network",
		},
		Handler: redditSubredditReadHandler(configDomains),
	}
}

func RedditUserReadEntry(configDomains []string) registry.Entry {
	return registry.Entry{
		Descriptor: protocol.ToolDescriptor{
			Name:         ToolRedditUserRead,
			Description:  "List Reddit posts or comments from a user.",
			InputSchema:  registry.MustSchema(redditListingInputSchema("username")),
			OutputSchema: registry.MustSchema(redditListingOutputSchema()),
			Capabilities: []string{"reddit", "platform.list"},
			Risk:         "network",
		},
		Handler: redditUserReadHandler(configDomains),
	}
}

func redditPostReadHandler(configDomains []string, artStore artifact.Store, read func(context.Context, reddit.ReadInput) (*platform.Content, error)) func(context.Context, *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
	return func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
		in, err := executor.DecodeInput[redditPostReadInput](call)
		if err != nil {
			return nil, nil, err
		}
		if err := validatePlatformURL(in.URL, call, configDomains, redditDefaultDomains); err != nil {
			return nil, nil, err
		}
		if err := executor.ValidateCookieDomain(in.UseCookieDomain, call, configDomains); err != nil {
			return nil, nil, err
		}
		includeComments := true
		if in.IncludeComments != nil {
			includeComments = *in.IncludeComments
		}
		expandMore := false
		if in.ExpandMore != nil {
			expandMore = *in.ExpandMore
		}
		content, err := read(ctx, reddit.ReadInput{
			ReadInput: platform.ReadInput{
				URL:               in.URL,
				Format:            in.Format,
				IncludeMedia:      in.IncludeMedia || in.IncludeOCR || in.IncludeASR || in.IncludeSummary,
				IncludeScreenshot: in.IncludeScreenshot,
				WaitSelector:      in.WaitSelector,
				UseCookieDomain:   in.UseCookieDomain,
			},
			IncludeComments: includeComments,
			CommentLimit:    in.CommentLimit,
			CommentDepth:    in.CommentDepth,
			CommentSort:     in.CommentSort,
			ExpandMore:      expandMore,
			ExpandMoreLimit: in.ExpandMoreLimit,
		})
		if err != nil {
			var re *platform.ReadError
			if errors.As(err, &re) {
				return platform.ErrorOutput(re), nil, re
			}
			return nil, nil, err
		}
		out := platform.ApplyFormats(content, in.Format)
		if !strings.HasPrefix(call.ID, "collect_") && (in.IncludeOCR || in.IncludeASR || in.IncludeSummary) {
			if err := skill.Enhance(ctx, out, config.Skill(), skill.EnhanceOptions{
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

func redditSubredditReadHandler(configDomains []string) func(context.Context, *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
	return func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
		in, err := executor.DecodeInput[redditListingInput](call)
		if err != nil {
			return nil, nil, err
		}
		if err := validateOptionalRedditURL(in.URL, call, configDomains); err != nil {
			return nil, nil, err
		}
		sub := firstNonEmpty(in.Subreddit, subredditFromURL(in.URL))
		if sub == "" {
			return nil, nil, errors.New("subreddit or subreddit url is required")
		}
		res, err := reddit.ReadSubreddit(ctx, reddit.ListingOptions{Subreddit: sub, Sort: in.Sort, TimeRange: in.TimeRange, Limit: in.Limit, After: in.After})
		if err != nil {
			return nil, nil, err
		}
		return listingResultMap(res), nil, nil
	}
}

func redditUserReadHandler(configDomains []string) func(context.Context, *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
	return func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
		in, err := executor.DecodeInput[redditListingInput](call)
		if err != nil {
			return nil, nil, err
		}
		if err := validateOptionalRedditURL(in.URL, call, configDomains); err != nil {
			return nil, nil, err
		}
		user := firstNonEmpty(in.Username, usernameFromURL(in.URL))
		if user == "" {
			return nil, nil, errors.New("username or user url is required")
		}
		res, err := reddit.ReadUser(ctx, reddit.ListingOptions{Username: user, Where: in.Where, Limit: in.Limit, After: in.After})
		if err != nil {
			return nil, nil, err
		}
		return listingResultMap(res), nil, nil
	}
}

func redditPostInputSchema() map[string]any {
	schema := platformInputSchema()
	props := schema["properties"].(map[string]any)
	props["include_comments"] = map[string]string{"type": "boolean"}
	props["comment_limit"] = map[string]any{"type": "integer", "minimum": 0, "maximum": 500}
	props["comment_depth"] = map[string]any{"type": "integer", "minimum": 0, "maximum": 10}
	props["comment_sort"] = map[string]any{"type": "string", "enum": []string{"confidence", "top", "new", "controversial", "old", "qa"}}
	props["expand_more"] = map[string]string{"type": "boolean"}
	props["expand_more_limit"] = map[string]any{"type": "integer", "minimum": 0}
	return schema
}

func redditListingInputSchema(_ string) map[string]any {
	props := map[string]any{
		"url":        map[string]string{"type": "string"},
		"subreddit":  map[string]string{"type": "string"},
		"username":   map[string]string{"type": "string"},
		"sort":       map[string]string{"type": "string"},
		"time_range": map[string]any{"type": "string", "enum": []string{"hour", "day", "week", "month", "year", "all"}},
		"where":      map[string]string{"type": "string"},
		"limit":      map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
		"after":      map[string]string{"type": "string"},
	}
	return map[string]any{"type": "object", "required": []string{}, "properties": props}
}

func redditListingOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"platform": map[string]string{"type": "string"},
			"items":    map[string]any{"type": "array"},
			"after":    map[string]string{"type": "string"},
		},
	}
}

func listingResultMap(res *reddit.ListingResult) map[string]any {
	items := []map[string]any{}
	if res != nil {
		raw, _ := json.Marshal(res.Items)
		_ = json.Unmarshal(raw, &items)
	}
	out := map[string]any{"platform": "reddit", "items": items}
	if res != nil && strings.TrimSpace(res.After) != "" {
		out["after"] = strings.TrimSpace(res.After)
	}
	return out
}

func validateOptionalRedditURL(rawURL string, call *protocol.ToolCall, domains []string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil
	}
	return validatePlatformURL(rawURL, call, domains, redditDefaultDomains)
}

func subredditFromURL(rawURL string) string {
	parts := redditURLParts(rawURL)
	if len(parts) >= 2 && strings.EqualFold(parts[0], "r") {
		return parts[1]
	}
	return ""
}

func usernameFromURL(rawURL string) string {
	parts := redditURLParts(rawURL)
	if len(parts) >= 2 && (strings.EqualFold(parts[0], "user") || strings.EqualFold(parts[0], "u")) {
		return parts[1]
	}
	return ""
}

func redditURLParts(rawURL string) []string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	path := strings.Trim(u.Path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
