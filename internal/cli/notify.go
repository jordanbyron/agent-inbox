package cli

import (
	"log/slog"

	"github.com/jordanbyron/agent-inbox/internal/config"
	"github.com/jordanbyron/agent-inbox/internal/notify"
	"github.com/jordanbyron/agent-inbox/internal/watcher"
)

// With no program to notify through, the watcher runs on without notifying.
func desktopNotifier(cfg config.Config, log *slog.Logger) watcher.Notifier {
	if !cfg.Notify {
		return nil
	}
	desktop, err := notify.NewSystemDesktop()
	if err != nil {
		log.Warn("notify is on, but nothing can send notifications", "err", err)
		return nil
	}
	log.Info("notifying", "via", desktop.Program)
	return desktop
}
