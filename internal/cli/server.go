package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/watcher"
)

// server is the Rex server agent-inbox works with, and its state directory.
type server struct {
	rex *rex.Client
	dir string
}

func findRexBinary() (string, error) {
	bin, err := rex.FindBinary(os.Getenv)
	if errors.Is(err, rex.ErrNoBinary) {
		return "", errors.New("agent-inbox needs Rex: no rex binary in REX_BIN_DIR, PATH or " + rex.MacAppBinary)
	}
	return bin, err
}

func findServer(ctx context.Context) (server, error) {
	bin, err := findRexBinary()
	if err != nil {
		return server{}, err
	}
	client := rex.New(bin)
	srv, err := client.Server(ctx)
	if err != nil {
		return server{}, fmt.Errorf("cannot reach the Rex server: %w", err)
	}
	dir, err := state.Dir(srv.StateID())
	if err != nil {
		return server{}, err
	}
	return server{rex: client, dir: dir}, nil
}

// ensureWatcher starts this binary's watcher for the server, unless one is
// already writing a fresh snapshot.
func ensureWatcher(ctx context.Context, srv server) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding this binary to start the watcher: %w", err)
	}
	return watcher.EnsureRunning(ctx, watcher.Launch{
		StateDir: srv.dir,
		Version:  version(),
		Command:  []string{exe, "watch"},
	})
}
