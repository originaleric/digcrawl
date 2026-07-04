package x

import (
	"testing"

	"github.com/originaleric/digcrawl/internal/platform"
)

func TestMapAPIResponse(t *testing.T) {
	resp := &tweetLookupResponse{
		Data: tweetData{
			ID:          "42",
			Text:        "hello from x",
			AuthorID:    "u1",
			CreatedAt:   "2026-07-04T00:00:00.000Z",
			Attachments: tweetAttachments{MediaKeys: []string{"m1"}},
			PublicMetrics: tweetPublicMetrics{
				LikeCount:    10,
				ReplyCount:   2,
				RetweetCount: 3,
				QuoteCount:   4,
			},
		},
		Includes: tweetIncludes{
			Users: []tweetUser{{
				ID:              "u1",
				Name:            "Tester",
				Username:        "tester",
				ProfileImageURL: "https://img.example/avatar.jpg",
				Verified:        true,
			}},
			Media: []tweetMedia{{
				MediaKey: "m1",
				Type:     "photo",
				URL:      "https://img.example/post.jpg",
			}},
		},
	}
	content, err := MapAPIResponse(platform.ReadInput{URL: "https://x.com/tester/status/42", IncludeMedia: true}, &platform.NormalizedURL{
		OriginalURL:  "https://x.com/tester/status/42",
		CanonicalURL: "https://x.com/tester/status/42",
		PlatformID:   "42",
	}, resp)
	if err != nil {
		t.Fatal(err)
	}
	if content.CanonicalURL != "https://x.com/tester/status/42" {
		t.Fatalf("canonical=%q", content.CanonicalURL)
	}
	if content.Author == nil || content.Author.Name != "Tester" || content.Author.ProfileURL != "https://x.com/tester" {
		t.Fatalf("author=%+v", content.Author)
	}
	if content.Engagement == nil || content.Engagement.Likes != 10 || content.Engagement.Reposts != 3 {
		t.Fatalf("engagement=%+v", content.Engagement)
	}
	if len(content.Media) != 1 || content.Media[0].URL != "https://img.example/post.jpg" {
		t.Fatalf("media=%+v", content.Media)
	}
	if content.PlatformMetadata["author_verified"] != true {
		t.Fatalf("metadata=%+v", content.PlatformMetadata)
	}
}
