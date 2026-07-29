package zhihu

import (
	"context"
	"testing"
)

func TestNormalizeZhihuAnswerURL(t *testing.T) {
	n, err := NormalizeURL(context.Background(), "https://www.zhihu.com/question/123/answer/456")
	if err != nil {
		t.Fatal(err)
	}
	if n.PlatformID != "456" || n.ContentKind != "answer" || n.CanonicalURL != "https://www.zhihu.com/question/123/answer/456" {
		t.Fatalf("unexpected normalized url: %+v", n)
	}
}

func TestNormalizeZhihuArticleURL(t *testing.T) {
	n, err := NormalizeURL(context.Background(), "https://zhuanlan.zhihu.com/p/789")
	if err != nil {
		t.Fatal(err)
	}
	if n.PlatformID != "789" || n.ContentKind != "article" || n.CanonicalURL != "https://zhuanlan.zhihu.com/p/789" {
		t.Fatalf("unexpected normalized url: %+v", n)
	}
}
