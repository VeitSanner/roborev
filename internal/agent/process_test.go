package agent

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"
)

type countingCloser struct {
	closed atomic.Int32
}

func (c *countingCloser) Close() error {
	c.closed.Add(1)
	return nil
}

func TestCloseOnContextDoneClosesOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	closer := &countingCloser{}
	stop := closeOnContextDone(ctx, closer)
	defer stop()

	cancel()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if closer.closed.Load() == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("expected closer to be closed after context cancellation")
}

func TestCloseOnContextDoneStopPreventsClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	closer := &countingCloser{}
	stop := closeOnContextDone(ctx, closer)
	stop()
	cancel()
	time.Sleep(20 * time.Millisecond)

	if got := closer.closed.Load(); got != 0 {
		t.Fatalf("closer should not be closed after stop(), got %d", got)
	}
}

func TestCloseOnContextDoneBackgroundIsNoop(t *testing.T) {
	closer := &countingCloser{}
	stop := closeOnContextDone(context.Background(), closer)
	stop()

	if got := closer.closed.Load(); got != 0 {
		t.Fatalf("background context should not close the closer, got %d", got)
	}
}

func TestContextProcessError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tracker := &subprocessTracker{}
	tracker.canceledByContext.Store(true)

	if got := contextProcessError(ctx, tracker, errors.New("agent failed"), nil); got != nil {
		t.Fatalf("real subprocess error should be preserved, got context error %v", got)
	}
	if got := contextProcessError(ctx, tracker, exec.ErrWaitDelay, nil); !errors.Is(got, context.Canceled) {
		t.Fatalf("expected context cancellation for wait delay, got %v", got)
	}
	if got := contextProcessError(ctx, tracker, nil, fs.ErrClosed); !errors.Is(got, context.Canceled) {
		t.Fatalf("expected context cancellation for closed pipe parse error, got %v", got)
	}
	if got := contextProcessError(ctx, tracker, errors.New("agent failed"), fs.ErrClosed); got != nil {
		t.Fatalf("real subprocess error should not be masked by closed pipe parse error, got %v", got)
	}
}

func TestContextProcessErrorRunPathCancellation(t *testing.T) {
	skipIfWindows(t)

	cmdPath := writeTempCommand(t, "#!/bin/sh\nsleep 5\n")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, cmdPath)
	tracker := configureSubprocess(cmd)

	err := cmd.Run()
	if err == nil {
		t.Fatal("expected command cancellation")
	}
	if got := contextProcessError(ctx, tracker, err, nil); !errors.Is(got, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v (run err: %v)", got, err)
	}
}

func TestContextProcessErrorDoesNotMaskSignalExitAfterContextDone(t *testing.T) {
	skipIfWindows(t)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "sh", "-c", "kill -KILL $$")
	tracker := configureSubprocess(cmd)

	err := cmd.Run()
	if err == nil {
		t.Fatal("expected signal exit")
	}
	cancel()

	if got := contextProcessError(ctx, tracker, err, nil); got != nil {
		t.Fatalf("signal exit should not be rewritten as context error, got %v (run err: %v)", got, err)
	}
}

func TestConfigureSubprocessDoesNotMarkCanceledWhenProcessAlreadyExited(t *testing.T) {
	skipIfWindows(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", "exit 0")
	tracker := configureSubprocess(cmd)

	if err := cmd.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cmd.Cancel == nil {
		t.Fatal("expected wrapped cancel")
	}
	if err := cmd.Cancel(); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("expected os.ErrProcessDone, got %v", err)
	}
	if tracker.canceledByContext.Load() {
		t.Fatal("tracker should stay false when cancel runs after process exit")
	}
}
