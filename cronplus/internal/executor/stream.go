package executor

import (
	"fmt"
	"sync"
	"unicode/utf8"
)

// truncatingWriter captures process output without letting a chatty task
// exhaust memory. It keeps the first headLimit bytes and the last tailLimit
// bytes in a ring buffer, so a task that prints gigabytes still costs a fixed
// 256 KB instead of an unbounded read.
//
// The previous implementation used io.ReadAll, so `yes` (or any runaway loop)
// could allocate several GB before the timeout fired.
type truncatingWriter struct {
	mu        sync.Mutex
	head      []byte
	headLimit int
	ring      *ringBuffer
	total     int64
}

func newTruncatingWriter(headLimit, tailLimit int) *truncatingWriter {
	if headLimit < 0 {
		headLimit = 0
	}
	if tailLimit < 0 {
		tailLimit = 0
	}
	return &truncatingWriter{
		head:      make([]byte, 0, headLimit),
		headLimit: headLimit,
		ring:      newRingBuffer(tailLimit),
	}
}

// Write never fails and always reports the full length: the child process must
// keep running while we throw the excess away.
func (w *truncatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	written := len(p)
	w.total += int64(written)

	rest := p
	if room := w.headLimit - len(w.head); room > 0 {
		if len(rest) <= room {
			w.head = append(w.head, rest...)
			return written, nil
		}
		w.head = append(w.head, rest[:room]...)
		rest = rest[room:]
	}
	w.ring.Write(rest)
	// Report the full length, not the kept part: a short write makes io.Copy
	// stop reading, which leaves the child blocked on a full pipe forever.
	return written, nil
}

// Total returns the number of bytes the process wrote.
func (w *truncatingWriter) Total() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.total
}

// String renders the captured output, inserting a marker when bytes were
// dropped between the head and the tail.
func (w *truncatingWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	tail := w.ring.Bytes()
	if w.total <= int64(w.headLimit+w.ring.size) {
		return string(w.head) + string(tail)
	}

	head := trimRuneTail(w.head)
	tail = trimRuneHead(tail)
	omitted := w.total - int64(len(head)) - int64(len(tail))
	if omitted < 0 {
		omitted = 0
	}
	return string(head) + fmt.Sprintf(
		"\n[cronplus] ---- output truncated: %d bytes omitted (kept first %d and last %d of %d bytes) ----\n",
		omitted, len(head), len(tail), w.total) + string(tail)
}

// trimRuneTail drops the incomplete UTF-8 sequence at the end of b.
func trimRuneTail(b []byte) []byte {
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return b
}

// trimRuneHead advances past continuation bytes so the tail starts on a rune
// boundary.
func trimRuneHead(b []byte) []byte {
	for len(b) > 0 && !utf8.RuneStart(b[0]) {
		b = b[1:]
	}
	return b
}

// ringBuffer keeps the last size bytes written to it.
type ringBuffer struct {
	buf  []byte
	pos  int
	full bool
	size int
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{buf: make([]byte, size), size: size}
}

func (r *ringBuffer) Write(p []byte) {
	if r.size == 0 {
		return
	}
	if len(p) >= r.size {
		copy(r.buf, p[len(p)-r.size:])
		r.pos = 0
		r.full = true
		return
	}
	n := copy(r.buf[r.pos:], p)
	if n < len(p) {
		copy(r.buf, p[n:])
		r.pos = len(p) - n
		r.full = true
		return
	}
	r.pos += n
	if r.pos == r.size {
		r.pos = 0
		r.full = true
	}
}

// Bytes returns the buffered bytes in write order.
func (r *ringBuffer) Bytes() []byte {
	if r.size == 0 {
		return nil
	}
	if !r.full {
		out := make([]byte, r.pos)
		copy(out, r.buf[:r.pos])
		return out
	}
	out := make([]byte, 0, r.size)
	out = append(out, r.buf[r.pos:]...)
	out = append(out, r.buf[:r.pos]...)
	return out
}
