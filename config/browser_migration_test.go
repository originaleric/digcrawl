package config

import (
	"context"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestBrowserMigrationPreservesSessionsAndOtherConfiguration(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "profile.yaml")
	original := []byte("# private profile\nLocalBrowser:\n  ChromePath: /Applications/Google Chrome.app/Contents/MacOS/Google Chrome\n  Headless: false\n  CookieStoreDir: /private/cookies\n  UserDataDir: /private/profiles\n  ProxyURL: http://127.0.0.1:8080\n  AllowedDomains: [example.com]\nGateway:\n  AuthToken: secret-fixture\nUnrelated: preserved\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := MigrateBrowserRuntimeProfile(context.Background(), path); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err = yaml.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	local := values["LocalBrowser"].(map[string]any)
	if local["ChromePath"] != "" || local["Headless"] != true || local["CookieStoreDir"] != "/private/cookies" || local["UserDataDir"] != "/private/profiles" || values["Unrelated"] != "preserved" || !strings.Contains(string(data), "# private profile") {
		t.Fatal("migration changed unrelated configuration")
	}
	backup, err := os.ReadFile(path + ".before-isolated-browser")
	if err != nil || string(backup) != string(original) {
		t.Fatal("original profile backup changed")
	}
	info, _ := os.Stat(path + ".before-isolated-browser")
	if info.Mode().Perm() != 0600 {
		t.Fatal("private backup permissions")
	}
}

func TestBrowserMigrationRejectsUntrustedBackupPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires local privilege on Windows")
	}
	path := filepath.Join(t.TempDir(), "profile.yaml")
	original := []byte("LocalBrowser:\n  ChromePath: desktop\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other")
	if err := os.WriteFile(other, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, path+".before-isolated-browser"); err != nil {
		t.Fatal(err)
	}
	if err := MigrateBrowserRuntimeProfile(context.Background(), path); err == nil {
		t.Fatal("nonregular backup accepted")
	}
	data, _ := os.ReadFile(path)
	if string(data) != string(original) {
		t.Fatal("profile changed after backup rejection")
	}
	data, _ = os.ReadFile(other)
	if string(data) != "preserve" {
		t.Fatal("unrelated backup target changed")
	}
}

func TestReviewBrowserMigrationResolvesYAMLAlias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.yaml")
	data := []byte("BrowserDefaults: &browser\n  ChromePath: /legacy/desktop/chrome\n  Headless: false\n  CookieStoreDir: /private/cookies\nLocalBrowser: *browser\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := MigrateBrowserRuntimeProfile(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		LocalBrowser    LocalBrowserConfig `yaml:"LocalBrowser"`
		BrowserDefaults LocalBrowserConfig `yaml:"BrowserDefaults"`
	}
	if err := yaml.Unmarshal(saved, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.BrowserDefaults.ChromePath != "/legacy/desktop/chrome" || cfg.LocalBrowser.CookieStoreDir != "/private/cookies" {
		t.Fatal("shared anchor or cookies changed")
	}
	if cfg.LocalBrowser.ChromePath != "" || cfg.LocalBrowser.Headless == nil || !*cfg.LocalBrowser.Headless {
		t.Fatalf("migration reported success but retained old browser configuration: ChromePath=%q Headless=%v", cfg.LocalBrowser.ChromePath, cfg.LocalBrowser.Headless)
	}
}

func TestBrowserMigrationNullMapping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.yaml")
	if err := os.WriteFile(path, []byte("LocalBrowser: null\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := MigrateBrowserRuntimeProfile(context.Background(), path); err != nil {
		t.Fatal(err)
	}
}

func TestBrowserMigrationNullAlias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.yaml")
	if err := os.WriteFile(path, []byte("Defaults: &empty null\nLocalBrowser: *empty # keep note\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := MigrateBrowserRuntimeProfile(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "keep note") {
		t.Fatal("alias comment lost", err)
	}
}

