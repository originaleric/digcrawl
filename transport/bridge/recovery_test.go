package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecoveryBudgetPersistsAndExplicitStartDoesNotRefund(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "bridge.sock")
	for i := 0; i <= bridgeRecoveryLimit; i++ {
		if err := reserveBridgeStart(context.Background(), socket, false); err != nil {
			t.Fatal(err)
		}
		r, err := readRecovery(socket)
		if err != nil {
			t.Fatal(err)
		}
		r.LastAttempt = time.Now().Add(-time.Hour)
		if err = writeRecovery(socket, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := reserveBridgeStart(context.Background(), socket, false); !errors.Is(err, ErrRecoveryExhausted) {
		t.Fatalf("budget: %v", err)
	}
	if err := reserveBridgeStart(context.Background(), socket, true); err != nil {
		t.Fatal(err)
	}
	r, _ := readRecovery(socket)
	if r.Attempts != bridgeRecoveryLimit {
		t.Fatalf("manual start refunded budget: %+v", r)
	}
}
func TestRecoveryStabilityRejectsObserverGapsAndResetsAfterSustainedRPCs(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "bridge.sock")
	now := time.Now().UTC()
	r := recoveryRecord{Attempts: 4, HealthySince: now.Add(-time.Hour), LastHealthy: now.Add(-time.Hour)}
	if err := writeRecovery(socket, r); err != nil {
		t.Fatal(err)
	}
	if err := noteBridgeHealthy(socket, now); err != nil {
		t.Fatal(err)
	}
	r, _ = readRecovery(socket)
	if r.Attempts != 4 {
		t.Fatal("sleep/gap reset budget")
	}
	for elapsed := 30 * time.Second; elapsed <= bridgeRecoveryStable; elapsed += 30 * time.Second {
		if err := noteBridgeHealthy(socket, now.Add(elapsed)); err != nil {
			t.Fatal(err)
		}
	}
	r, _ = readRecovery(socket)
	if r.Attempts != 0 {
		t.Fatalf("stable serving did not reset: %+v", r)
	}
}
func TestRecoveryStopIntentsAreIndependentAndBlockStarting(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "bridge.sock")
	one, err := stopIntent(socket)
	if err != nil {
		t.Fatal(err)
	}
	defer one()
	two, err := stopIntent(socket)
	if err != nil {
		t.Fatal(err)
	}
	defer two()
	one()
	if !bridgeStopping(socket) {
		t.Fatal("one stop cleared another stop's intent")
	}
	if err := reserveBridgeStart(context.Background(), socket, true); !errors.Is(err, ErrBridgeStopping) {
		t.Fatalf("manual bypassed stop: %v", err)
	}
	two()
	if err := reserveBridgeStart(context.Background(), socket, false); err != nil {
		t.Fatal(err)
	}
}
func TestRecoveryCorruptionFailsClosedAndBackoffHonorsCancellation(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "bridge.sock")
	if err := os.WriteFile(socket+".recovery.json", []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := reserveBridgeStart(context.Background(), socket, true); err == nil {
		t.Fatal("corrupt metadata ignored")
	}
	if err := writeRecovery(socket, recoveryRecord{Attempts: 1, Unclean: true, LastAttempt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := reserveBridgeStart(ctx, socket, false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("backoff: %v", err)
	}
	r, _ := readRecovery(socket)
	if r.Attempts != 1 {
		t.Fatal("cancelled wait spent an attempt")
	}
}

func TestOldResponseCannotWriteAfterObservationOwnerCloses(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "bridge.sock")
	if err := writeRecovery(socket, recoveryRecord{Attempts: 4}); err != nil {
		t.Fatal(err)
	}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	handler, closeOwner := observeBridgeResponses(socket, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; w.WriteHeader(http.StatusOK) }))
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/manifest", nil))
		close(done)
	}()
	<-entered
	closeOwner()
	close(release)
	<-done
	r, err := readRecovery(socket)
	if err != nil {
		t.Fatal(err)
	}
	if !r.LastHealthy.IsZero() {
		t.Fatal("old handler wrote after lifecycle owner closed")
	}
}
func TestFailedRPCDoesNotCountAsHealthyRecoveryObservation(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "bridge.sock")
	if err := writeRecovery(socket, recoveryRecord{Attempts: 4}); err != nil {
		t.Fatal(err)
	}
	handler, finish := observeBridgeResponses(socket, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer finish()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/manifest", nil))
	r, err := readRecovery(socket)
	if err != nil {
		t.Fatal(err)
	}
	if !r.LastHealthy.IsZero() {
		t.Fatal("failed RPC reset stability observation")
	}
}

