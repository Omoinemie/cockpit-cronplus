package executor

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"cronplus/pkg/model"
)

// RunCommand executes a task's command once and returns the outcome.
//
// Output is captured into a bounded buffer for the log and, when tee is not
// nil, also written to it as it arrives (the CLI uses os.Stdout so a manual run
// streams live). onStart is called right after the process starts.
//
// Both the daemon and the CLI call this, so a manual run and a scheduled run
// behave identically: same timeout, same sanitisation, same logging format.
func RunCommand(ctx context.Context, task model.Task, command string, stdin io.Reader, tee io.Writer, startHook func(*exec.Cmd)) (status string, outputStr string, durationMs int, exitCode int) {

	if ctx.Err() != nil {
		return "error", AppendResultSummary("[cronplus] stopped by user\n", "error", -1, 0), 0, -1
	}

	var cmd *exec.Cmd
	if task.RunUser != "" && task.RunUser != "root" {
		cmd = exec.Command("su", "-", task.RunUser, "-c", command)
	} else {
		cmd = exec.Command("bash", "-c", command)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if task.Cwd != "" {
		cmd.Dir = ExpandPathForUser(task.Cwd, task.RunUser)
	}
	env, rejectedEnv := BuildEnv(task.EnvVars)
	cmd.Env = env
	if stdin != nil {
		cmd.Stdin = stdin
	}

	// Merge stdout and stderr into a single stream.
	//
	// A single os.Pipe is required: cmd.StdoutPipe() hands back the read end
	// (an io.ReadCloser), so it cannot be assigned to cmd.Stderr, which wants
	// an io.Writer. Leaving cmd.Stderr nil makes os/exec send it to /dev/null,
	// which is how every error message a task writes used to disappear from
	// the log — the manual run path merges them, so the two paths disagreed.
	pr, pw, err := os.Pipe()
	if err != nil {
		status = "error"
		exitCode = -1
		outputStr = fmt.Sprintf("[cronplus] pipe error: %v\n", err)
		outputStr = AppendResultSummary(outputStr, status, exitCode, durationMs)
		return
	}
	defer pr.Close()
	cmd.Stdout = pw
	cmd.Stderr = pw

	startTime := time.Now()
	if err := cmd.Start(); err != nil {
		pw.Close()
		status = "error"
		exitCode = -1
		durationMs = int(time.Since(startTime).Milliseconds())
		outputStr = AppendResultSummary(fmt.Sprintf("[cronplus] start error: %v\n", err), status, exitCode, durationMs)
		return
	}
	if startHook != nil {
		startHook(cmd)
	}

	// The parent must drop its copy of the write end, otherwise the reader
	// below never sees EOF and blocks until the timeout fires.
	pw.Close()

	pgid := cmd.Process.Pid

	// Output is captured through a bounded writer: a task that prints without
	// end must not be able to allocate gigabytes before its timeout fires.
	tw := newTruncatingWriter(HeadOutputBytes, TailOutputBytes)
	readDone := make(chan struct{}, 1)
	go func() {
		if tee != nil {
			_, _ = io.Copy(io.MultiWriter(tw, tee), pr)
		} else {
			_, _ = io.Copy(tw, pr)
		}
		readDone <- struct{}{}
	}()

	stopReason := ""

	waitReader := func(d time.Duration) {
		select {
		case <-readDone:
		case <-time.After(d):
		}
	}

	killGroup := func() {
		syscall.Kill(-pgid, syscall.SIGTERM)
		time.Sleep(500 * time.Millisecond)
		syscall.Kill(-pgid, syscall.SIGKILL)
	}

	if task.Timeout > 0 {
		timer := time.NewTimer(time.Duration(task.Timeout) * time.Second)
		select {
		case <-readDone:
			timer.Stop()
		case <-timer.C:
			stopReason = "timeout"
			killGroup()
			waitReader(3 * time.Second)
		case <-ctx.Done():
			stopReason = "cancelled"
			killGroup()
			waitReader(3 * time.Second)
		}
	} else {
		select {
		case <-readDone:
		case <-ctx.Done():
			stopReason = "cancelled"
			killGroup()
			waitReader(3 * time.Second)
		}
	}

	// Wait for process to exit
	cmd.Wait()
	durationMs = int(time.Since(startTime).Milliseconds())

	exitCode = -1
	status = "error"
	if cmd.ProcessState != nil && cmd.ProcessState.Success() {
		exitCode = 0
		status = "success"
	} else if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	outputStr = tw.String()

	if len(rejectedEnv) > 0 {
		log.Printf("[security] Blocked dangerous env vars for task [%d]: %v", task.ID, rejectedEnv)
		outputStr = fmt.Sprintf("[cronplus] blocked env vars (not passed to the task): %s\n",
			strings.Join(rejectedEnv, ", ")) + outputStr
	}

	switch stopReason {
	case "timeout":
		outputStr += fmt.Sprintf("\n[cronplus] command timeout (%ds) terminated\n", task.Timeout)
		status = "error"
		exitCode = -1
	case "cancelled":
		outputStr += "\n[cronplus] stopped by user\n"
		status = "error"
		exitCode = -1
	}

	// Added last, so the outcome survives truncation.
	outputStr = AppendResultSummary(outputStr, status, exitCode, durationMs)

	return
}
