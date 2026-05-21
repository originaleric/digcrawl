package platform

import "time"

// ReadInput 平台单 URL 读取统一输入（与 gateway executor 对齐）。
type ReadInput struct {
	URL               string   `json:"url"`
	Format            []string `json:"format,omitempty"`
	IncludeMedia      bool     `json:"include_media,omitempty"`
	IncludeScreenshot bool     `json:"include_screenshot,omitempty"`
	WaitSelector      string   `json:"wait_selector,omitempty"`
	UseCookieDomain   string   `json:"use_cookie_domain,omitempty"`
}

// Author 作者信息。
type Author struct {
	ID         string `json:"id,omitempty"`
	Name       string `json:"name,omitempty"`
	ProfileURL string `json:"profile_url,omitempty"`
	AvatarURL  string `json:"avatar_url,omitempty"`
}

// MediaItem 媒体引用。
type MediaItem struct {
	Type       string `json:"type"`
	URL        string `json:"url,omitempty"`
	ArtifactID string `json:"artifact_id,omitempty"`
}

// Engagement 互动数据。
type Engagement struct {
	Likes    int `json:"likes,omitempty"`
	Comments int `json:"comments,omitempty"`
	Shares   int `json:"shares,omitempty"`
	Reposts  int `json:"reposts,omitempty"`
	Bookmarks int `json:"bookmarks,omitempty"`
}

// Content 平台采集统一输出模型。
type Content struct {
	Platform         string         `json:"platform"`
	ContentType      string         `json:"content_type"`
	SourceURL        string         `json:"source_url"`
	CanonicalURL     string         `json:"canonical_url,omitempty"`
	Title            string         `json:"title,omitempty"`
	Text             string         `json:"text,omitempty"`
	Markdown         string         `json:"markdown,omitempty"`
	HTML             string         `json:"html,omitempty"`
	Author           *Author        `json:"author,omitempty"`
	PublishedAt      string         `json:"published_at,omitempty"`
	CapturedAt       string         `json:"captured_at"`
	Media            []MediaItem    `json:"media,omitempty"`
	Engagement       *Engagement    `json:"engagement,omitempty"`
	Tags             []string       `json:"tags,omitempty"`
	PlatformMetadata map[string]any `json:"platform_metadata,omitempty"`
	ScreenshotBase64 string         `json:"-"`
}

// BrowseSpec 平台浏览器采集参数。
type BrowseSpec struct {
	WaitSelector    string
	ContentSelector string
	CookieDomain    string
	MetadataScript  string
	Action          string // read | screenshot
}

// NowRFC3339 返回当前采集时间。
func NowRFC3339() string {
	return time.Now().Format(time.RFC3339)
}
