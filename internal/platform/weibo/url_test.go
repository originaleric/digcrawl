package weibo

import (
	"context"
	"testing"
)

func TestNormalizeWeiboDetailURL(t *testing.T) {
	n, err := NormalizeURL(context.Background(), "https://m.weibo.cn/detail/502233445566")
	if err != nil {
		t.Fatal(err)
	}
	if n.PlatformID != "502233445566" || n.ContentKind != "post" || n.CanonicalURL != "https://m.weibo.cn/detail/502233445566" {
		t.Fatalf("unexpected normalized url: %+v", n)
	}
}

func TestNormalizeWeiboUserURL(t *testing.T) {
	n, err := NormalizeURL(context.Background(), "https://m.weibo.cn/u/123456")
	if err != nil {
		t.Fatal(err)
	}
	if n.PlatformID != "123456" || n.ContentKind != "user" || n.CanonicalURL != "https://m.weibo.cn/u/123456" {
		t.Fatalf("unexpected normalized url: %+v", n)
	}
}
