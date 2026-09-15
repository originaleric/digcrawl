package browser

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/protocol"
)

func TestValidateCallURLOpensPublicWebWhenDomainRestrictionIsUnset(t *testing.T) {
	if err := validateCallURL("https://8.8.8.8/article", &protocol.ToolCall{}, nil); err != nil {
		t.Fatalf("expected public URL to be allowed without a domain restriction: %v", err)
	}
}

func TestBrowseEntryExposesScreenshotAlongsideRead(t *testing.T) {
	entry := BrowseEntry(nil, nil)
	var schema struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(entry.Descriptor.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if _, ok := schema.Properties["include_screenshot"]; !ok {
		t.Fatal("browser.browse must expose include_screenshot so a host can receive text and an image attachment together")
	}
	if _, ok := schema.Properties["include_media"]; !ok {
		t.Fatal("browser.browse must expose include_media for rendered image references")
	}
}

func TestBrowserImageMediaKeepsOnlyUniqueURLs(t *testing.T) {
	media := browserImageMedia(map[string]any{"image_urls": []any{"https://cdn.example/a.jpg", "https://cdn.example/a.jpg", "", 42}})
	if len(media) != 1 || media[0]["type"] != "image" || media[0]["url"] != "https://cdn.example/a.jpg" {
		t.Fatalf("media=%+v", media)
	}
}

func TestBrowserImageMediaRejectsCredentialBearingOrInvalidURLs(t *testing.T) {
	media := browserImageMedia(map[string]any{"image_urls": []any{
		"https://user:password@cdn.example/private.jpg",
		"javascript:alert(1)",
		"https://cdn.example/public.jpg",
	}})
	if len(media) != 1 || media[0]["url"] != "https://cdn.example/public.jpg" {
		t.Fatalf("media=%+v", media)
	}
	if !strings.Contains(browserImageMetadataScript(), "!u.username") || !strings.Contains(browserImageMetadataScript(), "!u.password") {
		t.Fatal("browser metadata script must exclude URL credentials before they enter metadata")
	}
}

func TestValidateCallURLAllowsLocalTargetsAndHonorsOptionalRestriction(t *testing.T) {
	if err := validateCallURL("http://127.0.0.1/admin", &protocol.ToolCall{}, nil); err != nil {
		t.Fatalf("expected an unrestricted local target to be allowed: %v", err)
	}
	if err := validateCallURL("https://1.1.1.1/article", &protocol.ToolCall{}, []string{"8.8.8.8"}); err == nil {
		t.Fatal("expected configured domain restriction to be enforced")
	}
}
