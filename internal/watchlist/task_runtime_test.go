package watchlist

import (
	"context"
	"testing"
	"time"
)

func TestTaskRuntimeDueUsesGenericTaskTables(t *testing.T) {
	st, err := OpenStore(t.TempDir() + "/watchlist.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	item := WatchItem{ID: "source-1", Interval: 6 * time.Hour, Enabled: true}
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	def := TaskDefinitionForItem(item)
	ctx := context.Background()
	if err := st.UpsertTaskDefinition(ctx, def, now); err != nil {
		t.Fatal(err)
	}
	due, err := st.IsTaskDue(ctx, def.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if !due {
		t.Fatal("new task should be due at initial next_run_at")
	}
	runID, err := st.StartTaskRun(ctx, def, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishTaskRun(ctx, runID, def, "success", "", `{"ok":true}`, now); err != nil {
		t.Fatal(err)
	}
	due, err = st.IsTaskDue(ctx, def.ID, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if due {
		t.Fatal("item should not be due before its interval has elapsed")
	}
	due, err = st.IsTaskDue(ctx, def.ID, now.Add(6*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !due {
		t.Fatal("item should be due once its interval has elapsed")
	}
	changed := def
	changed.ScheduleEvery = time.Hour
	changed.ConfigHash = "changed"
	if err := st.UpsertTaskDefinition(ctx, changed, now.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	due, err = st.IsTaskDue(ctx, def.ID, now.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !due {
		t.Fatal("config changes should reset next_run_at so the new schedule can take effect")
	}
}

func TestTaskRuntimeSyncsDisabledTasks(t *testing.T) {
	st, err := OpenStore(t.TempDir() + "/watchlist.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	disabled := WatchItem{ID: "disabled", Interval: time.Hour, Enabled: false, Platform: "rss", Type: "rss", URL: "https://example.com/feed.xml"}
	def := TaskDefinitionForItem(disabled)
	ctx := context.Background()
	if err := st.UpsertTaskDefinition(ctx, def, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var enabled int
	if err := st.db.QueryRowContext(ctx, `SELECT enabled FROM dc_tasks_runtime WHERE task_id = ?`, def.ID).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 {
		t.Fatalf("disabled task should be synced as enabled=0, got %d", enabled)
	}
}

func TestTaskRuntimeDisablesMissingDefinitions(t *testing.T) {
	st, err := OpenStore(t.TempDir() + "/watchlist.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	active := TaskDefinitionForItem(WatchItem{ID: "active", Interval: time.Hour, Enabled: true})
	removed := TaskDefinitionForItem(WatchItem{ID: "removed", Interval: time.Hour, Enabled: true})
	if err := st.UpsertTaskDefinition(ctx, active, now); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertTaskDefinition(ctx, removed, now); err != nil {
		t.Fatal(err)
	}
	if err := st.DisableMissingTaskDefinitions(ctx, "watchlist.collect", []string{active.ID}, now); err != nil {
		t.Fatal(err)
	}
	var activeEnabled, removedEnabled int
	if err := st.db.QueryRowContext(ctx, `SELECT enabled FROM dc_tasks_runtime WHERE task_id = ?`, active.ID).Scan(&activeEnabled); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(ctx, `SELECT enabled FROM dc_tasks_runtime WHERE task_id = ?`, removed.ID).Scan(&removedEnabled); err != nil {
		t.Fatal(err)
	}
	if activeEnabled != 1 || removedEnabled != 0 {
		t.Fatalf("unexpected enabled flags: active=%d removed=%d", activeEnabled, removedEnabled)
	}
}
