package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/originaleric/digcrawl/runtime/artifact"
)

var markdownImageRe = regexp.MustCompile(`!\[[^\]]*\]\((https?://[^\s\)]+)\)`)

// AssetDownloadOptions configures media file downloads into a Skill-owned directory.
type AssetDownloadOptions struct {
	MaxBytes      int64
	TimeoutSec    int
	ArtifactStore artifact.Store
}

// DownloadAssets mirrors the collect media acquisition rules without depending on collect.
func DownloadAssets(ctx context.Context, assetsDir string, out map[string]any, opts AssetDownloadOptions) ([]string, error) {
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		return nil, err
	}
	var saved []string
	var manifest []map[string]any

	if opts.ArtifactStore != nil {
		if p, err := copyScreenshotArtifact(opts.ArtifactStore, out, assetsDir); err == nil && p != "" {
			saved = append(saved, p)
			manifest = append(manifest, map[string]any{"type": "screenshot", "local": filepath.Base(p)})
		}
	}

	items := extractMediaItems(out)
	if len(items) == 0 {
		items = fallbackMediaItems(out)
		if len(items) > 0 {
			out["media"] = items
		}
	}
	if len(items) > 0 || len(manifest) > 0 {
		if opts.MaxBytes <= 0 {
			opts.MaxBytes = 10 * 1024 * 1024
		}
		if opts.TimeoutSec <= 0 {
			opts.TimeoutSec = 30
		}
		client := &http.Client{Timeout: time.Duration(opts.TimeoutSec) * time.Second}
		referer := refererForDownload(out)
		for i, item := range items {
			rawURL, _ := item["url"].(string)
			rawURL = strings.TrimSpace(rawURL)
			if rawURL == "" || !strings.HasPrefix(rawURL, "http") {
				continue
			}
			if err := validateRemoteAssetURL(rawURL); err != nil {
				manifest = append(manifest, map[string]any{
					"type": item["type"], "url": rawURL, "error": err.Error(),
				})
				continue
			}
			mediaType, _ := item["type"].(string)
			name := assetFilename(rawURL, mediaType, i)
			dest := filepath.Join(assetsDir, name)
			contentType, err := downloadFile(ctx, client, rawURL, dest, opts.MaxBytes, referer)
			if err != nil {
				manifest = append(manifest, map[string]any{
					"type": item["type"], "url": rawURL, "error": err.Error(),
				})
				continue
			}
			finalName, finalDest, err := normalizeDownloadedAssetName(name, dest, rawURL, mediaType, contentType)
			if err != nil {
				manifest = append(manifest, map[string]any{
					"type": item["type"], "url": rawURL, "error": err.Error(),
				})
				continue
			}
			name = finalName
			dest = finalDest
			saved = append(saved, dest)
			manifest = append(manifest, map[string]any{
				"type": mediaType, "url": rawURL, "local": name,
			})
		}
	}

	manifestFiles, err := writeAssetsManifest(assetsDir, out, manifest)
	return append(saved, manifestFiles...), err
}

func validateRemoteAssetURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported asset scheme %q", u.Scheme)
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return fmt.Errorf("asset URL missing host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateOrLocalIP(ip) {
			return fmt.Errorf("asset host %q is private/local", host)
		}
		return nil
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return fmt.Errorf("asset host %q is local", host)
	}
	return nil
}

func isPrivateOrLocalIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

func copyScreenshotArtifact(store artifact.Store, out map[string]any, assetsDir string) (string, error) {
	id, _ := out["screenshot_artifact_id"].(string)
	if id == "" || store == nil {
		return "", nil
	}
	data, ctype, err := store.Get(context.Background(), id)
	if err != nil {
		return "", err
	}
	ext := ".png"
	if strings.Contains(ctype, "jpeg") {
		ext = ".jpg"
	}
	dest := filepath.Join(assetsDir, "screenshot"+ext)
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return "", err
	}
	out["screenshot_local"] = filepath.Base(dest)
	return dest, nil
}

