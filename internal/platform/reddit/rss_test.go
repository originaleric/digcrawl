package reddit

import (
	"testing"

	"github.com/originaleric/digcrawl/internal/feed"
	"github.com/originaleric/digcrawl/internal/platform"
)

func TestMapPostRSSBuildsContentAndComments(t *testing.T) {
	doc, err := feed.Parse([]byte(`<?xml version="1.0"?>
<feed>
  <title>Post title : artificial</title>
  <entry>
    <id>post</id>
    <title>Post title</title>
    <author><name>/u/alice</name><uri>https://www.reddit.com/user/alice</uri></author>
    <link href="https://www.reddit.com/r/artificial/comments/abc/post_title/"/>
    <updated>2026-07-15T08:00:00Z</updated>
    <content type="html">&lt;p&gt;Post body&lt;/p&gt;</content>
  </entry>
  <entry>
    <id>comment1</id>
    <title>Comment</title>
    <author><name>/u/bob</name></author>
    <link href="https://www.reddit.com/r/artificial/comments/abc/post_title/comment1/"/>
    <updated>2026-07-15T08:01:00Z</updated>
    <content type="html">&lt;p&gt;First comment&lt;/p&gt;</content>
  </entry>
</feed>`), "https://www.reddit.com/r/artificial/comments/abc/post_title/.rss")
	if err != nil {
		t.Fatal(err)
	}
	content, err := mapPostRSS(ReadInput{ReadInput: platform.ReadInput{URL: "https://www.reddit.com/r/artificial/comments/abc/post_title/"}, IncludeComments: true}, &platform.NormalizedURL{PlatformID: "abc", CanonicalURL: "https://www.reddit.com/r/artificial/comments/abc/"}, doc, PostReadOptions{IncludeComments: true, CommentLimit: 10}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if content.Title != "Post title" || content.Text != "Post body" {
		t.Fatalf("unexpected content: %+v", content)
	}
	comments, ok := content.PlatformMetadata["comments_tree"].([]RedditComment)
	if !ok || len(comments) != 1 || comments[0].Body != "First comment" {
		t.Fatalf("unexpected comments: %+v", content.PlatformMetadata["comments_tree"])
	}
	if content.PlatformMetadata["extraction_source"] != "rss" {
		t.Fatalf("expected rss metadata: %+v", content.PlatformMetadata)
	}
}

func TestListingFromFeedDocumentMapsEntries(t *testing.T) {
	doc, err := feed.Parse([]byte(`<?xml version="1.0"?><rss><channel><item><title>A</title><link>https://www.reddit.com/r/artificial/comments/abc/a/</link><guid>g1</guid></item></channel></rss>`), "https://www.reddit.com/r/artificial/.rss")
	if err != nil {
		t.Fatal(err)
	}
	res := listingFromFeedDocument(doc, "r/artificial", 10)
	if len(res.Items) != 1 {
		t.Fatalf("unexpected items: %+v", res.Items)
	}
	if res.Items[0].Title != "A" || res.Items[0].CanonicalURL == "" {
		t.Fatalf("unexpected item: %+v", res.Items[0])
	}
}

func TestSubredditRSSURLPreservesListingOptions(t *testing.T) {
	got := subredditRSSURL(ListingOptions{
		Subreddit: "LocalLLaMA",
		Sort:      "top",
		TimeRange: "week",
		Limit:     12,
		After:     "t3_after",
	})
	want := "https://www.reddit.com/r/LocalLLaMA/top/.rss?after=t3_after&limit=12&t=week"
	if got != want {
		t.Fatalf("rss url = %q, want %q", got, want)
	}
}

func TestUserRSSURLPreservesWhereAndLimit(t *testing.T) {
	got := userRSSURL(ListingOptions{Username: "example", Where: "comments", Limit: 7})
	want := "https://www.reddit.com/user/example/comments/.rss?limit=7"
	if got != want {
		t.Fatalf("rss url = %q, want %q", got, want)
	}
}
