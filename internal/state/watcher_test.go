package state_test

import (
	"fmt"
	"io/fs"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/state"
)

func TestWatcherLockRefusesSecondTaker(t *testing.T) {
	dir := t.TempDir()
	lock, err := state.New(dir).TakeWatcherLock("v1.2.3")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Close() })

	_, err = state.New(dir).TakeWatcherLock("v2.0.0")

	var held *state.HeldError
	require.ErrorAs(t, err, &held)
	require.Equal(t, &state.HeldError{PID: os.Getpid(), Version: "v1.2.3"}, held)
	require.EqualError(t, err, fmt.Sprintf("the watcher lock is held by pid %d, version v1.2.3", os.Getpid()))
}

func TestWatcherLockRetakenAfterClose(t *testing.T) {
	dir := t.TempDir()
	first, err := state.New(dir).TakeWatcherLock("v1.2.3")
	require.NoError(t, err)
	require.NoError(t, first.Close())

	second, err := state.New(dir).TakeWatcherLock("v2.0.0")
	require.NoError(t, err)
	require.NoError(t, second.Close())
}

func TestWatcherLockFreedWhenHolderKilled(t *testing.T) {
	dir := t.TempDir()
	holder := startWatcherLockHolder(t, dir, "v1.2.3")

	_, err := state.New(dir).TakeWatcherLock("v2.0.0")
	var held *state.HeldError
	require.ErrorAs(t, err, &held)
	require.Equal(t, &state.HeldError{PID: holder.Process.Pid, Version: "v1.2.3"}, held)

	require.NoError(t, holder.Process.Kill())
	_ = holder.Wait()

	lock, err := state.New(dir).TakeWatcherLock("v2.0.0")
	require.NoError(t, err)
	require.NoError(t, lock.Close())
}

func TestWatcherHolder(t *testing.T) {
	dir := t.TempDir()
	_, err := state.New(dir).WatcherHolder()
	require.ErrorIs(t, err, fs.ErrNotExist)

	lock, err := state.New(dir).TakeWatcherLock("v1.2.3")
	require.NoError(t, err)
	held, err := state.New(dir).WatcherHolder()
	require.NoError(t, err)
	require.Equal(t, state.HeldError{PID: os.Getpid(), Version: "v1.2.3"}, held)

	require.NoError(t, lock.Close())
	held, err = state.New(dir).WatcherHolder()
	require.NoError(t, err)
	require.Equal(t, os.Getpid(), held.PID, "the last holder stays written after it lets go")
}
