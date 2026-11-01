package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const bridgeRecoveryLimit = 3
const bridgeRecoveryStable = 10 * time.Minute
const bridgeRecoveryBackoff = 2 * time.Second

var ErrRecoveryExhausted = errors.New("local Bridge recovery exhausted; correct the dependency and explicitly run bridge restart")
var ErrBridgeStopping = errors.New("local Bridge stop is in progress or incomplete; finish the owning stop before reconnecting")

// This bounded record is read/written only by the existing lifecycle-lock
// owner. It is not a daemon registry or a second lock/ownership domain.
type recoveryRecord struct {
	Version      int       `json:"version"`
	Unclean      bool      `json:"unclean"` // reserved start without a verified controlled exit
	Attempts     int       `json:"attempts"`
	LastAttempt  time.Time `json:"last_attempt"`
	HealthySince time.Time `json:"healthy_since,omitempty"`
	LastHealthy  time.Time `json:"last_healthy,omitempty"`
}

func readRecovery(socket string) (recoveryRecord, error) {
	var r recoveryRecord
	f, err := os.Open(socket + ".recovery.json")
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return r, err
	}
	if len(data) > 4096 || json.Unmarshal(data, &r) != nil || (r.Version != 1 && r.Version != 2) || r.Attempts < 0 || r.Attempts > 100000 {
		return r, errors.New("unsafe local Bridge recovery metadata; inspect locally before restarting")
	}
	if r.Version == 1 {
		// v1 could reset its counter after sustained health; later counts may
		// consist entirely of real failures. Its provenance and final exit are
		// unknown, so preserve the count as a conservative upper bound.
		r.Unclean = true
		r.Version = 2
	}
	return r, nil
}
func writeRecovery(socket string, r recoveryRecord) error {
	r.Version = 2
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(socket), ".bridge-recovery-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), socket+".recovery.json")
}
func bridgeStopping(socket string) bool {
	paths, err := filepath.Glob(socket + ".stop-*")
	return err != nil || len(paths) > 0
}

// reserveBridgeStart must run while the existing lifecycle lock is held, before
// opening listeners or starting owned work. A proxy crash cannot refund it.
func reserveBridgeStart(ctx context.Context, socket string, manual bool) error {
	if bridgeStopping(socket) {
		return ErrBridgeStopping
	}
	r, err := readRecovery(socket)
	if err != nil {
		return err
	}
	recovering := r.Unclean && !manual
	if recovering && r.Attempts >= bridgeRecoveryLimit {
		return ErrRecoveryExhausted
	}
	wait := time.Until(r.LastAttempt.Add(bridgeRecoveryBackoff))
	if wait > bridgeRecoveryBackoff {
		wait = bridgeRecoveryBackoff
	}
	if recovering && wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	if bridgeStopping(socket) {
		return ErrBridgeStopping
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if recovering {
		r.Attempts++
	}
	r.Unclean = true
	r.LastAttempt = time.Now().UTC()
	r.HealthySince = time.Time{}
	r.LastHealthy = time.Time{}
	return writeRecovery(socket, r)
}

// Observe actual successful RPC responses. No independent heartbeat can reset
// the budget while the serving path is wedged. Observer gaps/sleep cost no
// recovery attempts and do not count as continuous healthy observations.
func noteBridgeHealthy(socket string, now time.Time) error {
	r, err := readRecovery(socket)
	if err != nil {
		return err
	}
	if r.HealthySince.IsZero() || now.Sub(r.LastHealthy) > time.Minute || now.Before(r.LastHealthy) {
		r.HealthySince = now
	}
	r.LastHealthy = now
	if now.Sub(r.HealthySince) >= bridgeRecoveryStable {
		r.Attempts = 0
		r.LastAttempt = time.Time{}
	}
	return writeRecovery(socket, r)
}
func stopIntent(socket string) (func(), error) {
	// Each controller owns its own intent. Concurrent stops cannot remove each
	// other's marker. A killed controller leaves a conservative visible blocker.
	f, err := os.CreateTemp(filepath.Dir(socket), filepath.Base(socket)+".stop-*")
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	return func() { _ = os.Remove(f.Name()) }, nil
}
func bridgeRecoveryDetails(err error) map[string]any {
	reason := "unavailable"
	action := "restart_bridge"
	if errors.Is(err, ErrRecoveryExhausted) {
		reason = "recovery_exhausted"
	}
	if errors.Is(err, ErrBridgeStopping) {
		reason = "stopping"
		action = "finish_stop"
	}
	if errors.Is(err, ErrBuildMismatch) {
		reason = "build_mismatch"
	}
	return map[string]any{"handled": true, "retryable": false, "reason": reason, "stage": "connect", "execution_state": "not_dispatched", "recovery_limit": bridgeRecoveryLimit, "next_actions": []map[string]any{{"type": action, "executor": "user_or_agent", "precondition": "inspect doctor; finish prior cleanup; use the matching installed binary"}}}
}
func recoveryError(err error) error { return fmt.Errorf("local Bridge recovery unavailable: %w", err) }

// CheckRecovery is a read-only diagnostic. Atomic publication lets doctor
// inspect the record without claiming the running Bridge's lifecycle lock.
func CheckRecovery(ctx context.Context, profile string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	socket, err := SocketPath(profile)
	if err != nil {
		return err
	}
	if bridgeStopping(socket) {
		return ErrBridgeStopping
	}
	r, err := readRecovery(socket)
	if err != nil {
		return err
	}
	if r.Unclean && r.Attempts >= bridgeRecoveryLimit {
		if _, _, err := probe(ctx, socket); err != nil {
			return ErrRecoveryExhausted
		}
	}
	return nil
}

// Only the lifecycle-lock owner may acknowledge a verified controlled shutdown.
// Normal exits do not erase previous failures or replenish recovery allowance.
func markBridgeStopped(socket string) error {
	r, err := readRecovery(socket)
	if err != nil {
		return err
	}
	r.Unclean = false
	return writeRecovery(socket, r)
}
