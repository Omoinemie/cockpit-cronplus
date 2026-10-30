package executor

import (
	"context"
	"strings"
	"testing"

	"cronplus/pkg/model"
)

func newTestPool(t *testing.T) *Pool {
	t.Helper()
	return NewPool(nil, 4)
}

// A long-running chatty command must keep its tail: the whole point of the
// truncation fix is that the end of the output (where the failure reason is)
// survives.
func TestRunOnceKeepsTailOfHugeOutput(t *testing.T) {
	p := newTestPool(t)
	task := model.Task{ID: 1, Command: "", RunUser: "root"}

	status, output, duration, exitCode := p.runOnce(context.Background(), task, "seq 1 200000", 1, "auto")

	if status != "success" || exitCode != 0 {
		t.Fatalf("expected success, got status=%q exit=%d", status, exitCode)
	}
	if len(output) > MaxOutputBytes+2048 {
		t.Fatalf("output not bounded: %d bytes", len(output))
	}
	if !strings.Contains(output, "output truncated") {
		t.Fatal("missing truncation marker")
	}
	if !strings.HasSuffix(output, "[cronplus] result: success (exit=0, "+FormatDuration(duration)+")\n") {
		t.Fatalf("missing result summary at the end: %q", lastLine(output))
	}
	if !strings.Contains(output, "200000") {
		t.Fatal("tail of the output was dropped")
	}
}

func TestRunOnceReportsFailureAndStderr(t *testing.T) {
	p := newTestPool(t)
	task := model.Task{ID: 2, RunUser: "root"}

	status, output, duration, exitCode := p.runOnce(context.Background(), task, "echo boom >&2; exit 3", 1, "auto")

	if status != "error" || exitCode != 3 {
		t.Fatalf("expected error/3, got status=%q exit=%d", status, exitCode)
	}
	if !strings.Contains(output, "boom") {
		t.Fatalf("stderr missing from output: %q", output)
	}
	if !strings.HasSuffix(output, "[cronplus] result: error (exit=3, "+FormatDuration(duration)+")\n") {
		t.Fatalf("missing result summary at the end: %q", lastLine(output))
	}
}

func lastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}
