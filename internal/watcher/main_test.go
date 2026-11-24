package watcher_test

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"testing"

	"github.com/jordanbyron/agent-inbox/internal/config"
	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/rex/rextest"
	"github.com/jordanbyron/agent-inbox/internal/watcher"
	"github.com/jordanbyron/agent-inbox/internal/worktree"
)

// TestMain lets a test re-run this binary as a watcher of its own, so the
// lock and the takeover are proven between processes.
func TestMain(m *testing.M) {
	if os.Getenv("WATCHER_TEST_HELPER") == "watch" {
		os.Exit(watchHelper())
	}
	os.Exit(m.Run())
}

// watchHelper runs a watcher over an empty fake Rex server until SIGTERM.
func watchHelper() int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	log, err := watcher.OpenLog(os.Getenv("WATCHER_TEST_DIR"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = log.Close() }()
	err = watcher.Run(ctx, watcher.Options{
		Rex:      rextest.New(rex.Server{HostID: "host"}),
		GH:       newFakeGH(),
		Git:      worktree.ExecGit{},
		Clock:    watcher.SystemClock{},
		StateDir: os.Getenv("WATCHER_TEST_DIR"),
		Config:   config.Default(),
		Version:  os.Getenv("WATCHER_TEST_VERSION"),
		Log:      newLogger(log),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
