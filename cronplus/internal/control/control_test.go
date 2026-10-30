package control

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"cronplus/internal/executor"
)

// The socket is the only way the WebUI and CLI can see what the daemon is
// running, so a round trip through it is worth a test: a wrong path, a wrong
// file mode or a missing newline would silently disable `cronplus ps`/`stop`.
func TestServeAndDoRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sock := SocketPath(filepath.Join(dir, "tasks.conf"))

	var got Request
	stop, err := Serve(sock, func(req Request) Response {
		got = req
		if req.Cmd == "ps" {
			return Response{OK: true, Running: []executor.RunningInfo{
				{TaskID: 7, PID: 4242, RunID: "#7-root-auto-0001", Trigger: "auto"},
			}}
		}
		return Response{OK: true, Stopped: true}
	})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	defer stop()

	fi, err := os.Stat(sock)
	if err != nil {
		t.Fatalf("socket missing: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0600 {
		t.Errorf("socket mode = %o, want 600", perm)
	}

	resp, err := Do(sock, Request{Cmd: "ps"})
	if err != nil {
		t.Fatalf("Do(ps): %v", err)
	}
	if !resp.OK || len(resp.Running) != 1 || resp.Running[0].TaskID != 7 || resp.Running[0].PID != 4242 {
		t.Errorf("unexpected ps response: %+v", resp)
	}
	if got.Cmd != "ps" {
		t.Errorf("handler saw cmd %q, want %q", got.Cmd, "ps")
	}

	resp, err = Do(sock, Request{Cmd: "stop", TaskID: 7})
	if err != nil {
		t.Fatalf("Do(stop): %v", err)
	}
	if !resp.OK || !resp.Stopped {
		t.Errorf("unexpected stop response: %+v", resp)
	}
	if got.TaskID != 7 {
		t.Errorf("handler saw task id %d, want 7", got.TaskID)
	}
}

// A socket file left behind by a crash must not stop the daemon from starting:
// closing a unix listener does not unlink its path, so this is the normal state
// after `kill -9`. Serve has to replace it instead of failing with
// "address already in use".
func TestServeReplacesStaleSocket(t *testing.T) {
	dir := t.TempDir()
	sock := SocketPath(filepath.Join(dir, "tasks.conf"))

	addr, err := net.ResolveUnixAddr("unix", sock)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	ln, err := net.ListenUnix("unix", addr)
	if err != nil {
		t.Fatalf("seed listener: %v", err)
	}
	// Keep the file on close, which is what a killed daemon leaves behind.
	ln.SetUnlinkOnClose(false)
	ln.Close()
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("expected a stale socket file: %v", err)
	}

	stop, err := Serve(sock, func(Request) Response { return Response{OK: true} })
	if err != nil {
		t.Fatalf("Serve over stale socket: %v", err)
	}
	if _, err := Do(sock, Request{Cmd: "ping"}); err != nil {
		t.Fatalf("Do after replacing stale socket: %v", err)
	}
	stop()

	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Errorf("socket file still present after stop: %v", err)
	}
}

// Do must report a usable error, not hang, when no daemon is listening.
func TestDoWithoutDaemon(t *testing.T) {
	sock := SocketPath(filepath.Join(t.TempDir(), "tasks.conf"))
	if _, err := Do(sock, Request{Cmd: "ping"}); err == nil {
		t.Fatal("expected an error when the socket does not exist")
	}
}
