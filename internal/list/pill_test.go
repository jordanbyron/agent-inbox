package list_test

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/golden"
	"github.com/jordanbyron/agent-inbox/internal/list"
	"github.com/jordanbyron/agent-inbox/internal/snapshot"
)

// upgraded is the list running v0.2.0 with whatever installed reports on
// the binary's path.
func upgraded(t *testing.T, width, height int, installed *string) *screen {
	t.Helper()
	s := &screen{t: t, actions: &recorder{}, clock: now}
	s.model = list.New(list.Config{
		Load:      func() (snapshot.Snapshot, time.Time, error) { return fixture(t), now, nil },
		Actions:   s.actions,
		Now:       func() time.Time { return s.clock },
		Version:   "v0.2.0",
		Installed: func() (string, error) { return *installed, nil },
	})
	return s.send(tea.WindowSizeMsg{Width: width, Height: height})
}

// check lets the next reload tick ask what is installed, and answers it.
func (s *screen) check() *screen {
	s.clock = s.clock.Add(5 * time.Second)
	s.send(s.model.Init()().(tea.BatchMsg)[0]())
	s.send(s.last().(tea.BatchMsg)[1]())
	return s
}

func TestUpgradePill(t *testing.T) {
	installed := "v0.2.0"
	s := upgraded(t, 100, 30, &installed)

	t.Run("the running version shows no pill, and R does nothing", func(t *testing.T) {
		s.check()
		require.NotContains(t, s.frame(), "relaunch")
		s.press("R")
		require.False(t, s.model.Relaunching())
		require.Nil(t, s.last)
	})

	t.Run("another one installed offers a relaunch", func(t *testing.T) {
		installed = "v0.3.0"
		s.check()
		require.Contains(t, s.frame(), "✓ agent-inbox v0.3.0 installed   R relaunch")
	})

	t.Run("R quits to relaunch", func(t *testing.T) {
		s.press("R")
		require.True(t, s.model.Relaunching())
		require.IsType(t, tea.QuitMsg{}, s.last())
	})
}

func TestGoldenPill(t *testing.T) {
	installed := "v0.3.0"
	golden.Require(t, "pill_100x30", upgraded(t, 100, 30, &installed).check().frame())
	golden.Require(t, "pill_50x30", upgraded(t, 50, 30, &installed).check().frame())
}
