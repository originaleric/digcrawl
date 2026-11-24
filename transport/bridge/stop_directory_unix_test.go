//go:build !windows

package bridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/internal/browser"
)

func TestStopUsesRecordedDirectoryThroughLegacyMigration(t *testing.T) {
	ownerDirectory := t.TempDir()
	unrelatedDirectory := t.TempDir()
	t.Chdir(unrelatedDirectory)
	profile := filepath.Join(t.TempDir(), "profile.yaml")
	if err := os.WriteFile(profile, []byte("LocalBrowser:\n  UserDataDir: storage/browser\n"), 0600); err != nil {
		t.Fatal(err)
	}
	unrelated, _, err := browser.AcquireProfileLock(filepath.Join(unrelatedDirectory, "storage/browser"))
	if err != nil {
		t.Fatal(err)
	}
	defer unrelated.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := StopInDirectory(ctx, profile, ownerDirectory); err != nil {
		t.Fatalf("unrelated cwd contaminated release proof: %v", err)
	}
	owner, _, err := browser.AcquireProfileLock(filepath.Join(ownerDirectory, "storage/browser"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if _, err := StopInDirectory(ctx, profile, ownerDirectory); !errors.Is(err, browser.ErrProfileInUse) {
		t.Fatalf("recorded resource lock ignored: %v", err)
	}
}
