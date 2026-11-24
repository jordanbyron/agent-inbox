package cli

import (
	"log/slog"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/jordanbyron/agent-inbox/internal/config"
	"github.com/jordanbyron/agent-inbox/internal/pulls"
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
			return watcher.Run(ctx, watcher.Options{
				Rex:      srv.rex,
				GH:       pulls.GH{},
				Git:      worktree.ExecGit{},
				Clock:    watcher.SystemClock{},
				StateDir: srv.dir,
				Config:   cfg,
				Version:  version(),
				Log:      slog.New(slog.NewTextHandler(log, nil)),
			})
		},
	}
}