// RewriteMarkdownAssets replaces remote image URLs in markdown with local asset paths.
func RewriteMarkdownAssets(mdPath, assetsDir string, manifest []map[string]any) error {
	data, err := os.ReadFile(mdPath)
	if err != nil {
		return err
	}
	body := string(data)
	changed := false
	for _, item := range manifest {
		rawURL, _ := item["url"].(string)
		local, _ := item["local"].(string)
		if rawURL == "" || local == "" {
			continue
		}
		rel := filepath.Base(assetsDir) + "/" + local
		rel = strings.ReplaceAll(rel, "\\", "/")
		if strings.Contains(body, rawURL) {
			body = strings.ReplaceAll(body, rawURL, rel)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return os.WriteFile(mdPath, []byte(body), 0o644)
}

func extractMediaItems(out map[string]any) []map[string]any {
	raw, ok := out["media"]
	if !ok || raw == nil {
		return nil
	}
	switch t := raw.(type) {
	case []any:
		var items []map[string]any
		for _, v := range t {
			switch m := v.(type) {
			case map[string]any:
				items = append(items, m)
			default:
				// platform.MediaItem JSON decodes as map with type/url keys
				if b, err := json.Marshal(v); err == nil {
					var mm map[string]any
					if json.Unmarshal(b, &mm) == nil {
						items = append(items, mm)
					}
				}
			}
		}
		return items
	default:
		data, err := json.Marshal(t)
		if err != nil {
			return nil
		}
		var items []map[string]any
		if err := json.Unmarshal(data, &items); err != nil {
			return nil
		}
		return items
	}
}

func fallbackMediaItems(out map[string]any) []map[string]any {
	seen := make(map[string]struct{})
	var items []map[string]any
	add := func(rawURL string) {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" || !strings.HasPrefix(rawURL, "http") {
			return
		}
		if _, ok := seen[rawURL]; ok {
			return
		}
		seen[rawURL] = struct{}{}
		items = append(items, map[string]any{"type": "image", "url": rawURL})
	}
	if md, ok := out["markdown"].(string); ok {
		for _, m := range markdownImageRe.FindAllStringSubmatch(md, -1) {
			if len(m) > 1 {
				add(m[1])
			}
		}
	}
	if htmlBody, ok := out["html"].(string); ok && strings.TrimSpace(htmlBody) != "" {
		for _, u := range imageURLsFromHTML(htmlBody) {
			add(u)
		}
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

func imageURLsFromHTML(body string) []string {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil
	}
	var urls []string
	seen := make(map[string]struct{})
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" {
			for _, a := range n.Attr {
				if a.Key != "src" && a.Key != "data-src" {
					continue
				}
				u := strings.TrimSpace(a.Val)
				if u == "" || strings.HasPrefix(u, "data:") {
					continue
				}
				if _, ok := seen[u]; !ok {
					seen[u] = struct{}{}
					urls = append(urls, u)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return urls
}

func refererForDownload(out map[string]any) string {
	if platform, _ := out["platform"].(string); platform == "wechat" {
		return "https://mp.weixin.qq.com/"
	}
	for _, key := range []string{"source_url", "canonical_url"} {
		if u, _ := out[key].(string); strings.Contains(u, "mp.weixin.qq.com") {
			return "https://mp.weixin.qq.com/"
		}
	}
	return ""
}

func assetFilename(rawURL, mediaType string, index int) string {
	u, err := url.Parse(rawURL)
	ext := ".bin"
	if err == nil {
		base := filepath.Base(u.Path)
		if e := filepath.Ext(base); e != "" && len(e) <= 8 {
			ext = e
		}
	}
	if ext == ".bin" {
		switch strings.ToLower(mediaType) {
		case "image":
			ext = ".jpg"
		case "video":
			ext = ".mp4"
		}
	}
	return fmt.Sprintf("%03d%s", index+1, ext)
}

func normalizeDownloadedAssetName(name, dest, rawURL, mediaType, contentType string) (string, string, error) {
	ext := assetExtension(rawURL, mediaType, contentType, dest)
	currentExt := strings.ToLower(filepath.Ext(name))
	if ext == "" || ext == currentExt {
		return name, dest, nil
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	finalName := base + ext
	finalDest := filepath.Join(filepath.Dir(dest), finalName)
	if finalDest == dest {
		return finalName, finalDest, nil
	}
	if err := os.Rename(dest, finalDest); err != nil {
		return "", "", err
	}
	return finalName, finalDest, nil
}

func assetExtension(rawURL, mediaType, contentType, path string) string {
	if ext := extensionFromContentType(contentType); ext != "" {
		return ext
	}
	if path != "" {
		if ext := extensionFromContentType(detectFileContentType(path)); ext != "" {
			return ext
		}
	}
	if ext := knownExtensionFromURL(rawURL); ext != "" {
		return ext
	}
	switch strings.ToLower(mediaType) {
	case "image":
		return ".jpg"
	case "video":
		return ".mp4"
	default:
		return ".bin"
	}
}

func extensionFromContentType(contentType string) string {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch contentType {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp", "image/x-ms-bmp":
		return ".bmp"
	case "image/avif":
		return ".avif"
	case "image/svg+xml":
		return ".svg"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	default:
		return ""
	}
}

func detectFileContentType(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return ""
	}
	if n == 0 {
		return ""
	}
	return http.DetectContentType(buf[:n])
}

func knownExtensionFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	path := strings.ToLower(u.EscapedPath())
	for _, ext := range []string{".jpeg", ".jpg", ".png", ".gif", ".webp", ".bmp", ".avif", ".svg", ".mp4", ".webm", ".mov"} {
		if strings.Contains(path, ext) {
			if ext == ".jpeg" {
				return ".jpg"
			}
			return ext
		}
	}
	return ""
}

func downloadFile(ctx context.Context, client *http.Client, rawURL, dest string, maxBytes int64, referer string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.ContentLength > maxBytes {
		return "", fmt.Errorf("content length %d exceeds max %d", resp.ContentLength, maxBytes)
	}
	f, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer f.Close()
	limited := io.LimitReader(resp.Body, maxBytes+1)
	n, err := io.Copy(f, limited)
	if err != nil {
		return "", err
	}
	if n > maxBytes {
		_ = os.Remove(dest)
		return "", fmt.Errorf("download exceeds max bytes %d", maxBytes)
	}
	return resp.Header.Get("Content-Type"), nil
}

func writeAssetsManifest(assetsDir string, out map[string]any, downloaded []map[string]any) ([]string, error) {
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		return nil, err
	}
	manifest := map[string]any{
		"media":                  out["media"],
		"screenshot_artifact_id": out["screenshot_artifact_id"],
		"screenshot_local":       out["screenshot_local"],
	}
	if downloaded != nil {
		manifest["downloaded"] = downloaded
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(assetsDir, "media.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, err
	}
	return []string{path}, nil
}
