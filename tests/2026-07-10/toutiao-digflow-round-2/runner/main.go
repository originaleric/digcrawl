package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/summary"
	"gopkg.in/yaml.v3"
)

type collectOutput struct {
	SourceURL        string         `json:"source_url"`
	CanonicalURL     string         `json:"canonical_url"`
	Platform         string         `json:"platform"`
	Title            string         `json:"title"`
	Text             string         `json:"text"`
	PlatformMetadata map[string]any `json:"platform_metadata"`
}

type gormFile struct {
	Gorm struct {
		Mysql struct {
			Write struct {
				Host     string `yaml:"Host"`
				DataBase string `yaml:"DataBase"`
				Port     int    `yaml:"Port"`
				User     string `yaml:"User"`
				Pass     string `yaml:"Pass"`
			} `yaml:"Write"`
		} `yaml:"Mysql"`
	} `yaml:"Gorm"`
}

func main() {
	ctx := context.Background()
	root := "."
	outDir := "tests/2026-07-10/toutiao-digflow-round-2"

	db, err := loadDBConfig("config/gorm.yml")
	if err != nil {
		fatal(err)
	}
	gormConfigPath, cleanup, err := writeTempDigFlowGorm(db)
	if err != nil {
		fatal(err)
	}
	defer cleanup()
	if strings.TrimSpace(os.Getenv("DIGFLOW_SUMMARY_API_KEY")) == "" {
		fatal(errors.New("DIGFLOW_SUMMARY_API_KEY is required"))
	}

	article, srcPath, err := loadCollectOutput(filepath.Join(outDir, "collect-output"))
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(article.Text) == "" {
		fatal(fmt.Errorf("collect output has empty text: %s", srcPath))
	}

	req := summary.Request{
		WatchID:    "prod-test-toutiao-round-2",
		Platform:   valueOr(article.Platform, "toutiao"),
		SourceName: "Toutiao article production test",
		RunTime:    time.Now().UTC().Format(time.RFC3339),
		Items: []summary.Item{{
			ItemID:   metadataString(article.PlatformMetadata, "article_id"),
			URL:      valueOr(article.CanonicalURL, article.SourceURL),
			Title:    article.Title,
			Text:     article.Text,
			Metadata: map[string]any{"platform_metadata": article.PlatformMetadata, "collect_json": srcPath},
		}},
	}
	if req.Items[0].ItemID == "" {
		req.Items[0].ItemID = "1870280855070795"
	}
	if err := writeJSON(filepath.Join(outDir, "summary-request.json"), req); err != nil {
		fatal(err)
	}

	provider := &summary.EmbeddedDigFlowProvider{
		RootDir:           root,
		ConfigPath:        "config/digflow.yml",
		GormConfigPath:    gormConfigPath,
		EinoConfigPath:    "config/eino.yml",
		ManifestPath:      filepath.Join(outDir, "agents/agents.yml"),
		GlobalToolsPath:   "config/global_tools.yml",
		GlobalSkillsDir:   "skills",
		Agent:             "watchlist_summary_agent",
		DisableCheckpoint: true,
		Timeout:           120 * time.Second,
		Budget: summary.InputBudget{
			TextChars:     12000,
			ASRChars:      12000,
			OCRChars:      4000,
			MetadataChars: 2000,
			MaxInputChars: 30000,
		},
	}
	res, err := provider.Summarize(ctx, req)
	if err != nil {
		fatal(err)
	}
	if err := writeJSON(filepath.Join(outDir, "summary-result.json"), res); err != nil {
		fatal(err)
	}

	fmt.Printf("collect_json=%s\n", srcPath)
	fmt.Printf("summary_provider=%s\n", res.Provider)
	fmt.Printf("digest_title=%s\n", res.DigestTitle)
	fmt.Printf("items=%d\n", len(res.Items))
}

func loadDBConfig(path string) (gormFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return gormFile{}, err
	}
	var cfg gormFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return gormFile{}, err
	}
	if cfg.Gorm.Mysql.Write.Port == 0 {
		cfg.Gorm.Mysql.Write.Port = 3306
	}
	if strings.TrimSpace(cfg.Gorm.Mysql.Write.Host) == "" ||
		strings.TrimSpace(cfg.Gorm.Mysql.Write.DataBase) == "" ||
		strings.TrimSpace(cfg.Gorm.Mysql.Write.User) == "" {
		return gormFile{}, errors.New("config/gorm.yml mysql write config is incomplete")
	}
	return cfg, nil
}

func writeTempDigFlowGorm(db gormFile) (string, func(), error) {
	write := db.Gorm.Mysql.Write
	tmp, err := os.CreateTemp("", "digcrawl-digflow-gorm-*.yml")
	if err != nil {
		return "", func() {}, err
	}
	path := tmp.Name()
	cleanup := func() { _ = os.Remove(path) }
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", func() {}, err
	}
	doc := map[string]any{
		"Gorm": map[string]any{
			"UseDbType": "mysql",
			"Mysql": map[string]any{
				"IsInitGolobalGormMysql": 1,
				"SlowThreshold":          30,
				"Write": map[string]any{
					"Host":               os.ExpandEnv(strings.TrimSpace(write.Host)),
					"DataBase":           os.ExpandEnv(strings.TrimSpace(write.DataBase)),
					"Port":               write.Port,
					"Prefix":             "df_",
					"User":               os.ExpandEnv(strings.TrimSpace(write.User)),
					"Pass":               os.ExpandEnv(strings.TrimSpace(write.Pass)),
					"Charset":            "utf8mb4",
					"SetMaxIdleConns":    10,
					"SetMaxOpenConns":    100,
					"SetConnMaxLifetime": 3600,
				},
				"IsOpenReadDb": 0,
			},
			"Sqlite": map[string]any{
				"IsInitGolobalGormSqlite": 0,
				"SlowThreshold":           30,
				"Write": map[string]any{
					"Path":               "./storage/sqlite/digflow.db",
					"SetMaxIdleConns":    1,
					"SetMaxOpenConns":    1,
					"SetConnMaxLifetime": 3600,
				},
				"IsOpenReadDb": 0,
			},
		},
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		_ = tmp.Close()
		cleanup()
		return "", func() {}, err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return path, cleanup, nil
}

func loadCollectOutput(dir string) (collectOutput, string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*", "*.json"))
	if err != nil {
		return collectOutput{}, "", err
	}
	for _, path := range matches {
		if strings.HasSuffix(path, "media.json") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return collectOutput{}, "", err
		}
		var out collectOutput
		if err := json.Unmarshal(data, &out); err != nil {
			return collectOutput{}, "", err
		}
		if strings.TrimSpace(out.Text) != "" {
			return out, path, nil
		}
	}
	return collectOutput{}, "", fmt.Errorf("no collect json with text under %s", dir)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func valueOr(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func metadataString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	value, _ := meta[key].(string)
	return strings.TrimSpace(value)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
