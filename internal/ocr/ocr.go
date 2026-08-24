package ocr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform"
)

var (
	markdownImageRE = regexp.MustCompile(`!\[[^\]]*\]\((https?://[^\s\)]+)\)`)
	htmlImageRE     = regexp.MustCompile(`(?is)<img\b[^>]*(?:src|data-src)=["']([^"']+)["'][^>]*>`)
)

// ImageRef identifies one OCR candidate image.
type ImageRef struct {
	URL       string
	LocalPath string
	DataURL   string
	Source    string
	Type      string
}

// Result stores OCR output for one image.
type Result struct {
	SourceURL string `json:"source_url,omitempty"`
	LocalPath string `json:"local_path,omitempty"`
	Source    string `json:"source,omitempty"`
	Type      string `json:"type,omitempty"`
	Text      string `json:"text,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	Error     string `json:"error,omitempty"`
}

// EnrichOptions controls OCR enrichment source selection.
type EnrichOptions struct {
	AssetsDir   string
	Downloaded  []map[string]any
	ExtraImages []ImageRef
	Force       bool
}

type provider interface {
	Recognize(ctx context.Context, ref ImageRef) (string, error)
	Name() string
	Model() string
}

// EnrichContent adds OCR metadata and optional markdown/text sections to Content.
func EnrichContent(ctx context.Context, content *platform.Content, cfg config.OCRConfig) []Result {
	return EnrichContentWithOptions(ctx, content, cfg, EnrichOptions{Force: false})
}

// EnrichContentWithOptions adds OCR metadata and optional markdown/text sections to Content.
func EnrichContentWithOptions(ctx context.Context, content *platform.Content, cfg config.OCRConfig, opts EnrichOptions) []Result {
	if content == nil || !cfg.EnabledOrDefault() {
		return nil
	}
	out := content.ToMap()
	results := EnrichMap(ctx, out, cfg, opts)
	if len(results) == 0 {
		return nil
	}
	if md, _ := out["markdown"].(string); md != "" {
		content.Markdown = md
	}
	if text, _ := out["text"].(string); text != "" {
		content.Text = text
	}
	if meta, ok := out["platform_metadata"].(map[string]any); ok {
		content.PlatformMetadata = meta
	}
	return results
}

// EnrichMap adds OCR metadata and optional markdown/text sections to a generic output map.
func EnrichMap(ctx context.Context, out map[string]any, cfg config.OCRConfig, opts EnrichOptions) []Result {
	if out == nil || !cfg.EnabledOrDefault() {
		return nil
	}
	meta := ensureMeta(out)
	if !opts.Force && hasOCRResults(meta) {
		if !cfg.VideoFrames.EnabledOrDefault() {
			return nil
		}
		refs := pendingVideoFrameRefs(opts.ExtraImages, meta)
		if len(refs) == 0 {
			return nil
		}
		results, err := recognizeRefs(ctx, cfg, refs)
		if err != nil {
			meta["ocr_enabled"] = true
			meta["ocr_error"] = err.Error()
			return nil
		}
		writeResults(out, meta, cfg, results, true)
		return results
	}

	refs := collectImageRefs(out, opts, cfg)
	if len(refs) == 0 {
		meta["ocr_enabled"] = true
		meta["ocr_count"] = 0
		return nil
	}

	refs, truncated := limitOCRRefs(refs, cfg)
	results, err := recognizeRefs(ctx, cfg, refs)
	if err != nil {
		meta["ocr_enabled"] = true
		meta["ocr_error"] = err.Error()
		return nil
	}
	if truncated {
		meta["ocr_truncated"] = true
	}

	writeResults(out, meta, cfg, results, false)
	return results
}

func recognizeRefs(ctx context.Context, cfg config.OCRConfig, refs []ImageRef) ([]Result, error) {
	p, err := newProvider(cfg)
	if err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(refs))
	for _, ref := range refs {
		result := Result{
			SourceURL: ref.URL,
			Source:    ref.Source,
			Provider:  p.Name(),
			Model:     p.Model(),
		}
		// Video frames live only in a private temporary directory. Never expose
		// that path in an exported OCR result.
		if ref.Type != "video_frame" {
			result.LocalPath = ref.LocalPath
		} else {
			result.Type = ref.Type
		}
		text, err := p.Recognize(ctx, ref)
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Text = strings.TrimSpace(text)
		}
		results = append(results, result)
	}
	return results, nil
}

// limitOCRRefs keeps OCR.MaxImages for article images and screenshots.
// Extracted video frames have their own per-video and per-content budgets, so
// subtitle OCR cannot silently displace regular image OCR results.
func limitOCRRefs(refs []ImageRef, cfg config.OCRConfig) ([]ImageRef, bool) {
	maxImages := cfg.MaxImagesOrDefault()
	selected := make([]ImageRef, 0, len(refs))
	imageCount := 0
	truncated := false
	for _, ref := range refs {
		if ref.Type == "video_frame" {
			selected = append(selected, ref)
			continue
		}
		if imageCount >= maxImages {
			truncated = true
			continue
		}
		selected = append(selected, ref)
		imageCount++
	}
	return selected, truncated
}

// pendingVideoFrameRefs returns only video frames that have not already been
// recorded in OCR metadata. Existing OCR results intentionally remain an
// idempotency barrier for regular images and screenshots.
func pendingVideoFrameRefs(extra []ImageRef, meta map[string]any) []ImageRef {
	seenSources := make(map[string]struct{})
	for _, result := range ocrResultMaps(meta) {
		kind, _ := result["type"].(string)
		if !strings.EqualFold(strings.TrimSpace(kind), "video_frame") {
			continue
		}
		source, _ := result["source"].(string)
		if source = strings.TrimSpace(source); source != "" {
			seenSources[source] = struct{}{}
		}
	}
	pending := make([]ImageRef, 0, len(extra))
	for _, ref := range extra {
		if !strings.EqualFold(strings.TrimSpace(ref.Type), "video_frame") {
			continue
		}
		source := strings.TrimSpace(ref.Source)
		if source != "" {
			if _, ok := seenSources[source]; ok {
				continue
			}
			seenSources[source] = struct{}{}
		}
		pending = append(pending, ref)
	}
	return pending
}

func newProvider(cfg config.OCRConfig) (provider, error) {
	switch cfg.ProviderOrDefault() {
	case "stub":
		return stubProvider{model: cfg.ModelOrDefault()}, nil
	case "openai_compatible":
		key := strings.TrimSpace(cfg.APIKey)
		if key == "" {
			return nil, fmt.Errorf("OCR APIKey is empty")
		}
		return &openAICompatibleProvider{
			apiKey:    key,
			baseURL:   cfg.BaseURLOrDefault(),
			model:     cfg.ModelOrDefault(),
			prompt:    cfg.PromptOrDefault(),
			timeout:   time.Duration(cfg.TimeoutSecOrDefault()) * time.Second,
			maxBytes:  cfg.MaxImageBytesOrDefault(),
			maxTokens: cfg.MaxTokensOrDefault(),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported OCR provider %q", cfg.Provider)
	}
}

func collectImageRefs(out map[string]any, opts EnrichOptions, cfg config.OCRConfig) []ImageRef {
	seen := map[string]struct{}{}
	var refs []ImageRef
	add := func(ref ImageRef) {
		ref.URL = strings.TrimSpace(ref.URL)
		ref.LocalPath = strings.TrimSpace(ref.LocalPath)
		ref.DataURL = strings.TrimSpace(ref.DataURL)
		ref.Source = strings.TrimSpace(ref.Source)
		ref.Type = strings.ToLower(strings.TrimSpace(ref.Type))
		if ref.Type != "" && ref.Type != "image" && ref.Type != "photo" && ref.Type != "screenshot" && ref.Type != "video_frame" {
			return
		}
		if ref.Type == "video_frame" && !cfg.VideoFrames.EnabledOrDefault() {
			return
		}
		key := imageRefKey(ref)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		if ref.URL != "" {
			if _, ok := seen[ref.URL]; ok {
				return
			}
		}
		if ref.LocalPath != "" {
			if _, ok := seen[ref.LocalPath]; ok {
				return
			}
		}
		seen[key] = struct{}{}
		if ref.URL != "" {
			seen[ref.URL] = struct{}{}
		}
		if ref.LocalPath != "" {
			seen[ref.LocalPath] = struct{}{}
		}
		refs = append(refs, ref)
	}

	for _, ref := range opts.ExtraImages {
		add(ref)
	}
	for _, item := range opts.Downloaded {
		local, _ := item["local"].(string)
		if local != "" && opts.AssetsDir != "" {
			local = filepath.Join(opts.AssetsDir, local)
		}
		rawURL, _ := item["url"].(string)
		mediaType, _ := item["type"].(string)
		add(ImageRef{URL: rawURL, LocalPath: local, Type: mediaType})
	}

	for _, item := range mediaItems(out) {
		rawURL, _ := item["url"].(string)
		mediaType, _ := item["type"].(string)
		add(ImageRef{URL: rawURL, Type: mediaType})
	}
	for _, rawURL := range imageURLsFromTextMap(out) {
		add(ImageRef{URL: rawURL, Type: "image"})
	}
	return refs
}

func imageRefKey(ref ImageRef) string {
	switch {
	case ref.LocalPath != "":
		return ref.LocalPath
	case ref.URL != "":
		return ref.URL
	case ref.DataURL != "":
		return fmt.Sprintf("data:%x", sha256.Sum256([]byte(ref.DataURL)))
	case ref.Source != "":
		return "source:" + ref.Source
	default:
		return ""
	}
}

// PNGDataURLImageRef builds an inline screenshot image reference from PNG base64 data.
func PNGDataURLImageRef(source, b64 string) ImageRef {
	b64 = strings.TrimSpace(b64)
	if b64 == "" {
		return ImageRef{}
	}
	return ImageRef{
		DataURL: "data:image/png;base64," + b64,
		Source:  strings.TrimSpace(source),
		Type:    "screenshot",
	}
}

func mediaItems(out map[string]any) []map[string]any {
	raw := out["media"]
	switch t := raw.(type) {
	case []any:
		return normalizeMediaItems(t)
	default:
		if b, err := json.Marshal(t); err == nil {
			var items []any
			if json.Unmarshal(b, &items) == nil {
				return normalizeMediaItems(items)
			}
		}
		return nil
	}
}

func normalizeMediaItems(items []any) []map[string]any {
	var outItems []map[string]any
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			outItems = append(outItems, m)
			continue
		}
		if b, err := json.Marshal(item); err == nil {
			var m map[string]any
			if json.Unmarshal(b, &m) == nil {
				outItems = append(outItems, m)
			}
		}
	}
	return outItems
}

func imageURLsFromTextMap(out map[string]any) []string {
	seen := map[string]struct{}{}
	var urls []string
	add := func(rawURL string) {
		rawURL = strings.TrimSpace(html.UnescapeString(rawURL))
		if rawURL == "" || !strings.HasPrefix(rawURL, "http") {
			return
		}
		if _, ok := seen[rawURL]; ok {
			return
		}
		seen[rawURL] = struct{}{}
		urls = append(urls, rawURL)
	}
	if md, _ := out["markdown"].(string); md != "" {
		for _, m := range markdownImageRE.FindAllStringSubmatch(md, -1) {
			if len(m) > 1 {
				add(m[1])
			}
		}
	}
	if htmlBody, _ := out["html"].(string); htmlBody != "" {
		for _, m := range htmlImageRE.FindAllStringSubmatch(htmlBody, -1) {
			if len(m) > 1 {
				add(m[1])
			}
		}
	}
	return urls
}

func writeResults(out map[string]any, meta map[string]any, cfg config.OCRConfig, results []Result, appendExisting bool) {
	encoded := make([]map[string]any, 0, len(results))
	successCount := 0
	for _, r := range results {
		m := map[string]any{
			"provider": r.Provider,
			"model":    r.Model,
		}
		if r.SourceURL != "" {
			m["source_url"] = r.SourceURL
		}
		if r.LocalPath != "" {
			m["local_path"] = r.LocalPath
		}
		if r.Source != "" {
			m["source"] = r.Source
		}
		if r.Type != "" {
			m["type"] = r.Type
		}
		if r.Text != "" {
			m["text"] = r.Text
			successCount++
		}
		if r.Error != "" {
			m["error"] = r.Error
		}
		encoded = append(encoded, m)
	}
	if appendExisting {
		existing := ocrResultMaps(meta)
		encoded = append(existing, encoded...)
		for _, result := range existing {
			if text, _ := result["text"].(string); strings.TrimSpace(text) != "" {
				successCount++
			}
		}
	}
	meta["ocr_enabled"] = true
	meta["ocr_count"] = successCount
	meta["ocr_results"] = encoded
	if successCount == 0 {
		return
	}
	if cfg.AppendToMarkdownOrDefault() {
		out["markdown"] = appendOCRSection(stringValue(out, "markdown"), results)
	}
	if cfg.AppendToTextOrDefault() {
		out["text"] = appendOCRSection(stringValue(out, "text"), results)
	}
}

func ocrResultMaps(meta map[string]any) []map[string]any {
	if meta == nil {
		return nil
	}
	switch results := meta["ocr_results"].(type) {
	case []map[string]any:
		return results
	case []any:
		maps := make([]map[string]any, 0, len(results))
		for _, raw := range results {
			if result, ok := raw.(map[string]any); ok {
				maps = append(maps, result)
			}
		}
		return maps
	default:
		return nil
	}
}

func appendOCRSection(body string, results []Result) string {
	var b strings.Builder
	body = strings.TrimRight(body, "\n")
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	b.WriteString("## OCR\n\n")
	index := 1
	for _, r := range results {
		if strings.TrimSpace(r.Text) == "" {
			continue
		}
		label := "Image"
		if r.Type == "video_frame" {
			label = "Video frame"
		}
		b.WriteString(fmt.Sprintf("### %s %d\n\n", label, index))
		b.WriteString(strings.TrimSpace(r.Text))
		b.WriteString("\n\n")
		index++
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func ensureMeta(out map[string]any) map[string]any {
	if meta, ok := out["platform_metadata"].(map[string]any); ok && meta != nil {
		return meta
	}
	meta := map[string]any{}
	out["platform_metadata"] = meta
	return meta
}

func hasOCRResults(meta map[string]any) bool {
	if meta == nil {
		return false
	}
	switch v := meta["ocr_results"].(type) {
	case []any:
		return len(v) > 0
	case []map[string]any:
		return len(v) > 0
	default:
		return false
	}
}

func stringValue(out map[string]any, key string) string {
	if out == nil {
		return ""
	}
	s, _ := out[key].(string)
	return s
}

type stubProvider struct {
	model string
}

func (p stubProvider) Recognize(_ context.Context, ref ImageRef) (string, error) {
	if ref.Type == "video_frame" {
		return "stub OCR text from video frame", nil
	}
	src := ref.LocalPath
	if src == "" {
		src = ref.URL
	}
	if src == "" {
		src = ref.Source
	}
	if src == "" && ref.DataURL != "" {
		src = "inline image"
	}
	return "stub OCR text from " + src, nil
}

func (p stubProvider) Name() string { return "stub" }
func (p stubProvider) Model() string {
	if p.model == "" {
		return "stub"
	}
	return p.model
}

type openAICompatibleProvider struct {
	apiKey    string
	baseURL   string
	model     string
	prompt    string
	timeout   time.Duration
	maxBytes  int64
	maxTokens int
}

func (p *openAICompatibleProvider) Name() string  { return "openai_compatible" }
func (p *openAICompatibleProvider) Model() string { return p.model }

func (p *openAICompatibleProvider) Recognize(ctx context.Context, ref ImageRef) (string, error) {
	imageURL, err := p.imageURL(ref)
	if err != nil {
		return "", err
	}
	payload := map[string]any{
		"model":       p.model,
		"temperature": 0,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": p.prompt},
					{"type": "image_url", "image_url": map[string]any{"url": imageURL}},
				},
			},
		},
		"max_tokens": p.maxTokens,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.baseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: p.timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("OCR provider HTTP %d", resp.StatusCode)
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
			Text string `json:"text"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("OCR provider response decode: %w", err)
	}
	for _, choice := range parsed.Choices {
		if text := ocrResponseText(choice.Message.Content); text != "" {
			return text, nil
		}
		if text := strings.TrimSpace(choice.Text); text != "" {
			return text, nil
		}
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("OCR provider returned empty text")
	}
	return "", fmt.Errorf("OCR provider returned empty text")
}

