package list_test

import (
	"errors"
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
	return released(t, width, height, "", installed, nil)
}

// released is the list running v0.2.0 with a snapshot that says latest is
// the newest release, and upgrade to install it.
func released(t *testing.T, width, height int, latest string, installed *string, upgrade func() error) *screen {
	t.Helper()
	snap := fixture(t)
	snap.Latest = latest
	s := &screen{t: t, actions: &recorder{}, clock: now}
	s.model = list.New(list.Config{
		Load:      func() (snapshot.Snapshot, time.Time, error) { return snap, now, nil },
		Actions:   s.actions,
		Now:       func() time.Time { return s.clock },
		Version:   "v0.2.0",
		Installed: func() (string, error) { return *installed, nil },
		Upgrade:   upgrade,
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

func TestReleasePill(t *testing.T) {
	installed := "v0.2.0"
	upgrades := 0
	var upgradeErr error
	upgrade := func() error {
		upgrades++
		if upgradeErr == nil {
			installed = "v0.3.0"
		}
		return upgradeErr
	}

	t.Run("the release running shows no pill, and U does nothing", func(t *testing.T) {
		s := released(t, 100, 30, "v0.2.0", &installed, upgrade)
		require.NotContains(t, s.frame(), "available")
		s.press("U")
		require.Zero(t, upgrades)
	})

	t.Run("without a way to upgrade, it only says so", func(t *testing.T) {
		s := released(t, 100, 30, "v0.3.0", &installed, nil)
		require.Contains(t, s.frame(), "↑ agent-inbox v0.3.0 available │")
		s.press("U")
		require.Nil(t, s.last)
	})

	s := released(t, 100, 30, "v0.3.0", &installed, upgrade)

	t.Run("a newer one offers an upgrade", func(t *testing.T) {
		require.Contains(t, s.frame(), "↑ agent-inbox v0.3.0 available   U upgrade")
	})

	t.Run("a failed upgrade says why and keeps the offer", func(t *testing.T) {
		upgradeErr = errors.New("brew upgrade: Error: no bottle available")
		s.press("U")
		require.Contains(t, s.header(), "no bottle available")
		require.Contains(t, s.frame(), "U upgrade")
		upgradeErr = nil
		s.clock = s.clock.Add(5 * time.Second)
	})

	t.Run("U upgrades, then offers a relaunch at once", func(t *testing.T) {
		s.send(keyPress("U"))
		require.Contains(t, s.header(), "upgrading agent-inbox…")
		s.send(s.last()) // the upgrade
		s.send(s.last()) // the check of what is installed
		require.Equal(t, 2, upgrades)
		require.Contains(t, s.frame(), "✓ agent-inbox v0.3.0 installed   R relaunch")
		require.NotContains(t, s.frame(), "available")
	})
}

func TestGoldenPill(t *testing.T) {
	installed := "v0.3.0"
	golden.Require(t, "pill_100x30", upgraded(t, 100, 30, &installed).check().frame())
	golden.Require(t, "pill_50x30", upgraded(t, 50, 30, &installed).check().frame())
	installed = "v0.2.0"
	upgrade := func() error { return nil }
	golden.Require(t, "release_pill_100x30", released(t, 100, 30, "v0.3.0", &installed, upgrade).frame())
	golden.Require(t, "release_pill_40x30", released(t, 40, 30, "v0.3.0", &installed, upgrade).frame())
}
