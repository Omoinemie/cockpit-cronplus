// Package control implements the local control channel between the CLI/WebUI
// and the running daemon: a Unix socket in the config directory (0700, root
// only) carrying one JSON request/response per connection.
//
// Before this existed the only way to learn what the daemon was running, or to
// stop a scheduled task, was to send it a signal or restart the whole service
// (which kills every task).
package control

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"cronplus/internal/executor"
)

// SocketName is the socket file name inside the config directory.
const SocketName = "cronplus.sock"

// Request is one control command.
type Request struct {
	Cmd    string `json:"cmd"`               // ping | ps | stop
	TaskID int    `json:"task_id,omitempty"` // for stop
}

// Response is the daemon's answer.
type Response struct {
	OK      bool                   `json:"ok"`
	Error   string                 `json:"error,omitempty"`
	Version string                 `json:"version,omitempty"`
	PID     int                    `json:"pid,omitempty"` // daemon pid, from ping
	Running []executor.RunningInfo `json:"running,omitempty"`
	Stopped bool                   `json:"stopped,omitempty"`
}

// SocketPath returns the socket path next to the config file.
func SocketPath(confPath string) string {
	return filepath.Join(filepath.Dir(confPath), SocketName)
}

// Do sends one request and returns the response.
func Do(path string, req Request) (Response, error) {
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	payload, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		return Response{}, err
	}

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return Response{}, err
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return Response{}, fmt.Errorf("bad response: %w", err)
	}
	return resp, nil
}

// Serve listens on the socket until stop is called. Requests are handled by
// handler; a panic in one handler does not take the daemon down.
func Serve(path string, handler func(Request) Response) (stop func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	// A socket left behind by a crash or a `kill -9` would make Listen fail.
	if fi, statErr := os.Stat(path); statErr == nil && fi.Mode()&os.ModeSocket != 0 {
		os.Remove(path)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		ln.Close()
		return nil, err
	}

	done := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-done:
					return
				default:
					return
				}
			}
			go handleConn(conn, handler)
		}
	}()

	return func() {
		close(done)
		ln.Close()
		os.Remove(path)
	}, nil
}

func handleConn(conn net.Conn, handler func(Request) Response) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: "bad request"})
		return
	}
	writeResponse(conn, safeHandle(handler, req))
}

func safeHandle(handler func(Request) Response, req Request) (resp Response) {
	defer func() {
		if r := recover(); r != nil {
			resp = Response{OK: false, Error: fmt.Sprintf("internal error: %v", r)}
		}
	}()
	return handler(req)
}

func writeResponse(conn net.Conn, resp Response) {
	out, err := json.Marshal(resp)
	if err != nil {
		return
	}
	conn.Write(append(out, '\n'))
}
