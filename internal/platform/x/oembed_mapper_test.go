package x

import (
	"testing"

	"github.com/originaleric/digcrawl/internal/platform"
)

func TestMapOEmbedResponse(t *testing.T) {
	resp := &oEmbedResponse{
		URL:          "https://x.com/jack/status/20",
		AuthorName:   "jack",
		AuthorURL:    "https://x.com/jack",
		HTML:         `<blockquote class="twitter-tweet"><p lang="en" dir="ltr">just setting up my twttr</p>&mdash; jack (@jack) <a href="https://x.com/jack/status/20?ref_src=twsrc%5Etfw">March 21, 2006</a></blockquote>`,
		ProviderName: "X",
	}
	content, err := MapOEmbedResponse(platform.ReadInput{URL: "https://x.com/jack/status/20"}, &platform.NormalizedURL{
		OriginalURL:  "https://x.com/jack/status/20",
		CanonicalURL: "https://x.com/jack/status/20",
		PlatformID:   "20",
	}, resp)
	if err != nil {
		t.Fatal(err)
	}
	if content.Text != "just setting up my twttr" {
		t.Fatalf("text=%q", content.Text)
	}
	if content.PublishedAt != "March 21, 2006" {
		t.Fatalf("published=%q", content.PublishedAt)
	}
	if content.Author == nil || content.Author.Name != "jack" || content.Author.ProfileURL != "https://x.com/jack" {
		t.Fatalf("author=%+v", content.Author)
	}
	if content.PlatformMetadata["extraction_source"] != "oembed" {
		t.Fatalf("metadata=%+v", content.PlatformMetadata)
	}
}
