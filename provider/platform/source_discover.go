package platformprov

import (
	"context"
	"encoding/base64"
	"encoding/json"
	iplatform "github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/internal/source"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/policy"
	"github.com/originaleric/digcrawl/runtime/registry"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Discovery is a read-only Skill capability. Watchlists and business scheduling
// remain host-owned; this adapter reuses DigCrawl's existing source readers.
const ToolSourceDiscover = "source.discover"

var discoverKinds = map[string]string{"rss": "rss", "sitemap": "sitemap", "web_list": "web", "x_user": "x", "reddit_subreddit": "reddit", "reddit_user": "reddit", "wechat_list": "wechat", "toutiao_author": "toutiao", "xiaohongshu_user": "xiaohongshu", "douyin_user": "douyin", "bilibili_user": "bilibili", "youtube_channel": "youtube", "weibo_user": "weibo", "kuaishou_user": "kuaishou", "zhihu_user": "zhihu", "threads_user": "threads", "instagram_user": "instagram"}

type discoverInput struct {
	URL        string `json:"url"`
	Kind       string `json:"source_kind"`
	Platform   string `json:"platform"`
	Limit      int    `json:"limit"`
	Checkpoint string `json:"checkpoint"`
}
type discoveredItem struct {
	Key       string    `json:"content_key"`
	URL       string    `json:"canonical_url"`
	Platform  string    `json:"platform"`
	Title     string    `json:"title"`
	Author    string    `json:"author"`
	Published string    `json:"published_at"`
	Observed  time.Time `json:"observed_at"`
}
type discoverySnapshot struct {
	URL      string           `json:"url"`
	Kind     string           `json:"kind"`
	Platform string           `json:"platform"`
	Items    []discoveredItem `json:"items"`
	Coverage string           `json:"coverage"`
	Reason   string           `json:"reason"`
	Observed time.Time        `json:"observed_at"`
}
type discoveryCursor struct {
	ID     string `json:"id"`
	Offset int    `json:"offset"`
}

func SourceDiscoverEntry(domains []string, store artifact.Store) registry.Entry {
	kinds := []string{"article"}
	for k := range discoverKinds {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return registry.Entry{Descriptor: protocol.ToolDescriptor{Name: ToolSourceDiscover, Description: "Discover actual entries from a source URL. Read-only; no watchlist, collection, summary or notification. Check coverage; account/list readers expose a visible snapshot, not a complete historical timeline. Opaque checkpoints page one immutable snapshot and expire with the artifact store. source_kind=article returns the caller-supplied detail URL for subsequent read.", Capabilities: []string{"source.discover", "read-only"}, Risk: "network", InputSchema: registry.MustSchema(map[string]any{"type": "object", "additionalProperties": false, "required": []string{"url", "source_kind"}, "properties": map[string]any{"url": map[string]any{"type": "string"}, "source_kind": map[string]any{"type": "string", "enum": kinds}, "platform": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}, "checkpoint": map[string]any{"type": "string", "maxLength": 2048}}}), OutputSchema: registry.MustSchema(map[string]any{"type": "object", "required": []string{"items", "coverage", "has_more", "next_checkpoint", "observed_at"}, "properties": map[string]any{"items": map[string]any{"type": "array"}, "coverage": map[string]any{"type": "string"}, "reason": map[string]any{"type": "string"}, "has_more": map[string]any{"type": "boolean"}, "next_checkpoint": map[string]any{"type": "string"}, "observed_at": map[string]any{"type": "string"}}})}, Handler: func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
		var in discoverInput
		dec := json.NewDecoder(strings.NewReader(string(call.Input)))
		dec.DisallowUnknownFields()
		if e := dec.Decode(&in); e != nil {
			return nil, nil, iplatform.NewReadError("INVALID_INPUT", "discovery input", "source", nil)
		}
		if _, e := dec.Token(); e != io.EOF {
			return nil, nil, iplatform.NewReadError("INVALID_INPUT", "trailing discovery input", "source", nil)
		}
		if in.Limit == 0 {
			in.Limit = 100
		}
		if in.Limit < 1 || in.Limit > 100 || len(in.Checkpoint) > 2048 {
			return nil, nil, iplatform.NewReadError("INVALID_INPUT", "discovery budget", "source", nil)
		}
		if e := policy.ValidateURLDomain(in.URL, call.Policy.AllowedDomains, domains); e != nil {
			return nil, nil, e
		}
		platform, ok := discoverKinds[in.Kind]
		if in.Kind == "article" {
			platform = in.Platform
			ok = platform != ""
		}
		if !ok {
			return nil, nil, iplatform.NewReadError("CAPABILITY_UNAVAILABLE", "unknown source_kind", "source", nil)
		}
		if in.Platform != "" && in.Platform != platform {
			return nil, nil, iplatform.NewReadError("INVALID_INPUT", "platform/kind mismatch", "source", nil)
		}
		var snap discoverySnapshot
		offset := 0
		sid := ""
		if in.Checkpoint != "" {
			if store == nil {
				return nil, nil, iplatform.NewReadError("CHECKPOINT_UNAVAILABLE", "artifact store required", "source", nil)
			}
			raw, e := base64.RawURLEncoding.DecodeString(in.Checkpoint)
			var cursor discoveryCursor
			if e != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Offset < 0 || cursor.Offset > 10000 {
				return nil, nil, iplatform.NewReadError("INVALID_INPUT", "checkpoint", "source", nil)
			}
			data, ct, e := store.Get(ctx, cursor.ID)
			if e != nil || ct != "application/vnd.digcrawl.discovery+json" || json.Unmarshal(data, &snap) != nil {
				return nil, nil, iplatform.NewReadError("CHECKPOINT_EXPIRED", "start a fresh scan without advancing baseline", "source", nil)
			}
			if snap.URL != in.URL || snap.Kind != in.Kind || snap.Platform != platform {
				return nil, nil, iplatform.NewReadError("INVALID_INPUT", "checkpoint source mismatch", "source", nil)
			}
			offset, sid = cursor.Offset, cursor.ID
		} else {
			snap = discoverySnapshot{URL: in.URL, Kind: in.Kind, Platform: platform, Items: []discoveredItem{}, Coverage: "unknown", Reason: "visible_snapshot_only; historical completeness is not guaranteed", Observed: time.Now().UTC()}
			if in.Kind == "article" {
				snap.Items = append(snap.Items, discoveredItem{Key: in.URL, URL: in.URL, Platform: platform, Observed: snap.Observed})
				snap.Coverage = "complete"
				snap.Reason = "single caller-supplied detail URL; content not read yet"
			} else {
				// The source readers own platform discovery. Disable automatic browser
				// fallback here: access-control failures must never start another route.
				client := &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
					if len(via) >= 10 {
						return iplatform.NewReadError("INVALID_INPUT", "redirect budget", "source", nil)
					}
					return policy.ValidateURLDomain(req.URL.String(), call.Policy.AllowedDomains, domains)
				}}
				items, e := (source.Service{Client: client}).ListLatest(ctx, source.Source{URL: in.URL, Platform: platform, Type: in.Kind, Options: map[string]any{"limit": 1000, "browser_fallback": false, "auth_mode": "public"}})
				if e != nil {
					return nil, nil, e
				}
				if len(items) > 10000 {
					return nil, nil, iplatform.NewReadError("SOURCE_LIMIT_EXCEEDED", "discovery snapshot too large", "source", nil)
				}
				seen := map[string]bool{}
				for _, it := range items {
					u := it.CanonicalURL
					if u == "" {
						u = it.URL
					}
					if e = policy.ValidateURLDomain(u, call.Policy.AllowedDomains, domains); e != nil {
						return nil, nil, e
					}
					key := it.ContentKey
					if key == "" {
						key = it.ID
					}
					if key == "" {
						key = u
					}
					if seen[key] {
						continue
					}
					seen[key] = true
					published := ""
					if !it.PublishedAt.IsZero() {
						published = it.PublishedAt.UTC().Format(time.RFC3339)
					}
					snap.Items = append(snap.Items, discoveredItem{key, u, platform, it.Title, it.Author, published, snap.Observed})
				}
				if in.Kind == "rss" || in.Kind == "sitemap" {
					snap.Coverage = "complete"
					snap.Reason = "all entries in the current feed/sitemap response; older disappeared entries are not recoverable"
				}
				if len(items) >= 1000 {
					snap.Coverage = "partial"
					snap.Reason = "reader item limit reached; upstream continuation not available"
				}
			}
		}
		if offset > len(snap.Items) {
			return nil, nil, iplatform.NewReadError("INVALID_INPUT", "checkpoint offset", "source", nil)
		}
		end := offset + in.Limit
		if end > len(snap.Items) {
			end = len(snap.Items)
		}
		more := end < len(snap.Items)
		next := ""
		if more {
			if store == nil {
				return nil, nil, iplatform.NewReadError("CHECKPOINT_UNAVAILABLE", "paging requires artifact store", "source", nil)
			}
			if sid == "" {
				data, e := json.Marshal(snap)
				if e != nil || len(data) > 4<<20 {
					return nil, nil, iplatform.NewReadError("SOURCE_LIMIT_EXCEEDED", "snapshot byte budget", "source", nil)
				}
				sid = artifact.NewID()
				if _, e = store.Put(ctx, sid, "application/vnd.digcrawl.discovery+json", "discovery-snapshot.json", data); e != nil {
					return nil, nil, e
				}
			}
			b, _ := json.Marshal(discoveryCursor{sid, end})
			next = base64.RawURLEncoding.EncodeToString(b)
		}
		// Validate returned cached entries under the current call policy as well.
		for _, it := range snap.Items[offset:end] {
			if e := policy.ValidateURLDomain(it.URL, call.Policy.AllowedDomains, domains); e != nil {
				return nil, nil, e
			}
		}
		return map[string]any{"items": snap.Items[offset:end], "coverage": snap.Coverage, "reason": snap.Reason, "has_more": more, "next_checkpoint": next, "observed_at": snap.Observed}, nil, nil
	}}
}
