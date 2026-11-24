package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/runtime/version"
)

const recoveryCooldown = 5 * time.Minute

func legacySocketPathForSocket(socket string) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "digcrawl", bridgeSocketDirName, filepath.Base(socket)), nil
}

func bridgePaused(socket string) bool {
	_, err := os.Stat(socket + ".paused")
	return !errors.Is(err, os.ErrNotExist)
}

// HoldStop extends the existing stop intent across a profile upgrade. Each
// controller removes only its own marker after its complete mutation finishes.
func HoldStop(profile string) (func(), error) {
	socket, err := SocketPath(profile)
	if err != nil {
		return nil, err
	}
	if err := ensurePrivateDir(filepath.Dir(socket)); err != nil {
		return nil, err
	}
	return stopIntent(socket)
}

// AcquireControl serializes complete local maintenance operations. A second
// controller is rejected before it publishes intent or performs resource work.
// Reuse the existing resource lock in the private runtime directory.
func AcquireControl() (*browser.ProfileLock, error) {
	directory, err := browser.RuntimeDirectory()
	if err != nil {
		return nil, err
	}
	if err := ensurePrivateDir(directory); err != nil {
		return nil, err
	}
	guard, err := browser.AcquireInstallLock(directory)
	if err != nil {
		return nil, fmt.Errorf("another local maintenance operation is in progress; wait for it to finish: %w", err)
	}
	return guard, nil
}

