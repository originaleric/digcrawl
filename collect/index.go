package collect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const indexVersion = 1

// IndexEntry records one collected item in the local index.
type IndexEntry struct {
	ID          string            `json:"id"`
	URL         string            `json:"url"`
	Platform    string            `json:"platform"`
	Title       string            `json:"title"`
	CollectedAt string            `json:"collected_at"`
	BaseName    string            `json:"base_name"`
	Dir         string            `json:"dir"`
	Files       map[string]string `json:"files,omitempty"`
	AssetsDir   string            `json:"assets_dir,omitempty"`
}

// Index is the top-level collect index file.
type Index struct {
	Version   int          `json:"version"`
	UpdatedAt string       `json:"updated_at"`
	Entries   []IndexEntry `json:"entries"`
}

// IndexPath returns the index.json path under output root.
func IndexPath(outputRoot string) string {
	return filepath.Join(outputRoot, "index.json")
}

// LoadIndex reads or initializes the collect index.
func LoadIndex(outputRoot string) (*Index, error) {
	path := IndexPath(outputRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Index{Version: indexVersion, Entries: []IndexEntry{}}, nil
		}
		return nil, err
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	if idx.Version == 0 {
		idx.Version = indexVersion
	}
	return &idx, nil
}

// SaveIndex writes the index atomically.
func SaveIndex(outputRoot string, idx *Index) error {
	if idx == nil {
		return nil
	}
	idx.Version = indexVersion
	idx.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	path := IndexPath(outputRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// FindByURL returns the first index entry for a canonical URL.
func FindByURL(outputRoot, rawURL string) (IndexEntry, bool, error) {
	idx, err := LoadIndex(outputRoot)
	if err != nil {
		return IndexEntry{}, false, err
	}
	key := normalizeURLKey(rawURL)
	for _, e := range idx.Entries {
		if normalizeURLKey(e.URL) == key {
			return e, true, nil
		}
	}
	return IndexEntry{}, false, nil
}

// UpsertEntry replaces an entry with the same URL or appends a new one.
func UpsertEntry(outputRoot string, entry IndexEntry) error {
	idx, err := LoadIndex(outputRoot)
	if err != nil {
		return err
	}
	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	if entry.CollectedAt == "" {
		entry.CollectedAt = time.Now().UTC().Format(time.RFC3339)
	}
	key := normalizeURLKey(entry.URL)
	replaced := false
	for i, e := range idx.Entries {
		if normalizeURLKey(e.URL) == key {
			entry.ID = e.ID
			idx.Entries[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		idx.Entries = append(idx.Entries, entry)
	}
	return SaveIndex(outputRoot, idx)
}

func normalizeURLKey(raw string) string {
	return strings.TrimSuffix(strings.TrimSpace(strings.ToLower(raw)), "/")
}

// EntryFromExport builds an index entry from export output.
func EntryFromExport(url, platform string, out map[string]any, paths OutputPaths, written []string) IndexEntry {
	title, _ := out["title"].(string)
	files := map[string]string{}
	for _, w := range written {
		switch filepath.Ext(w) {
		case ".md":
			files["md"] = w
		case ".html":
			files["html"] = w
		case ".json":
			files["json"] = w
		}
	}
	return IndexEntry{
		ID:          uuid.NewString(),
		URL:         url,
		Platform:    platform,
		Title:       title,
		CollectedAt: time.Now().UTC().Format(time.RFC3339),
		BaseName:    paths.BaseName,
		Dir:         paths.Dir,
		Files:       files,
		AssetsDir:   paths.Assets,
	}
}
