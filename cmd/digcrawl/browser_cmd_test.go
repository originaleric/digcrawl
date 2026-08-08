package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/config"
)

func TestBrowserProfilePathUsesUserSkillProfile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := config.DefaultSkillProfilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("Gateway: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := browserProfilePath("")
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("profile=%q want %q", got, path)
	}
}

func TestBrowserProfilePathRequiresInitialization(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, err := browserProfilePath("")
	if err == nil || !strings.Contains(err.Error(), "digcrawl init --profile skill") {
		t.Fatalf("error=%v", err)
	}
}

func TestBrowserProfilePathPreservesExplicitProfile(t *testing.T) {
	if got, err := browserProfilePath("/tmp/custom-skill.yaml"); err != nil || got != "/tmp/custom-skill.yaml" {
		t.Fatalf("profile=%q err=%v", got, err)
	}
}
