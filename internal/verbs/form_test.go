package verbs_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/rex"
)

func TestNewOpensTheForm(t *testing.T) {
	here, err := os.Getwd()
	require.NoError(t, err)

	t.Run("in a floating layer over the pane's own tab, inside Rex", func(t *testing.T) {
		w := newWorld(t)
		w.tab(t, "editor", "")
		listBlock := w.tab(t, "inbox", "")
		w.env["REX_SESSION"], w.env["REX_BLOCK"] = w.session, listBlock
		view, err := w.rex.View(t.Context(), w.session)
		require.NoError(t, err)

		cmd, err := w.inbox.ListActions(t.Context()).New()

		require.NoError(t, err)
		require.Nil(t, cmd, "nothing is left for the list to run")
		require.Equal(t, []rex.LayerOptions{{
			SessionID: w.session,
			WindowID:  view.Windows[1].ID,
			Command:   []string{"/opt/agent-inbox", "new"},
			Dir:       here,
			Bounds:    [4]float64{0.1, 0.1, 0.8, 0.8},
		}}, w.rex.Layers())
	})

	t.Run("in place, outside Rex", func(t *testing.T) {
		w := newWorld(t)

		cmd, err := w.inbox.ListActions(t.Context()).New()

		require.NoError(t, err)
		require.Equal(t, []string{"/opt/agent-inbox", "new"}, cmd.Args)
		require.Equal(t, here, cmd.Dir)
		require.Empty(t, w.rex.Layers())
	})

	t.Run("a pane Rex no longer has", func(t *testing.T) {
		w := newWorld(t)
		w.env["REX_SESSION"], w.env["REX_BLOCK"] = w.session, "block:gone"

		_, err := w.inbox.ListActions(t.Context()).New()

		require.ErrorContains(t, err, "block:gone is in no window")
	})
}
