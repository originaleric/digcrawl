package zhihu

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/originaleric/digcrawl/internal/platform"
)

const (
	PlatformName = "zhihu"
	CookieDomain = "www.zhihu.com"
)

var (
	answerPattern   = regexp.MustCompile(`(?i)/question/(\d+)/answer/(\d+)`)
	questionPattern = regexp.MustCompile(`(?i)/question/(\d+)`)
	articlePattern  = regexp.MustCompile(`(?i)/p/(\d+)`)
	zvideoPattern   = regexp.MustCompile(`(?i)/zvideo/(\d+)`)
	peoplePattern   = regexp.MustCompile(`(?i)/(?:people|org)/([^/]+)`)
	zhihuHosts      = []string{"zhihu.com", "www.zhihu.com", "zhuanlan.zhihu.com"}
)

func NormalizeURL(_ context.Context, rawURL string) (*platform.NormalizedURL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("url 不能为空")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("无效 url: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	if !hostMatches(host, zhihuHosts) {
		return nil, fmt.Errorf("非知乎域名: %s", host)
	}
	out := &platform.NormalizedURL{OriginalURL: rawURL, ResolvedURL: rawURL, CookieDomain: CookieDomain}
	if qid, aid := extractAnswerID(u.Path); aid != "" {
		out.PlatformID = aid
		out.ContentKind = "answer"
		out.CanonicalURL = fmt.Sprintf("https://www.zhihu.com/question/%s/answer/%s", qid, aid)
		out.BrowseURL = out.CanonicalURL
		return out, nil
	}
	if id := extractArticleID(u.Path); id != "" {
		out.PlatformID = id
		out.ContentKind = "article"
		out.CanonicalURL = "https://zhuanlan.zhihu.com/p/" + id
		out.BrowseURL = out.CanonicalURL
		return out, nil
	}
	if id := extractZvideoID(u.Path); id != "" {
		out.PlatformID = id
		out.ContentKind = "video"
		out.CanonicalURL = "https://www.zhihu.com/zvideo/" + id
		out.BrowseURL = out.CanonicalURL
		return out, nil
	}
	if id := extractQuestionID(u.Path); id != "" {
		out.PlatformID = id
		out.ContentKind = "question"
		out.CanonicalURL = "https://www.zhihu.com/question/" + id
		out.BrowseURL = out.CanonicalURL
		return out, nil
	}
	if token := extractPeopleToken(u.Path); token != "" {
		out.PlatformID = token
		out.ContentKind = "user"
		out.CanonicalURL = "https://www.zhihu.com/people/" + token
		out.BrowseURL = out.CanonicalURL
		return out, nil
	}
	out.ContentKind = "user"
	out.CanonicalURL = rawURL
	out.BrowseURL = rawURL
	out.RiskFlags = append(out.RiskFlags, "platform_id_unknown")
	return out, nil
}

func extractAnswerID(path string) (qid, aid string) {
	m := answerPattern.FindStringSubmatch(path)
	if len(m) < 3 {
		return "", ""
	}
	return m[1], m[2]
}

func extractQuestionID(path string) string {
	m := questionPattern.FindStringSubmatch(path)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func extractArticleID(path string) string {
	m := articlePattern.FindStringSubmatch(path)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func extractZvideoID(path string) string {
	m := zvideoPattern.FindStringSubmatch(path)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func extractPeopleToken(path string) string {
	m := peoplePattern.FindStringSubmatch(path)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func hostMatches(host string, allowed []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		if host == a || strings.HasSuffix(host, "."+a) {
			return true
		}
	}
	return false
}
