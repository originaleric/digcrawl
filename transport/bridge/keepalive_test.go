package bridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/internal/browser"
)

func TestMaintenanceControlCoversWholeOperation(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "profile.yaml")
	owner, err := AcquireControl()
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := Pause(profile, true); err != nil {
		t.Fatal(err)
	}
	if competing, err := AcquireControl(); !errors.Is(err, browser.ErrProfileInUse) {
		if competing != nil {
			competing.Close()
		}
		t.Fatalf("concurrent maintenance accepted: %v", err)
	}
	socket, _ := SocketPath(profile)
	if !bridgePaused(socket) {
		t.Fatal("rejected controller changed pause")
	}
	owner.Close()
	next, err := AcquireControl()
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if err := Pause(profile, false); err != nil {
		t.Fatal(err)
	}
	if bridgePaused(socket) {
		t.Fatal("next serialized operation failed to resume")
	}
}

func TestMaintenanceDoesNotWriteToReadOnlyProfileDirectory(t *testing.T) {
	directory := t.TempDir()
	profile := filepath.Join(directory, "profile.yaml")
	if err := os.WriteFile(profile, []byte("Gateway: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(directory, 0700)
	owner, err := AcquireControl()
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := Pause(profile, true); err != nil {
		t.Fatal(err)
	}
	if err := Pause(profile, false); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("control wrote beside profile: %v (%v)", entries, err)
	}
}

func TestReplacedWatcherYieldsBeforeReadingRecoveryState(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "profile.yaml")
	socket, err := SocketPath(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivateDir(filepath.Dir(socket)); err != nil {
		t.Fatal(err)
	}
	path := socket + ".recovery.json"
	data := []byte("state owned by a newer version")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
	replacement := filepath.Join(t.TempDir(), "digcrawl")
	if err := os.WriteFile(replacement, []byte("different program"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := Watch(ctx, replacement, profile, nil); err == nil || !strings.Contains(err.Error(), "executable replaced") {
		t.Fatalf("obsolete watcher did not yield: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(data) {
		t.Fatal("obsolete watcher changed another version's recovery state")
	}
}

func TestRecoveryCooldownAdmitsOnlyOneReservation(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "bridge.sock")
	r := recoveryRecord{Unclean: true, Attempts: 3, LastAttempt: time.Now().Add(-recoveryCooldown - time.Second)}
	if err := writeRecovery(socket, r); err != nil {
		t.Fatal(err)
	}
	if err := reserveBridgeStart(context.Background(), socket, false); err != nil {
		t.Fatal(err)
	}
	if err := reserveBridgeStart(context.Background(), socket, false); !errors.Is(err, ErrRecoveryExhausted) {
		t.Fatalf("second cooldown reservation: %v", err)
	}
	stored, err := readRecovery(socket)
	if err != nil || stored.Attempts != 3 {
		t.Fatalf("cooldown refunded budget: %+v %v", stored, err)
	}
}

func TestInspectAbsentProfileIsReadOnly(t *testing.T) {
	// Isolate the runtime root, not just the profile. Other packages may run
	// concurrently and Windows resolves UserConfigDir through APPDATA.
	root := bridgeSocketTempDir(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("APPDATA", root)
	profile := filepath.Join(root, "absent.yaml")
	socket, err := SocketPath(profile)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadDir(filepath.Dir(socket))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	status, err := Inspect(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	if status.Ready || status.Remaining != 3 {
		t.Fatalf("status: %+v", status)
	}
	after, err := os.ReadDir(filepath.Dir(socket))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(before) != 0 || len(after) != 0 || !os.IsNotExist(err) {
		t.Fatal("inspect created runtime state")
	}
}

func TestPauseBlocksAutomaticStartAndPreservesBudget(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "profile.yaml")
	if err := Pause(profile, true); err != nil {
		t.Fatal(err)
	}
	socket, _ := SocketPath(profile)
	if err := reserveBridgeStart(context.Background(), socket, false); !errors.Is(err, ErrBridgeStopping) {
		t.Fatalf("pause: %v", err)
	}
	if _, err := os.Stat(socket + ".recovery.json"); !os.IsNotExist(err) {
		t.Fatal("paused start consumed recovery")
	}
	if err := Pause(profile, false); err != nil {
		t.Fatal(err)
	}
	if err := reserveBridgeStart(context.Background(), socket, false); err != nil {
		t.Fatal(err)
	}
}

func TestFirstStartupFailureIsBoundedAndRetained(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "bridge.sock")
	if err := recordStartupFailure(socket, "listen", os.ErrPermission); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(socket + ".failure.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := recordStartupFailure(socket, "runtime", errors.New("secret signed URL")); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(socket + ".failure.json")
	if string(first) != string(second) || len(second) > 4096 {
		t.Fatal("first failure changed")
	}
}

func TestFirstStartMigratesLegacyRecoveryBeforeReservation(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "bridge.sock")
	legacy := filepath.Join(t.TempDir(), "bridge.sock")
	original := recoveryRecord{Unclean: true, Attempts: bridgeRecoveryLimit, LastAttempt: time.Now().UTC()}
	if err := writeRecovery(legacy, original); err != nil {
		t.Fatal(err)
	}
	if err := migrateRecoveryLocked(socket, legacy); err != nil {
		t.Fatal(err)
	}
	if err := reserveBridgeStart(context.Background(), socket, false); !errors.Is(err, ErrRecoveryExhausted) {
		t.Fatalf("first start bypassed cooldown: %v", err)
	}
	migrated, err := readRecovery(socket)
	if err != nil || migrated.Attempts != original.Attempts || !migrated.LastAttempt.Equal(original.LastAttempt) {
		t.Fatalf("migration changed history: %+v %v", migrated, err)
	}
	// A later legacy change cannot overwrite the already-published stable history.
	if err := writeRecovery(legacy, recoveryRecord{}); err != nil {
		t.Fatal(err)
	}
	if err := migrateRecoveryLocked(socket, legacy); err != nil {
		t.Fatal(err)
	}
	migrated, _ = readRecovery(socket)
	if migrated.Attempts != original.Attempts {
		t.Fatal("repeated migration refunded budget")
	}
}

func TestLegacyMigrationPreservesCorruptOrMissingState(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "bridge.sock")
	legacy := filepath.Join(t.TempDir(), "bridge.sock")
	if err := migrateRecoveryLocked(socket, legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(socket + ".recovery.json"); !os.IsNotExist(err) {
		t.Fatal("missing history created a record")
	}
	if err := os.WriteFile(legacy+".recovery.json", []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := migrateRecoveryLocked(socket, legacy); err == nil {
		t.Fatal("corrupt history was discarded")
	}
}

func TestLegacyDiagnosticsUsePredecessorBudgetWithoutMigrating(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "profile.yaml")
	socket, err := SocketPath(profile)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := legacySocketPathForSocket(socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivateDir(filepath.Dir(legacy)); err != nil {
		t.Fatal(err)
	}
	if err := writeRecovery(legacy, recoveryRecord{Unclean: true, Attempts: 3, LastAttempt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	status, err := Inspect(context.Background(), profile)
	if err != nil || status.Remaining != 0 || status.Reason != "recovery_cooldown" {
		t.Fatalf("legacy status=%+v err=%v", status, err)
	}
	if err := CheckRecovery(context.Background(), profile); !errors.Is(err, ErrRecoveryExhausted) {
		t.Fatalf("legacy doctor: %v", err)
	}
	if _, err := os.Stat(socket + ".recovery.json"); !os.IsNotExist(err) {
		t.Fatal("diagnostic migrated state")
	}
}
