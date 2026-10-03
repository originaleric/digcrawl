package watchlist

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

// DIGCRAWL_TEST_MYSQL_DSN must point to a disposable database.
func TestMySQLFailedCollectRestart(t *testing.T) {
	dsn := os.Getenv("DIGCRAWL_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("DIGCRAWL_TEST_MYSQL_DSN is not set")
	}
	st, err := OpenStoreWithOptions(StoreOptions{Driver: "mysql", MySQLDSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := "recovery-" + uuid.NewString()
	platform := "test-" + uuid.NewString()
	now := time.Now().UTC().Add(-time.Hour)
	first, created, err := st.UpsertFailedCollect(ctx, FailedCollect{TaskID: id, ConfigHash: "h", Platform: platform, Stage: RecoveryDiscoverSource, ErrorCode: "LOGIN_REQUIRED"}, 24*time.Hour, true, now)
	if err != nil || !created {
		t.Fatalf("first=%+v created=%v err=%v", first, created, err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = OpenStoreWithOptions(StoreOptions{Driver: "mysql", MySQLDSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	defer st.DeleteFailedCollect(ctx, first.ID)
	if err := st.RecordLogin(ctx, platform, time.Now()); err != nil {
		t.Fatal(err)
	}
	ready, err := st.ListReadyFailedCollects(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range ready {
		if row.ID == first.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("persisted MySQL failure was not unlocked after reopen and login")
	}
}
