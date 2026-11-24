package bridge

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func TestLegacyOwnerAcrossTimezoneAndReusedPID(t *testing.T) {
	ctx := context.Background()
	command := exec.Command("ps", "-p", strconv.Itoa(os.Getpid()), "-o", "lstart=,command=")
	command.Env = append(os.Environ(), "TZ=UTC", "LC_ALL=C")
	data, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	owner := bridgeOwner{pid: os.Getpid(), identity: fmt.Sprintf("%x", sha256.Sum256(data)), publishedAt: time.Now()}
	t.Setenv("TZ", "America/New_York")
	t.Setenv("LC_ALL", "C")
	if matches, err := ownerIdentityMatches(ctx, owner); err != nil || !matches {
		t.Fatalf("cross-TZ legacy owner: %v (%v)", matches, err)
	}
	// Simulate a PID recycled after the old record was published. A different
	// legacy hash cannot authorize a process newer than the recorded resource.
	owner.publishedAt = time.Unix(1, 0)
	if matches, err := ownerIdentityMatches(ctx, owner); err != nil || matches {
		t.Fatalf("reused legacy PID accepted: %v (%v)", matches, err)
	}
	t.Setenv("TZ", "UTC")
	if matches, err := ownerIdentityMatches(ctx, owner); err != nil || matches {
		t.Fatalf("matching display hash bypassed creation bound: %v (%v)", matches, err)
	}
}
