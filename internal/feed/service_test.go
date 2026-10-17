package feed

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseAtomFeedPreservesEntryContent(t *testing.T) {
	doc, err := Parse([]byte(`<?xml version="1.0"?>
<feed>
  <title>AI Feed</title>
  <entry>
    <id>e1</id>
    <title>Post A</title>
    <author><name>alice</name><uri>https://example.com/u/alice</uri></author>
    <link rel="alternate" href="https://example.com/a"/>
    <updated>2026-07-15T08:00:00Z</updated>
    <content type="html">&lt;p&gt;Hello &lt;b&gt;AI&lt;/b&gt;&lt;/p&gt;</content>
  </entry>
</feed>`), "https://example.com/feed.atom")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "AI Feed" || len(doc.Entries) != 1 {
		t.Fatalf("unexpected doc: %+v", doc)
	}
	entry := doc.Entries[0]
	if entry.URL != "https://example.com/a" || entry.Author != "alice" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	if got := TextFromHTML(entry.ContentHTML); got != "Hello AI" {
		t.Fatalf("content text = %q", got)
	}
}

func TestToContentIncludesEntriesMetadata(t *testing.T) {
	doc, err := Parse([]byte(`<?xml version="1.0"?><rss><channel><title>News</title><item><title>A</title><link>https://example.com/a</link><guid>g1</guid><description><![CDATA[<p>Summary</p>]]></description></item></channel></rss>`), "https://example.com/rss")
	if err != nil {
		t.Fatal(err)
	}
	content := ToContent(doc, doc.URL)
	if content.Platform != PlatformName || content.ContentType != ContentType {
		t.Fatalf("unexpected content: %+v", content)
	}
	entries, ok := content.PlatformMetadata["entries"].([]map[string]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("missing entries metadata: %+v", content.PlatformMetadata)
	}
	if entries[0]["summary"] != "Summary" {
		t.Fatalf("unexpected summary: %+v", entries[0])
	}
}

func TestReadFallsBackToSitemapXML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0"?><urlset><url><loc>https://example.com/a</loc><lastmod>2026-07-15</lastmod></url></urlset>`))
	}))
	defer srv.Close()
	doc, err := Service{Client: srv.Client()}.Read(context.Background(), srv.URL+"/sitemap.xml")
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Entries) != 1 || doc.Entries[0].URL != "https://example.com/a" {
		t.Fatalf("unexpected sitemap doc: %+v", doc)
	}
}

func TestFeedSourceIsConfirmedAcrossReadPaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/redirect/") {
			http.Redirect(w, r, "/actual/"+strings.TrimPrefix(r.URL.Path, "/redirect/"), http.StatusFound)
			return
		}
		if strings.HasSuffix(r.URL.Path, "sitemap.xml") {
			fmt.Fprint(w, `<urlset><url><loc>https://item.example/article</loc></url></urlset>`)
			return
		}
		fmt.Fprint(w, `<rss><channel><title>Feed</title><item><title>Item</title><link>https://item.example/article</link></item></channel></rss>`)
	}))
	defer server.Close()
	svc := Service{Client: server.Client()}
	for _, sitemap := range []bool{false, true} {
		name := "feed.xml"
		if sitemap {
			name = "sitemap.xml"
		}
		start := server.URL + "/redirect/" + name
		final := server.URL + "/actual/" + name
		var doc *Document
		var err error
		if sitemap {
			doc, err = svc.ReadSitemap(context.Background(), start)
		} else {
			doc, err = svc.Read(context.Background(), start)
		}
		if err != nil || doc.URL != final {
			t.Fatalf("doc=%+v err=%v", doc, err)
		}
		content := ToContent(doc, start)
		if content.SourceURL != start || content.CanonicalURL != final || content.PlatformMetadata["final_url"] != final {
			t.Fatalf("content=%+v", content)
		}
	}
}