// Pause is changed only by the controller holding AcquireControl. Background
// clients observe presence; no per-request generation or restart token exists.
func Pause(profile string, paused bool) error {
	socket, err := SocketPath(profile)
	if err != nil {
		return err
	}
	if err := ensurePrivateDir(filepath.Dir(socket)); err != nil {
		return err
	}
	if paused {
		return os.WriteFile(socket+".paused", []byte("operator stop\n"), 0600)
	}
	if err := os.Remove(socket + ".paused"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// migrateLocked runs under the stable lifecycle lock, before browser release.
func migrateLocked(ctx context.Context, profile, socket string, controlled bool, workingDirectory string) error {
	legacy, err := legacySocketPathForSocket(socket)
	if err != nil {
		return err
	}
	if legacy == socket {
		return nil
	}
	if err := ensurePrivateDir(filepath.Dir(legacy)); err != nil {
		return err
	}
	if _, err := stopSocket(ctx, profile, legacy, controlled, workingDirectory); err != nil {
		return err
	}
	return migrateRecoveryLocked(socket, legacy)
}

// Both lifecycle locks must be held before publishing legacy recovery history.
// A published stable record is authoritative across interrupted migrations.
func migrateRecoveryLocked(socket, legacy string) error {
	if socket == legacy {
		return nil
	}
	_, err := os.Stat(socket + ".recovery.json")
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	record, err := readRecovery(legacy)
	if err != nil {
		return err
	}
	if record.Version == 0 {
		return nil
	}
	return writeRecovery(socket, record)
}

// RecordStartupFailure persists safe diagnostics from the worker's config gate.
func RecordStartupFailure(profile, stage string, cause error) {
	socket, err := SocketPath(profile)
	if err != nil {
		return
	}
	if ensurePrivateDir(filepath.Dir(socket)) == nil {
		_ = recordStartupFailure(socket, stage, cause)
	}
}

type StartupFailure struct {
	Time   time.Time `json:"time"`
	Stage  string    `json:"stage"`
	Reason string    `json:"reason"`
}

func recordStartupFailure(socket, stage string, cause error) error {
	reason := "startup_failed"
	if stage == "config" {
		reason = "config_invalid"
	}
	if stage == "legacy_ownership" || stage == "ownership_cleanup" {
		reason = "owner_in_use"
	}
	switch {
	case errors.Is(cause, os.ErrPermission):
		reason = "permission_denied"
	case errors.Is(cause, ErrRecoveryExhausted):
		reason = "recovery_cooldown"
	case errors.Is(cause, ErrBridgeStopping):
		reason = "stopping"
	case errors.Is(cause, ErrBuildMismatch):
		reason = "build_mismatch"
	case errors.Is(cause, ErrBrowserProfileInUse), errors.Is(cause, ErrAlreadyRunning):
		reason = "owner_in_use"
	}
	// O_EXCL retains the first fault. Explicit successful restart clears it.
	f, err := os.OpenFile(socket+".failure.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(StartupFailure{Time: time.Now().UTC(), Stage: stage, Reason: reason})
}

type Status struct {
	Socket        string            `json:"socket"`
	Ready         bool              `json:"ready"`
	BuildID       string            `json:"build_id,omitempty"`
	Reason        string            `json:"reason"`
	Paused        bool              `json:"paused"`
	Stopping      bool              `json:"stopping"`
	Remaining     int               `json:"remaining_fast_recoveries"`
	CooldownUntil time.Time         `json:"cooldown_until,omitempty"`
	Owners        []OwnerDiagnostic `json:"owners,omitempty"`
	Failure       *StartupFailure   `json:"first_failure,omitempty"`
}

// Inspect has no filesystem writes and never starts a Bridge or browser.
func Inspect(ctx context.Context, profile string) (Status, error) {
	socket, err := SocketPath(profile)
	if err != nil {
		return Status{}, err
	}
	s := Status{Socket: socket, Reason: "socket_unavailable", Paused: bridgePaused(socket), Stopping: bridgeStopping(socket)}
	r, err := readRecoveryWithLegacy(socket)
	if err != nil {
		return s, err
	}
	s.Remaining = max(0, bridgeRecoveryLimit-r.Attempts)
	if r.Unclean && s.Remaining == 0 {
		s.CooldownUntil = r.LastAttempt.Add(recoveryCooldown)
	}
	if f, err := os.Open(socket + ".failure.json"); err == nil {
		var failure StartupFailure
		if json.NewDecoder(io.LimitReader(f, 4096)).Decode(&failure) == nil {
			s.Failure = &failure
		}
		_ = f.Close()
	}
	_, manifest, probeErr := probe(ctx, socket)
	if probeErr == nil {
		s.BuildID = manifest.BuildID
		if err := checkBuild(manifest); err != nil {
			s.Reason = "build_mismatch"
		} else {
			s.Ready = true
			s.Reason = "ready"
		}
	} else if errors.Is(probeErr, os.ErrPermission) {
		s.Reason = "permission_denied"
	}
	if s.Reason == "socket_unavailable" {
		if s.Remaining == 0 && time.Now().Before(s.CooldownUntil) {
			s.Reason = "recovery_cooldown"
		}
	}
	if s.Paused {
		s.Reason = "paused"
	}
	if s.Stopping {
		s.Reason = "stopping"
	}
	s.Owners, err = InspectOwners(ctx, profile)
	if !s.Ready && !s.Paused && !s.Stopping && s.Reason == "socket_unavailable" && len(s.Owners) > 0 {
		s.Reason = "owner_unreachable"
	}
	return s, err
}

// Watch uses real serving-path probes. Recovery never replays tool calls.
func Watch(ctx context.Context, executable, profile string, cfg *config.Config) error {
	socket, err := SocketPath(profile)
	if err != nil {
		return err
	}
	failures := 0
	var lastProbe time.Time
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		initialProbe := lastProbe.IsZero()
		matches, err := version.ExecutableMatches(executable)
		if err != nil {
			return fmt.Errorf("inspect installed watcher executable: %w", err)
		}
		if !matches {
			return fmt.Errorf("watcher executable replaced; restart with the installed program")
		}
		now := time.Now()
		if !lastProbe.IsZero() && now.Sub(lastProbe) > time.Minute {
			failures = 0
		}
		lastProbe = now
		if !bridgePaused(socket) && !bridgeStopping(socket) {
			checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			_, m, probeErr := probe(checkCtx, socket)
			cancel()
			if probeErr == nil {
				failures = 0
				if err := checkBuild(m); err != nil {
					log.Printf("Bridge watch: %v", err)
				}
			} else if errors.Is(probeErr, os.ErrPermission) {
				failures = 0
				log.Print("Bridge watch: permission_denied; inspect runtime directory permissions")
			} else {
				// A newly loaded watcher can cold-start an absent service just like
				// MCP. This owns no stop/replacement work and needs no maintenance
				// takeover; worker locks and pause/stop gates remain authoritative.
				if initialProbe {
					owners, inspectErr := InspectOwners(ctx, profile)
					if inspectErr == nil && len(owners) == 0 {
						start, finish := context.WithTimeout(ctx, 10*time.Second)
						_, startErr := NewExecutor(start, executable, profile, cfg)
						finish()
						if startErr == nil {
							failures = 0
							continue
						}
					}
				}
				failures++
				if failures >= 2 {
					recoverCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
					err := recoverBridge(recoverCtx, executable, profile, cfg)
					cancel()
					if err != nil {
						log.Printf("Bridge watch: %v", err)
					}
					failures = 0
				}
			}
		} else {
			failures = 0
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func recoverBridge(ctx context.Context, executable, profile string, cfg *config.Config) error {
	guard, err := AcquireControl()
	if err != nil {
		return err
	}
	defer guard.Close()

	socket, err := SocketPath(profile)
	if err != nil {
		return err
	}
	if bridgePaused(socket) || bridgeStopping(socket) {
		return ErrBridgeStopping
	}
	// Verified live but unreachable owners must release resources first. A ready
	// owner is always retained, including a mismatching build requiring upgrade.
	if _, m, err := probe(ctx, socket); err == nil {
		return checkBuild(m)
	}
	owners, err := InspectOwners(ctx, profile)
	if err != nil {
		return err
	}
	if len(owners) > 0 {
		expected, err := canonicalPath(executable)
		if err != nil {
			return err
		}
		for _, owner := range owners {
			actual, err := canonicalPath(owner.Executable)
			if err != nil || !owner.Verified || actual != expected {
				return fmt.Errorf("unverified Bridge owner requires inspection")
			}
		}
		if _, err := stopSocket(ctx, profile, socket, false, ""); err != nil {
			return err
		}
	}
	_, err = NewExecutor(ctx, executable, profile, cfg)
	return err
}
