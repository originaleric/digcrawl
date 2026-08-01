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

func TestTaskRuntimeDailyScheduleUsesConfiguredLocalTime(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.UTC
	defer func() { time.Local = oldLocal }()

	st, err := OpenStore(t.TempDir() + "/watchlist.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	item := WatchItem{ID: "daily-source", Interval: 24 * time.Hour, ScheduleMode: "daily", ScheduleAt: "09:00", Enabled: true}
	def := TaskDefinitionForItem(item)
	ctx := context.Background()
	now := time.Date(2026, 7, 16, 8, 30, 0, 0, time.UTC)
	if err := st.UpsertTaskDefinition(ctx, def, now); err != nil {
		t.Fatal(err)
	}
	due, err := st.IsTaskDue(ctx, def.ID, time.Date(2026, 7, 16, 8, 59, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if due {
		t.Fatal("daily task should not be due before configured time")
	}
	due, err = st.IsTaskDue(ctx, def.ID, time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !due {
		t.Fatal("daily task should be due at configured time")
	}
	runID, err := st.StartTaskRun(ctx, def, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishTaskRun(ctx, runID, def, "success", "", `{"ok":true}`, time.Date(2026, 7, 16, 9, 5, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	due, err = st.IsTaskDue(ctx, def.ID, time.Date(2026, 7, 17, 8, 59, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if due {
		t.Fatal("daily task should wait until next configured time after finishing")
	}
	due, err = st.IsTaskDue(ctx, def.ID, time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !due {
		t.Fatal("daily task should be due on the next day at configured time")
	}
}

func TestWatchSourcePersistsDailySchedule(t *testing.T) {
	st, err := OpenStore(t.TempDir() + "/watchlist.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	src := WatchSource{
		ID:           "toutiao-wtt",
		Name:         "头条微头条",
		Platform:     "toutiao",
		Type:         "toutiao_author",
		URL:          "https://www.toutiao.com/c/user/token/example/?tab=wtt",
		Enabled:      true,
		ScheduleMode: "daily",
		Interval:     24 * time.Hour,
		ScheduleAt:   "09:00",
		NotifyTo:     "feishu-default",
		Options:      map[string]any{"limit": 2},
	}
	if err := st.UpsertSource(ctx, src); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSource(ctx, src.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ScheduleMode != "daily" || got.ScheduleAt != "09:00" || got.Interval != 24*time.Hour {
		t.Fatalf("source schedule mismatch: %+v", got)
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