func TestReviewMigrationKeepsScalarAnchorConsumers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.yaml")
	data := []byte("LocalBrowser:\n  ChromePath: &chrome /legacy/chrome\n  Headless: false\nOtherPath: *chrome\n")
	var original Config
	if err := yaml.Unmarshal(data, &original); err != nil {
		t.Fatal("invalid fixture", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := MigrateBrowserRuntimeProfile(context.Background(), path); err != nil {
		t.Fatal("valid shared scalar anchor prevented migration:", err)
	}
}
func TestReviewMigrationKeepsMappingAnchorConsumers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.yaml")
	data := []byte("LocalBrowser: &browser\n  ChromePath: /legacy/chrome\n  Headless: false\nOtherSettings: *browser\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := MigrateBrowserRuntimeProfile(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(path)
	var decoded struct {
		OtherSettings LocalBrowserConfig `yaml:"OtherSettings"`
	}
	if err := yaml.Unmarshal(saved, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.OtherSettings.ChromePath != "/legacy/chrome" {
		t.Fatalf("migration changed unrelated alias consumer: ChromePath=%q", decoded.OtherSettings.ChromePath)
	}
}

func TestBrowserMigrationPreservesSpecialYAMLValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.yaml")
	data := []byte("LocalBrowser: {}\nUnrelated: {number: .nan, date: 2026-11-08, text: 'true'}\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := MigrateBrowserRuntimeProfile(context.Background(), path); err != nil {
		t.Fatal(err)
	}
}

func TestBrowserMigrationEffectiveConfigurationMatrix(t *testing.T) {
	styles := []string{"direct", "alias", "root-merge", "merge-list", "merge-chain", "local-merge", "override", "root-override"}
	for _, style := range styles {
		for _, sharing := range []string{"none", "values", "mapping-and-values"} {
			t.Run(style+"/"+sharing, func(t *testing.T) {
				anchors := sharing != "none"
				anchor := func(name string) string {
					if anchors {
						return "&" + name + " "
					}
					return ""
				}
				fields := fmt.Sprintf("ChromePath: %s/legacy/chrome # path-note\nHeadless: %sfalse\nCookieStoreDir: %s/preserve/cookies\nProxyURL: http://127.0.0.1:8080\nExtraArgs: %s[--lang=zh-CN]\n", anchor("path"), anchor("visible"), anchor("cookie"), anchor("args"))
				indent := func(n int) string {
					return strings.Repeat(" ", n) + strings.ReplaceAll(strings.TrimSuffix(fields, "\n"), "\n", "\n"+strings.Repeat(" ", n)) + "\n"
				}
				var data string
				switch style {
				case "direct":
					data = "LocalBrowser: &browser\n" + indent(2)
				case "alias":
					data = "Template: &browser\n" + indent(2) + "LocalBrowser: *browser # alias-note\n"
				case "root-merge":
					data = "Defaults: &defaults\n  LocalBrowser: &browser\n" + indent(4) + "<<: *defaults\n"
				case "merge-list":
					data = "Defaults: &defaults\n  LocalBrowser: &browser\n" + indent(4) + "Other: &other {Label: preserved}\n<<: [*defaults, *other]\n"
				case "merge-chain":
					data = "Defaults: &defaults\n  LocalBrowser: &browser\n" + indent(4) + "Inherited: &inherited {<<: *defaults}\n<<: *inherited\n"
				case "local-merge":
					data = "Template: &browser\n" + indent(2) + "LocalBrowser:\n  <<: *browser # merge-note\n"
				case "override":
					data = "Template: &browser\n" + indent(2) + "LocalBrowser:\n  <<: *browser\n  ProxyURL: http://127.0.0.1:9999\n"
				case "root-override":
					data = "Defaults: &defaults\n  LocalBrowser: &browser\n" + indent(4) + "<<: *defaults\nLocalBrowser: {CookieStoreDir: /explicit/cookies}\n"
				}
				if anchors {
					data += "OtherPath: *path\nOtherVisible: *visible\nOtherCookie: *cookie\nOtherArgs: *args\n"
				}
				if sharing == "mapping-and-values" {
					data += "OtherBrowser: *browser\n"
				}
				data += "Unrelated: {number: .nan, text: 'true', date: 2026-11-08}\n"
				type snapshot struct {
					LocalBrowser LocalBrowserConfig `yaml:"LocalBrowser"`
					OtherBrowser LocalBrowserConfig `yaml:"OtherBrowser"`
					OtherPath    string             `yaml:"OtherPath"`
					OtherCookie  string             `yaml:"OtherCookie"`
					OtherVisible *bool              `yaml:"OtherVisible"`
					OtherArgs    []string           `yaml:"OtherArgs"`
				}
				var before snapshot
				if err := yaml.Unmarshal([]byte(data), &before); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(t.TempDir(), "profile.yaml")
				if err := os.WriteFile(path, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
				if err := MigrateBrowserRuntimeProfile(context.Background(), path); err != nil {
					t.Fatal(err)
				}
				first, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var after snapshot
				if err := yaml.Unmarshal(first, &after); err != nil {
					t.Fatal(err)
				}
				headless := true
				before.LocalBrowser.ChromePath = ""
				before.LocalBrowser.Headless = &headless
				if !reflect.DeepEqual(before, after) {
					t.Fatalf("effective values drifted: before=%+v after=%+v", before, after)
				}
				if !strings.Contains(string(first), "path-note") {
					t.Fatal("comments lost")
				}
				if err := MigrateBrowserRuntimeProfile(context.Background(), path); err != nil {
					t.Fatal(err)
				}
				second, _ := os.ReadFile(path)
				if string(first) != string(second) {
					t.Fatal("migration was not idempotent")
				}
				backup, _ := os.ReadFile(path + ".before-isolated-browser")
				if string(backup) != data {
					t.Fatal("original backup changed")
				}
			})
		}
	}
}
