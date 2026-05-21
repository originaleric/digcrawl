package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/originaleric/diggate/runtime/artifact"
)

// ExportOptions configures local file export.
type ExportOptions struct {
	OutputDir       string
	Formats         []string
	Now             time.Time
	DownloadMedia   bool
	MaxMediaBytes   int64
	MediaTimeoutSec int
	ArtifactStore   artifact.Store
}

// ExportResult lists written files.
type ExportResult struct {
	Paths   OutputPaths
	Written []string
}

// Export writes collect output to Markdown / HTML / JSON, downloads media, and updates assets manifest.
func Export(ctx context.Context, out map[string]any, opts ExportOptions) (ExportResult, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	formats := normalizeExportFormats(opts.Formats)
	title, _ := out["title"].(string)
	paths, err := BuildOutputPaths(opts.OutputDir, title, opts.Now)
	if err != nil {
		return ExportResult{}, err
	}
	paths, err = UniquePaths(paths, formats)
	if err != nil {
		return ExportResult{}, err
	}

	var written []string

	if containsFormat(formats, "md") {
		body := pickMarkdownBody(out)
		if err := os.WriteFile(paths.Markdown, []byte(body), 0o644); err != nil {
			return ExportResult{}, err
		}
		written = append(written, paths.Markdown)
	}

	if containsFormat(formats, "html") {
		body := pickHTMLBody(out)
		if body != "" {
			if err := os.WriteFile(paths.HTML, []byte(body), 0o644); err != nil {
				return ExportResult{}, err
			}
			written = append(written, paths.HTML)
		}
	}

	if containsFormat(formats, "json") {
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return ExportResult{}, err
		}
		if err := os.WriteFile(paths.JSON, data, 0o644); err != nil {
			return ExportResult{}, err
		}
		written = append(written, paths.JSON)
	}

	var downloaded []map[string]any
	if opts.DownloadMedia {
		assetFiles, err := DownloadAssets(ctx, paths.Assets, out, AssetDownloadOptions{
			MaxBytes:      opts.MaxMediaBytes,
			TimeoutSec:    opts.MediaTimeoutSec,
			ArtifactStore: opts.ArtifactStore,
		})
		if err != nil {
			return ExportResult{}, err
		}
		written = append(written, assetFiles...)
		if data, err := os.ReadFile(filepath.Join(paths.Assets, "media.json")); err == nil {
			var m map[string]any
			if json.Unmarshal(data, &m) == nil {
				if d, ok := m["downloaded"].([]any); ok {
					for _, item := range d {
						if mm, ok := item.(map[string]any); ok {
							downloaded = append(downloaded, mm)
						}
					}
				}
			}
		}
	} else {
		manifestFiles, err := writeAssetsManifest(paths.Assets, out, nil)
		if err != nil {
			return ExportResult{}, err
		}
		written = append(written, manifestFiles...)
	}

	if containsFormat(formats, "md") && len(downloaded) > 0 {
		_ = RewriteMarkdownAssets(paths.Markdown, paths.Assets, downloaded)
	}

	return ExportResult{Paths: paths, Written: written}, nil
}

func normalizeExportFormats(formats []string) []string {
	if len(formats) == 0 {
		return []string{"md", "json"}
	}
	out := make([]string, 0, len(formats))
	for _, f := range formats {
		f = strings.ToLower(strings.TrimSpace(f))
		switch f {
		case "md", "markdown":
			out = append(out, "md")
		case "html", "htm":
			out = append(out, "html")
		case "json":
			out = append(out, "json")
		}
	}
	if len(out) == 0 {
		return []string{"md", "json"}
	}
	return out
}

func containsFormat(formats []string, want string) bool {
	for _, f := range formats {
		if f == want {
			return true
		}
	}
	return false
}

func pickMarkdownBody(out map[string]any) string {
	if md, ok := out["markdown"].(string); ok && strings.TrimSpace(md) != "" {
		return buildMarkdownDocument(out, md)
	}
	if text, ok := out["text"].(string); ok && strings.TrimSpace(text) != "" {
		return buildMarkdownDocument(out, text)
	}
	return buildMarkdownDocument(out, "")
}

func buildMarkdownDocument(out map[string]any, body string) string {
	title, _ := out["title"].(string)
	var b strings.Builder
	if title != "" {
		b.WriteString("# ")
		b.WriteString(title)
		b.WriteString("\n\n")
	}
	if src, ok := out["source_url"].(string); ok && src != "" {
		b.WriteString("> Source: ")
		b.WriteString(src)
		b.WriteString("\n\n")
	}
	if body != "" {
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func pickHTMLBody(out map[string]any) string {
	if html, ok := out["html"].(string); ok && strings.TrimSpace(html) != "" {
		return html
	}
	title, _ := out["title"].(string)
	text, _ := out["text"].(string)
	if text == "" {
		return ""
	}
	return fmt.Sprintf("<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>%s</title></head><body><pre>%s</pre></body></html>",
		escapeHTML(title), escapeHTML(text))
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
