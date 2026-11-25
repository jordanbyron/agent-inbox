package state_test

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/state"
)

// TestMain lets a test re-run this binary as a helper process, so the locks
// are proven between processes and not only between goroutines.
func TestMain(m *testing.M) {
	switch os.Getenv("STATE_TEST_HELPER") {
	case "":
		os.Exit(m.Run())
	case "add-records":
		os.Exit(addRecordsHelper())
	case "hold-watcher-lock":
		os.Exit(holdWatcherLockHelper())
	default:
		os.Exit(2)
	}
}

func addRecordsHelper() int {
	count, err := strconv.Atoi(os.Getenv("STATE_TEST_COUNT"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := addRecords(state.New(os.Getenv("STATE_TEST_DIR")), os.Getenv("STATE_TEST_WRITER"), count); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func holdWatcherLockHelper() int {
	_, err := state.New(os.Getenv("STATE_TEST_DIR")).TakeWatcherLock(os.Getenv("STATE_TEST_VERSION"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("held")
	time.Sleep(time.Hour)
	return 0
}

// addRecords makes count updates, each adding a record the update before
// it did not have, so a lost update shows up as a missing record.
func addRecords(store *state.Store, writer string, count int) error {
	for i := range count {
		err := store.Update(func(s *state.State) error {
			s.Records[fmt.Sprintf("%s-%d", writer, i)] = state.Record{Name: writer}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func helperCommand(t *testing.T, helper string, env ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), append([]string{"STATE_TEST_HELPER=" + helper}, env...)...)
	cmd.Stderr = os.Stderr
	return cmd
}

func startWatcherLockHolder(t *testing.T, dir, version string) *exec.Cmd {
	t.Helper()
	cmd := helperCommand(t, "hold-watcher-lock", "STATE_TEST_DIR="+dir, "STATE_TEST_VERSION="+version)
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	line, err := bufio.NewReader(out).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "held\n", line)
	return cmd
}
