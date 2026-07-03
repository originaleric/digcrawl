package collect

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var unsafeName = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]+`)

const maxFilenameBytes = 120

// OutputPaths describes files written for one collect run.
type OutputPaths struct {
	Dir      string
	BaseName string
	Markdown string
	HTML     string
	JSON     string
	Assets   string
}

// ResolveOutputDir returns the dated output directory under root.
func ResolveOutputDir(root string, t time.Time) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "collect-output"
	}
	day := t.Format("2006-01-02")
	dir := filepath.Join(root, day)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// BuildOutputPaths plans file paths for exported content.
func BuildOutputPaths(root string, title string, t time.Time) (OutputPaths, error) {
	dir, err := ResolveOutputDir(root, t)
	if err != nil {
		return OutputPaths{}, err
	}
	base := sanitizeFilename(title)
	if base == "" {
		base = "untitled"
	}
	return OutputPaths{
		Dir:      dir,
		BaseName: base,
		Markdown: filepath.Join(dir, base+".md"),
		HTML:     filepath.Join(dir, base+".html"),
		JSON:     filepath.Join(dir, base+".json"),
		Assets:   filepath.Join(dir, base+".assets"),
	}, nil
}

func sanitizeFilename(name string) string {
	name = strings.ToValidUTF8(strings.TrimSpace(name), "")
	name = unsafeName.ReplaceAllString(name, "_")
	name = strings.Trim(name, "._ ")
	name = truncateUTF8Bytes(name, maxFilenameBytes)
	name = strings.Trim(name, "._ ")
	return name
}

func truncateUTF8Bytes(name string, maxBytes int) string {
	if maxBytes <= 0 || len(name) <= maxBytes {
		return name
	}
	out := strings.Builder{}
	out.Grow(maxBytes)
	for _, r := range name {
		next := string(r)
		if out.Len()+len(next) > maxBytes {
			break
		}
		out.WriteString(next)
	}
	return out.String()
}

// UniquePaths avoids overwriting when the same title is collected twice.
func UniquePaths(paths OutputPaths, formats []string) (OutputPaths, error) {
	try := paths
	for i := 0; i < 1000; i++ {
		if !anyTargetExists(try, formats) {
			return try, nil
		}
		suffix := fmt.Sprintf("_%d", i+1)
		base := paths.BaseName + suffix
		try = OutputPaths{
			Dir:      paths.Dir,
			BaseName: base,
			Markdown: filepath.Join(paths.Dir, base+".md"),
			HTML:     filepath.Join(paths.Dir, base+".html"),
			JSON:     filepath.Join(paths.Dir, base+".json"),
			Assets:   filepath.Join(paths.Dir, base+".assets"),
		}
	}
	return OutputPaths{}, fmt.Errorf("too many duplicate titles for %q", paths.BaseName)
}

func anyTargetExists(paths OutputPaths, formats []string) bool {
	if len(formats) == 0 {
		formats = []string{"md", "json"}
	}
	for _, f := range formats {
		switch strings.ToLower(strings.TrimSpace(f)) {
		case "md", "markdown":
			if exists(paths.Markdown) {
				return true
			}
		case "html", "htm":
			if exists(paths.HTML) {
				return true
			}
		case "json":
			if exists(paths.JSON) {
				return true
			}
		}
	}
	return exists(paths.Assets)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