func TestPlannedStopsPreservePreviousFailureBudget(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "bridge.sock")
	if err := reserveBridgeStart(context.Background(), socket, false); err != nil {
		t.Fatal(err)
	}
	r, _ := readRecovery(socket)
	if r.Attempts != 0 || !r.Unclean {
		t.Fatalf("initial start: %+v", r)
	}
	r.LastAttempt = time.Now().Add(-time.Hour)
	if err := writeRecovery(socket, r); err != nil {
		t.Fatal(err)
	}
	if err := reserveBridgeStart(context.Background(), socket, false); err != nil {
		t.Fatal(err)
	}
	if err := markBridgeStopped(socket); err != nil {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 5; cycle++ {
		if err := reserveBridgeStart(context.Background(), socket, false); err != nil {
			t.Fatal(err)
		}
		if err := markBridgeStopped(socket); err != nil {
			t.Fatal(err)
		}
		r, _ = readRecovery(socket)
		if r.Attempts != 1 || r.Unclean {
			t.Fatalf("normal cycle changed history: %+v", r)
		}
	}
	if err := reserveBridgeStart(context.Background(), socket, true); err != nil {
		t.Fatal(err)
	}
	r, _ = readRecovery(socket)
	if r.Attempts != 1 || !r.Unclean {
		t.Fatalf("manual start changed history: %+v", r)
	}
}

func TestLegacyRecoveryRecordKeepsConservativeHistory(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "bridge.sock")
	if err := os.WriteFile(socket+".recovery.json", []byte(`{"version":1,"attempts":4}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := reserveBridgeStart(context.Background(), socket, false); !errors.Is(err, ErrRecoveryExhausted) {
		t.Fatalf("legacy budget reset: %v", err)
	}
	if err := reserveBridgeStart(context.Background(), socket, true); err != nil {
		t.Fatal(err)
	}
	r, err := readRecovery(socket)
	if err != nil || r.Version != 2 || r.Attempts != 4 || !r.Unclean {
		t.Fatalf("migration=%+v, err=%v", r, err)
	}
}

// v1 cleared its counter after sustained health. Later fault reservations
// cleared health timestamps too, so these snapshots cannot distinguish a
// first-launch count from actual failures since that stable reset.
func TestLegacyRecoveryAfterStableResetDoesNotRefund(t *testing.T) {
	for _, tc := range []struct {
		name     string
		attempts int
	}{
		{"one_failure", 1}, {"two_failures", 2}, {"exhausted", bridgeRecoveryLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			socket := filepath.Join(t.TempDir(), "bridge.sock")
			raw, err := json.Marshal(map[string]any{"version": 1, "attempts": tc.attempts, "last_attempt": time.Now().Add(-time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(socket+".recovery.json", raw, 0600); err != nil {
				t.Fatal(err)
			}
			migrated, err := readRecovery(socket)
			if err != nil || migrated.Version != 2 || migrated.Attempts != tc.attempts || !migrated.Unclean {
				t.Fatalf("read migration changed failure history: record=%+v err=%v", migrated, err)
			}
			unchanged, err := os.ReadFile(socket + ".recovery.json")
			if err != nil || string(unchanged) != string(raw) {
				t.Fatal("read migration modified the persisted legacy record")
			}
			err = reserveBridgeStart(context.Background(), socket, false)
			want := tc.attempts + 1
			if tc.attempts >= bridgeRecoveryLimit {
				if !errors.Is(err, ErrRecoveryExhausted) {
					t.Fatalf("admitted an extra recovery: %v", err)
				}
				unchanged, readErr := os.ReadFile(socket + ".recovery.json")
				if readErr != nil || string(unchanged) != string(raw) {
					t.Fatal("exhausted start changed persisted history")
				}
				if err = reserveBridgeStart(context.Background(), socket, true); err != nil {
					t.Fatal(err)
				}
				want = tc.attempts
			} else if err != nil {
				t.Fatal(err)
			}
			stored, err := os.ReadFile(socket + ".recovery.json")
			if err != nil {
				t.Fatal(err)
			}
			var persisted recoveryRecord
			if err = json.Unmarshal(stored, &persisted); err != nil {
				t.Fatal(err)
			}
			if persisted.Version != 2 || persisted.Attempts != want || !persisted.Unclean {
				t.Fatalf("persisted migration refunded/inflated budget: %+v want=%d", persisted, want)
			}
			again, err := readRecovery(socket)
			if err != nil || again.Attempts != want {
				t.Fatalf("v2 reread changed budget: %+v err=%v", again, err)
			}
		})
	}
}
