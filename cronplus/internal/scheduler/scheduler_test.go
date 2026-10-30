package scheduler

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cronplus/internal/store"
	"cronplus/pkg/model"
)

type mockPool struct {
	dispatched []int
	killed     []int
}

func (m *mockPool) Dispatch(task model.Task, trigger string) {
	m.dispatched = append(m.dispatched, task.ID)
}
func (m *mockPool) Kill(taskID int) bool { m.killed = append(m.killed, taskID); return true }
func (m *mockPool) KillAll()             {}
func (m *mockPool) IsRunning(int) bool   { return false }

// `*/0` produced an empty value set, which the matcher treated as "*", so the
// task fired every second. It must now be rejected at compile time.
func TestCompileRejectsInvalidFields(t *testing.T) {
	for _, spec := range []string{"*/0 * * * *", "0 0 99 * * *", "? * * * *", "* * * *"} {
		if _, err := Compile(spec); err == nil {
			t.Errorf("Compile(%q) = nil error, want failure", spec)
		}
	}
	for _, spec := range []string{"*/5 * * * *", "0 3 * * *", "@daily"} {
		if _, err := Compile(spec); err != nil {
			t.Errorf("Compile(%q) = %v, want success", spec, err)
		}
	}
}

// An anchored step schedule must still respect the other fields: 9-17 hours
// means 18:05 is not a valid run time.
func TestAnchorRespectsOtherFields(t *testing.T) {
	c, err := Compile("0 */5 9-17 * * *")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	after := time.Date(2026, 5, 1, 18, 30, 0, 0, time.Local)
	nxt := c.NextFromAnchor(after, "2026-05-01 08:00:00")
	want := time.Date(2026, 5, 2, 9, 0, 0, 0, time.Local)
	if !nxt.Equal(want) {
		t.Fatalf("next = %s, want %s", nxt, want)
	}
}

// Every @reboot task must fire, not only the first one in the config.
func TestAllRebootTasksFire(t *testing.T) {
	dir := t.TempDir()
	s := store.New(filepath.Join(dir, "tasks.conf"), filepath.Join(dir, "logs"))
	pool := &mockPool{}
	sched := New(s, pool)
	sched.running.Store(true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, id := range []int{1, 2, 3} {
		sched.fireRebootTask(ctx, model.Task{ID: id})
	}

	if len(pool.dispatched) != 3 {
		t.Fatalf("dispatched %v, want all three @reboot tasks", pool.dispatched)
	}
}

func TestRebootDelayIsHonoured(t *testing.T) {
	dir := t.TempDir()
	s := store.New(filepath.Join(dir, "tasks.conf"), filepath.Join(dir, "logs"))
	pool := &mockPool{}
	sched := New(s, pool)
	sched.running.Store(true)

	ctx, cancel := context.WithCancel(context.Background())
	sched.fireRebootTask(ctx, model.Task{ID: 7, RebootDelay: 30})
	if len(pool.dispatched) != 0 {
		t.Fatal("delayed @reboot task fired immediately")
	}
	cancel() // cancelling must drop the pending dispatch
	time.Sleep(20 * time.Millisecond)
	if len(pool.dispatched) != 0 {
		t.Fatal("cancelled @reboot task still fired")
	}
}

// `a/b` means "from a, every b" in Vixie cron, not "only a". Treating it as a
// single value made `5/10 * * * * *` fire at second 5 of every minute instead
// of every 10 seconds — and rejecting it outright (an earlier attempt at this
// fix) would have silently stopped tasks that were working before.
func TestStartStepExpansion(t *testing.T) {
	c, err := Compile("5/10 * * * * *")
	if err != nil {
		t.Fatalf("Compile(5/10): %v", err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var got []int
	cur := base
	for i := 0; i < 4; i++ {
		cur = c.Next(cur)
		if cur.IsZero() {
			t.Fatalf("no match #%d", i+1)
		}
		got = append(got, cur.Second())
	}
	want := []int{5, 15, 25, 35}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("seconds = %v, want %v", got, want)
		}
	}
}

// A bare numeric field still means exactly that value, not a step from it.
func TestSingleValueStillExact(t *testing.T) {
	c, err := Compile("7 * * * * *")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	nxt := c.Next(base)
	if nxt.Second() != 7 || nxt.Minute() != 0 {
		t.Fatalf("next = %s, want second 7 of the same minute", nxt)
	}
}
