package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/jordanbyron/agent-inbox/internal/config"
	"github.com/jordanbyron/agent-inbox/internal/pulls"
	"github.com/jordanbyron/agent-inbox/internal/upgrade"
	"github.com/jordanbyron/agent-inbox/internal/watcher"
	"github.com/jordanbyron/agent-inbox/internal/worktree"
)

func newWatchCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "watch",
		Short: "Run the watcher in the foreground (other commands start it for you)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, syscall.SIGINT)
			defer stop()
			srv, err := findServer(ctx)
			if err != nil {
				return err
			}
			path, err := config.Path()
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			log, err := watcher.OpenLog(srv.dir)
			if err != nil {
				return err
			}
			defer func() { _ = log.Close() }()
			logger := slog.New(slog.NewTextHandler(log, nil))
			deleter, err := reapDeleter(srv, cfg)
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("finding the home directory: %w", err)
			}
			return watcher.Run(ctx, watcher.Options{
				Rex:      srv.rex,
				GH:       pulls.GH{},
				Git:      worktree.ExecGit{},
				Clock:    watcher.SystemClock{},
				StateDir: srv.dir,
				Home:     home,
				Config:   cfg,
				Version:  version(),
				Log:      logger,
				Deleter:  deleter,
				Releases: upgrade.GitHub{},
			})
		},
	}
}
