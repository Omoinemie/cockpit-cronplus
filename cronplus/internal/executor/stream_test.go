package executor

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncatingWriterKeepsHeadAndTail(t *testing.T) {
	w := newTruncatingWriter(1024, 4096)

	head := strings.Repeat("H", 900)
	tail := strings.Repeat("T", 3000)
	// 100KB of filler between head and tail.
	w.Write([]byte(head))
	w.Write([]byte(strings.Repeat("x", 100*1024)))
	w.Write([]byte(tail))

	out := w.String()
	if !strings.HasPrefix(out, "HHHH") {
		t.Fatal("head lost")
	}
	if !strings.HasSuffix(out, "TTTT") {
		t.Fatal("tail lost")
	}
	if !strings.Contains(out, "output truncated") {
		t.Fatal("missing truncation marker")
	}
	if int64(len(out)) > 1024+4096+200 {
		t.Fatalf("output not bounded: %d bytes", len(out))
	}
	if w.Total() != int64(900+100*1024+3000) {
		t.Fatalf("wrong total: %d", w.Total())
	}
}

// A writer that reports a short write makes io.Copy stop reading, which leaves
// the child process blocked on a full pipe (this actually hung the test suite).
func TestTruncatingWriterReportsFullLength(t *testing.T) {
	w := newTruncatingWriter(8, 8)
	payload := []byte(strings.Repeat("a", 1000))
	n, err := w.Write(payload)
	if err != nil || n != len(payload) {
		t.Fatalf("Write = (%d, %v), want (%d, nil)", n, err, len(payload))
	}
}

func TestTruncatingWriterSmallOutputUnchanged(t *testing.T) {
	w := newTruncatingWriter(1024, 1024)
	msg := "hello\nworld\n"
	w.Write([]byte(msg))
	if got := w.String(); got != msg {
		t.Fatalf("small output changed: %q", got)
	}
}

func TestTruncatingWriterDoesNotSplitRunes(t *testing.T) {
	w := newTruncatingWriter(8, 8)
	for i := 0; i < 500; i++ {
		w.Write([]byte("日志输出")) // 4 runes × 3 bytes
	}
	if out := w.String(); !utf8.ValidString(out) {
		t.Fatal("truncation produced invalid UTF-8")
	}
}

func TestRingBufferKeepsLastBytes(t *testing.T) {
	r := newRingBuffer(4)
	r.Write([]byte("abcdef"))
	if got := string(r.Bytes()); got != "cdef" {
		t.Fatalf("ring = %q, want cdef", got)
	}
	r.Write([]byte("gh"))
	if got := string(r.Bytes()); got != "efgh" {
		t.Fatalf("ring = %q, want efgh", got)
	}
}
