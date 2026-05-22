package collect

import (
	"path/filepath"
	"strings"

	"github.com/originaleric/digcrawl/collect/store"
)

// UpsertInputFromExport builds a SQLite upsert payload from export output.
func UpsertInputFromExport(url string, match Match, out map[string]any, paths OutputPaths, downloaded []map[string]any, outputRoot string) store.UpsertInput {
	title, _ := out["title"].(string)
	contentType, _ := out["content_type"].(string)
	canonical, _ := out["canonical_url"].(string)
	captured, _ := out["captured_at"].(string)
	platform := match.Platform
	if p, ok := out["platform"].(string); ok && strings.TrimSpace(p) != "" {
		platform = p
	}
	if p, ok := out["_collect_platform"].(string); ok && strings.TrimSpace(p) != "" {
		platform = p
	}

	var files []store.FileRecord
	addFile := func(kind, path string) {
		if strings.TrimSpace(path) == "" {
			return
		}
		files = append(files, store.FileRecord{Kind: kind, Path: path})
	}
	addFile("md", paths.Markdown)
	addFile("html", paths.HTML)
	addFile("json", paths.JSON)

	return store.UpsertInput{
		SourceURL:    url,
		CanonicalURL: canonical,
		Platform:     platform,
		ToolName:     match.Tool,
		Title:        title,
		ContentType:  contentType,
		Status:       "success",
		CapturedAt:   captured,
		OutputRoot:   outputRoot,
		BaseName:     paths.BaseName,
		Files:        files,
		Media:        mediaRecordsFromManifest(downloaded, paths.Assets),
	}
}

func mediaRecordsFromManifest(downloaded []map[string]any, assetsDir string) []store.MediaRecord {
	if len(downloaded) == 0 {
		return nil
	}
	assetsBase := filepath.Base(assetsDir)
	var out []store.MediaRecord
	for i, item := range downloaded {
		mediaType, _ := item["type"].(string)
		if mediaType == "" {
			mediaType = "image"
		}
		remote, _ := item["url"].(string)
		local, _ := item["local"].(string)
		errMsg, _ := item["error"].(string)
		if local != "" && assetsBase != "" {
			local = assetsBase + "/" + local
		}
		out = append(out, store.MediaRecord{
			MediaType:     mediaType,
			RemoteURL:     remote,
			LocalPath:     local,
			DownloadError: errMsg,
			SortOrder:     i,
		})
	}
	return out
}
