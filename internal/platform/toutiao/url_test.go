package toutiao

import (
	"context"
	"strings"
	"testing"
)

func TestNormalizeURLArticle(t *testing.T) {
	t.Parallel()
	n, err := NormalizeURL(context.Background(), "https://www.toutiao.com/article/7123456789012345678/")
	if err != nil {
		t.Fatal(err)
	}
	if n.PlatformID != "7123456789012345678" {
		t.Fatalf("id: %s", n.PlatformID)
	}
	if n.CanonicalURL != "https://www.toutiao.com/article/7123456789012345678/" {
		t.Fatalf("canonical: %s", n.CanonicalURL)
	}
}

func TestNormalizeURLGroup(t *testing.T) {
	t.Parallel()
	n, err := NormalizeURL(context.Background(), "https://www.toutiao.com/group/1234567890/")
	if err != nil {
		t.Fatal(err)
	}
	if n.PlatformID != "1234567890" {
		t.Fatalf("id: %s", n.PlatformID)
	}
	if n.ContentKind != "group" {
		t.Fatalf("kind: %s", n.ContentKind)
	}
}

func TestNormalizeURLVideo(t *testing.T) {
	t.Parallel()
	n, err := NormalizeURL(context.Background(), "https://www.toutiao.com/video/7667505035089101366/?log_from=x")
	if err != nil {
		t.Fatal(err)
	}
	if n.PlatformID != "7667505035089101366" {
		t.Fatalf("id: %s", n.PlatformID)
	}
	if n.ContentKind != VideoContentType {
		t.Fatalf("kind: %s", n.ContentKind)
	}
	if n.CanonicalURL != "https://www.toutiao.com/video/7667505035089101366/" {
		t.Fatalf("canonical: %s", n.CanonicalURL)
	}
}

func TestNormalizeURLRejectForeign(t *testing.T) {
	t.Parallel()
	_, err := NormalizeURL(context.Background(), "https://example.com/article/1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestVideoMetadataChecksDeletedAfterMediaExtraction(t *testing.T) {
	t.Parallel()
	script := VideoMetadataScript()
	videoURLs := strings.Index(script, "const video_urls")
	deleted := strings.Index(script, "CONTENT_DELETED")
	if videoURLs < 0 || deleted < 0 {
		t.Fatalf("script missing expected markers")
	}
	if deleted < videoURLs {
		t.Fatalf("deleted state should be checked after extracting video urls")
	}
}
