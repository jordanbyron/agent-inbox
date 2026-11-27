package watcher_test

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/snapshot"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/watcher"
)

func newLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, nil))
}

// process is a watcher helper process and the result of waiting for it.
type process struct {
	cmd    *exec.Cmd
	exited chan error
}

func startWatcher(t *testing.T, dir, version string) *process {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "WATCHER_TEST_HELPER=watch", "WATCHER_TEST_DIR="+dir, "WATCHER_TEST_VERSION="+version)
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Start())
	p := &process{cmd: cmd, exited: make(chan error, 1)}
	go func() { p.exited <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		p.exited <- <-p.exited
	})
	return p
}

func (p *process) waitExit(t *testing.T) error {
	t.Helper()
	select {
	case err := <-p.exited:
		p.exited <- err
		return err
	case <-time.After(waitFor):
		require.FailNow(t, "the watcher did not exit")
		return nil
	}
}

func (p *process) running() bool {
	select {
	case err := <-p.exited:
		p.exited <- err
		return false
	default:
		return true
	}
}

func waitVersion(t *testing.T, dir, version string) {
	t.Helper()
	store := state.New(dir)
	require.Eventually(t, func() bool {
		var snap snapshot.Snapshot
		_, err := store.ReadSnapshot(&snap)
		return err == nil && snap.WatcherVersion == version
	}, waitFor, 5*time.Millisecond, "no snapshot from version %s", version)
}

func TestSecondWatcherOfSameVersionExitsQuietly(t *testing.T) {
	dir := t.TempDir()
	first := startWatcher(t, dir, "v1")
	waitVersion(t, dir, "v1")

	second := startWatcher(t, dir, "v1")
	require.NoError(t, second.waitExit(t))
	require.True(t, first.running())

	_, err := state.New(dir).TakeWatcherLock("v1")
	held, ok := errors.AsType[*state.HeldError](err)
	require.True(t, ok)
	require.Equal(t, first.cmd.Process.Pid, held.PID)
}

func TestWatcherOfAnotherVersionTakesOver(t *testing.T) {
	dir := t.TempDir()
	old := startWatcher(t, dir, "v1")
	waitVersion(t, dir, "v1")

	startWatcher(t, dir, "v2")
	require.NoError(t, old.waitExit(t), "the old watcher exits cleanly on SIGTERM")
	waitVersion(t, dir, "v2")

	logged, err := os.ReadFile(watcher.LogPath(dir))
	require.NoError(t, err)
	require.Contains(t, string(logged), "asking the watcher of another version to exit")
}

func TestEnsureRunning(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WATCHER_TEST_HELPER", "watch")
	t.Setenv("WATCHER_TEST_DIR", dir)
	t.Setenv("WATCHER_TEST_VERSION", "v1")
	launch := watcher.Launch{StateDir: dir, Version: "v1", Command: []string{os.Args[0], "-test.run=^$"}}
	t.Cleanup(func() { stopDetached(t, dir) })

	require.NoError(t, watcher.EnsureRunning(t.Context(), launch))
	holder := lockHolder(t, dir)
	require.NotZero(t, holder.PID)
	require.Equal(t, "v1", holder.Version)

	require.NoError(t, watcher.EnsureRunning(t.Context(), launch))
	require.Equal(t, holder, lockHolder(t, dir), "a fresh snapshot of this version leaves the watcher be")

	stopDetached(t, dir)
	require.NoError(t, watcher.EnsureRunning(t.Context(), launch))
	again := lockHolder(t, dir)
	require.NotZero(t, again.PID)
	require.NotEqual(t, holder.PID, again.PID, "a snapshot left by a watcher that exited doesn't count")
}

func TestEnsureRunningSaysWhenTheWatcherWontStart(t *testing.T) {
	dir := t.TempDir()
	launch := watcher.Launch{StateDir: dir, Version: "v1", Command: []string{"/usr/bin/false"}}

	err := watcher.EnsureRunning(t.Context(), launch)
	require.ErrorContains(t, err, "the watcher did not start; see "+watcher.LogPath(dir))
}

func lockHolder(t *testing.T, dir string) state.HeldError {
	t.Helper()
	lock, err := state.New(dir).TakeWatcherLock("probe")
	if err == nil {
		require.NoError(t, lock.Close())
		return state.HeldError{}
	}
	held, ok := errors.AsType[*state.HeldError](err)
	require.True(t, ok, "%v", err)
	return *held
}

// stopDetached ends the watcher EnsureRunning started, which no test owns.
func stopDetached(t *testing.T, dir string) {
	t.Helper()
	holder := lockHolder(t, dir)
	if holder.PID == 0 {
		return
	}
	p, err := os.FindProcess(holder.PID)
	require.NoError(t, err)
	require.NoError(t, p.Signal(os.Interrupt))
	_, err = p.Wait()
	require.NoError(t, err)
}