func ocrResponseText(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		var parts []string
		for _, item := range v {
			if text := ocrResponseText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		for _, key := range []string{"text", "content"} {
			if text := ocrResponseText(v[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

func (p *openAICompatibleProvider) imageURL(ref ImageRef) (string, error) {
	if ref.DataURL != "" {
		if err := validateDataImageURL(ref.DataURL, p.maxBytes); err != nil {
			return "", err
		}
		return ref.DataURL, nil
	}
	if ref.LocalPath != "" {
		return p.localDataURL(ref.LocalPath)
	}
	if ref.URL == "" {
		return "", fmt.Errorf("OCR image missing URL")
	}
	if err := validateRemoteImageURL(ref.URL); err != nil {
		return "", err
	}
	return ref.URL, nil
}

func (p *openAICompatibleProvider) localDataURL(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Size() > p.maxBytes {
		return "", fmt.Errorf("OCR image exceeds max bytes %d", p.maxBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	contentType := http.DetectContentType(data)
	if !strings.HasPrefix(contentType, "image/") {
		contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	}
	if !strings.HasPrefix(contentType, "image/") {
		return "", fmt.Errorf("OCR file is not an image: %s", contentType)
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func validateDataImageURL(raw string, maxBytes int64) error {
	if !strings.HasPrefix(raw, "data:") {
		return fmt.Errorf("OCR data image must be a data URL")
	}
	header, payload, ok := strings.Cut(raw, ",")
	if !ok || strings.TrimSpace(payload) == "" {
		return fmt.Errorf("OCR data image is malformed")
	}
	header = strings.ToLower(strings.TrimSpace(header))
	if !strings.HasPrefix(header, "data:image/") {
		return fmt.Errorf("OCR data URL is not an image")
	}
	if !strings.Contains(header, ";base64") {
		return fmt.Errorf("OCR data image must be base64 encoded")
	}
	if decodedLenExceeds(payload, maxBytes) {
		return fmt.Errorf("OCR image exceeds max bytes %d", maxBytes)
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return fmt.Errorf("OCR data image base64 decode: %w", err)
	}
	if int64(len(decoded)) > maxBytes {
		return fmt.Errorf("OCR image exceeds max bytes %d", maxBytes)
	}
	return nil
}

func decodedLenExceeds(payload string, maxBytes int64) bool {
	if maxBytes <= 0 {
		return false
	}
	strippedLen := 0
	for i := 0; i < len(payload); i++ {
		switch payload[i] {
		case '\r', '\n':
			continue
		default:
			strippedLen++
		}
	}
	if strippedLen == 0 {
		return false
	}
	padding := trailingBase64Padding(payload)
	estimated := int64(base64.StdEncoding.DecodedLen(strippedLen))
	if padding > 0 && int64(padding) < estimated {
		estimated -= int64(padding)
	}
	return estimated > maxBytes
}

func trailingBase64Padding(payload string) int {
	padding := 0
	for i := len(payload) - 1; i >= 0; i-- {
		switch payload[i] {
		case '\r', '\n':
			continue
		case '=':
			padding++
			continue
		default:
			return padding
		}
	}
	return padding
}

func validateRemoteImageURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported OCR image scheme %q", u.Scheme)
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return fmt.Errorf("OCR image URL missing host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("OCR image host %q is private/local", host)
		}
		return nil
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return fmt.Errorf("OCR image host %q is local", host)
	}
	return nil
}
