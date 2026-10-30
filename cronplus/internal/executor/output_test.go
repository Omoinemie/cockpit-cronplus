package executor

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateOutputKeepsShortOutput(t *testing.T) {
	in := "hello\nworld\n"
	if got := TruncateOutput(in); got != in {
		t.Fatalf("short output must be untouched, got %q", got)
	}
}

// The tail must survive: that is where the error message of a failing run is.
func TestTruncateOutputKeepsHeadAndTail(t *testing.T) {
	head := "HEAD-MARKER\n" + strings.Repeat("a", 100*1024)
	tail := strings.Repeat("b", 300*1024) + "\nTAIL-ERROR: permission denied\n"
	in := head + tail

	out := TruncateOutput(in)

	if !strings.HasPrefix(out, "HEAD-MARKER") {
		t.Fatal("head of the output is missing")
	}
	if !strings.HasSuffix(out, "TAIL-ERROR: permission denied\n") {
		t.Fatal("tail of the output is missing")
	}
	if !strings.Contains(out, "output truncated") {
		t.Fatal("truncation marker is missing")
	}
	if len(out) > MaxOutputBytes+1024 {
		t.Fatalf("truncated output too large: %d bytes", len(out))
	}
	if len(out) >= len(in) {
		t.Fatalf("output was not truncated: %d >= %d", len(out), len(in))
	}
}

func TestTruncateOutputDoesNotSplitRunes(t *testing.T) {
	in := strings.Repeat("日志输出测试", 100000) // 3 bytes per rune
	out := TruncateOutput(in)
	if !utf8.ValidString(out) {
		t.Fatal("truncation produced invalid UTF-8")
	}
}

func TestAppendResultSummary(t *testing.T) {
	got := AppendResultSummary("partial line", "error", 2, 1234)
	want := "partial line\n[cronplus] result: error (exit=2, 1.2s)\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	got = AppendResultSummary("", "success", 0, 5)
	want = "[cronplus] result: success (exit=0, 5ms)\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		ms   int
		want string
	}{
		{0, "0ms"},
		{5, "5ms"},
		{999, "999ms"},
		{1000, "1.0s"},
		{1234, "1.2s"},
		{4231, "4.2s"},
		{45000, "45s"},
		{67000, "1m 07s"},
		{3600000, "1h 00m"},
		{5400000 + 300000, "1h 35m"},
		{90000000, "1d 01h"},
		// A wall-clock timestamp used to be stored as a duration; it must not be
		// printed as "1793262922261ms".
		{1793262922261, "unknown"},
		{-1, "unknown"},
	}
	for _, c := range cases {
		if got := FormatDuration(c.ms); got != c.want {
			t.Errorf("FormatDuration(%d) = %q, want %q", c.ms, got, c.want)
		}
	}
}

// The WebUI mirrors this formatting in Utils.formatDuration; keep the two
// implementations in step by pinning the shapes the UI relies on.
func TestFormatDurationStaysShort(t *testing.T) {
	if got := FormatDuration(7200 * 1000); got != "2h 00m" {
		t.Fatalf("2h run rendered as %q", got)
	}
	if got := FormatDuration(59 * 1000); got != "59s" {
		t.Fatalf("59s run rendered as %q", got)
	}
}
