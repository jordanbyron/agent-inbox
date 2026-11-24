package list_test

import (
	"errors"
	"os/exec"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/list"
	"github.com/jordanbyron/agent-inbox/internal/snapshot"
	"github.com/jordanbyron/agent-inbox/internal/snoozetime"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

func tall(t *testing.T) *screen {
	t.Helper()
	return newScreen(t, fixture(t), 100, 80)
}

func TestMotion(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		want string
	}{
		{"the cursor starts on the first row", nil, "release notes for v2"},
		{"j moves down", []string{"j"}, "fix the login redirect loop"},
		{"the down arrow moves down", []string{"down"}, "fix the login redirect loop"},
		{"k moves up", []string{"j", "j", "k"}, "fix the login redirect loop"},
		{"k stops at the top", []string{"k", "k"}, "release notes for v2"},
		{"children are stops of their own", []string{"j", "j", "j"}, "queue names"},
		{"G goes to the last stop, a fold", []string{"G"}, "… 3 settled"},
		{"gg goes back to the top", []string{"G", "g", "g"}, "release notes for v2"},
		{"j stops at the bottom", []string{"G", "j"}, "… 3 settled"},
		{"Tab goes to the next section", []string{"tab"}, "fix the login redirect loop"},
		{"Tab walks the folds too", []string{"tab", "tab", "tab"}, "… 1 working"},
		{"Tab wraps", []string{"tab", "tab", "tab", "tab", "tab", "tab"}, "release notes for v2"},
		{"Shift-Tab wraps backwards", []string{"shift+tab"}, "… 3 settled"},
		{"Ctrl-d moves half a page of stops", []string{"ctrl+d"}, "… 1 working"},
		{"Ctrl-f moves a page of stops, as far as the last", []string{"ctrl+f"}, "… 3 settled"},
		{"Ctrl-u comes back half a page", []string{"ctrl+f", "ctrl+u"}, "migrate billing"},
		{"Ctrl-b comes back a page", []string{"ctrl+f", "ctrl+b"}, "release notes for v2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newScreen(t, fixture(t), 100, 30).press(tc.keys...)
			require.Contains(t, s.selectedLine(), tc.want)
		})
	}
}

func TestEveryStopInOrder(t *testing.T) {
	s := tall(t)
	for _, want := range []string{
		"release notes for v2",
		"fix the login redirect loop",
		"migrate billing to the new queue",
		"?   queue names",
		"◦   audit the old cron jobs",
		"⠋   port the retry worker",
		"nightly import",
		"sync the team calendar",
		"· claude",
		"Explain why the flaky spec",
		"deploy preview",
		"🚀 launch checklist",
		"bump dependencies",
		"triage github issues",
		"… 1 working   ⏎ or zo to expand",
		"… 2 snoozed   ⏎ or zo to expand",
		"… 3 settled   ⏎ or zo to expand",
		"… 3 settled   ⏎ or zo to expand",
	} {
		require.Contains(t, s.selectedLine(), want)
		s.press("j")
	}
}

func TestChords(t *testing.T) {
	t.Run("a chord with an unknown second key does nothing, and the key after it counts", func(t *testing.T) {
		s := tall(t).press("j", "g", "x")
		require.Empty(t, s.actions.calls)
		s.press("x")
		require.Equal(t, []string{"settle b81e04"}, s.actions.calls)
	})

	t.Run("a chord times out after a second", func(t *testing.T) {
		s := tall(t).press("G", "g")
		s.clock = s.clock.Add(1100 * time.Millisecond)
		s.press("g")
		require.Contains(t, s.selectedLine(), "… 3 settled")
		s.press("g")
		require.Contains(t, s.selectedLine(), "release notes for v2")
	})
}

func TestFolds(t *testing.T) {
	cases := []struct {
		name    string
		keys    []string
		want    string
		visible string
	}{
		{name: "zo opens the fold under the cursor and lands on its first row", keys: []string{"G", "z", "o"}, want: "update the README"},
		{name: "Enter on a fold opens it", keys: []string{"G", "enter"}, want: "update the README"},
		{name: "l on a fold opens it", keys: []string{"G", "l"}, want: "update the README"},
		{name: "zc closes it and lands on its fold line", keys: []string{"G", "z", "o", "j", "z", "c"}, want: "… 3 settled"},
		{name: "h closes it too", keys: []string{"G", "z", "o", "h"}, want: "… 3 settled"},
		{name: "za toggles", keys: []string{"G", "z", "a", "z", "a"}, want: "… 3 settled"},
		{
			name: "zo inside an open section leaves the cursor where it is",
			keys: []string{"G", "z", "o", "j", "z", "o"}, want: "spike: websocket client",
		},
		{name: "a fold elsewhere stays shut", keys: []string{"G", "z", "o"}, visible: "… 2 snoozed"},
		{name: "zo on a section that never folds does nothing", keys: []string{"z", "o"}, want: "release notes for v2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := tall(t).press(tc.keys...)
			if tc.want != "" {
				require.Contains(t, s.selectedLine(), tc.want)
			}
			if tc.visible != "" {
				require.Contains(t, s.frame(), tc.visible)
			}
		})
	}
}

func TestActions(t *testing.T) {
	tomorrow, ok := snoozetime.Parse("tomorrow", now)
	require.True(t, ok)
	inTwoHours := now.Add(2 * time.Hour)

	cases := []struct {
		name  string
		keys  []string
		typed string
		then  []string
		want  []string
	}{
		{name: "Enter shows the agent", keys: []string{"enter"}, want: []string{"open a7f3c2"}},
		{name: "l shows the agent", keys: []string{"j", "l"}, want: []string{"open b81e04"}},
		{name: "Enter on a closed child goes to it, to restore it", keys: []string{"j", "j", "j", "j", "enter"}, want: []string{"open c4d9ad"}},
		{name: "n starts a new agent", keys: []string{"n"}, want: []string{"new"}},
		{name: "t pins", keys: []string{"t"}, want: []string{"pin a7f3c2"}},
		{name: "u wakes", keys: []string{"u"}, want: []string{"wake a7f3c2"}},
		{name: "x settles", keys: []string{"x"}, want: []string{"settle a7f3c2"}},
		{name: "a verb on a child names the child", keys: []string{"j", "j", "j", "x"}, want: []string{"settle c4d9ab"}},
		{name: "s 1 snoozes for 15 minutes", keys: []string{"s", "1"}, want: []string{"snooze a7f3c2 until " + now.Add(15*time.Minute).Format(time.RFC3339)}},
		{name: "s 2 snoozes for an hour", keys: []string{"s", "2"}, want: []string{"snooze a7f3c2 until " + now.Add(time.Hour).Format(time.RFC3339)}},
		{name: "s 3 snoozes until tomorrow at 9", keys: []string{"s", "3"}, want: []string{"snooze a7f3c2 until " + tomorrow.Format(time.RFC3339)}},
		{name: "s 4 snoozes until woken", keys: []string{"s", "4"}, want: []string{"snooze a7f3c2 until woken"}},
		{name: "s 5 snoozes until a typed time", keys: []string{"s", "5"}, typed: "2h", then: []string{"enter"}, want: []string{"snooze a7f3c2 until " + inTwoHours.Format(time.RFC3339)}},
		{name: "s esc snoozes nothing", keys: []string{"s", "esc"}},
		{name: "s q snoozes nothing", keys: []string{"s", "q"}},
		{name: "s ignores a key it has no choice for", keys: []string{"s", "9"}},
		{name: "a sets an alias", keys: []string{"a"}, typed: "release", then: []string{"enter"}, want: []string{"alias a7f3c2 release"}},
		{name: "a saves an emptied alias, which clears it", keys: []string{"j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "a"}, then: []string{"backspace", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace", "enter"}, want: []string{"alias f4c5d6 "}},
		{name: "a takes q as text", keys: []string{"a"}, typed: "q", then: []string{"enter"}, want: []string{"alias a7f3c2 q"}},
		{name: "a esc saves nothing", keys: []string{"a"}, typed: "x", then: []string{"esc"}},
		{name: "P links a pull request", keys: []string{"j", "j", "j", "P"}, typed: "https://github.com/acme/billing/pull/80", then: []string{"enter"}, want: []string{"link c4d9ab https://github.com/acme/billing/pull/80"}},
		{name: "Ctrl-x y deletes", keys: []string{"ctrl+x", "y"}, want: []string{"delete a7f3c2"}},
		{name: "Ctrl-x esc keeps it", keys: []string{"ctrl+x", "esc"}},
		{name: "Ctrl-x n keeps it", keys: []string{"ctrl+x", "n"}},
		{name: "o opens the one pull request", keys: []string{"o"}, want: []string{"open https://github.com/acme/app/pull/912"}},
		{name: "y copies the one pull request", keys: []string{"y"}, want: []string{"copy https://github.com/acme/app/pull/912"}},
		{name: "o on a row without one asks for nothing", keys: []string{"tab", "tab", "o"}},
		{name: "o o opens the headline of several", keys: []string{"tab", "tab", "j", "j", "j", "j", "o", "o"}, want: []string{"open https://github.com/acme/app/pull/940"}},
		{name: "o Enter opens the headline too", keys: []string{"tab", "tab", "j", "j", "j", "j", "o", "enter"}, want: []string{"open https://github.com/acme/app/pull/940"}},
		{name: "o 2 opens the second", keys: []string{"tab", "tab", "j", "j", "j", "j", "o", "2"}, want: []string{"open https://github.com/acme/app/pull/941"}},
		{name: "o 3 is not on the list", keys: []string{"tab", "tab", "j", "j", "j", "j", "o", "3", "esc"}},
		{name: "y 2 copies the second", keys: []string{"tab", "tab", "j", "j", "j", "j", "y", "2"}, want: []string{"copy https://github.com/acme/app/pull/941"}},
		{name: "y y copies the headline", keys: []string{"tab", "tab", "j", "j", "j", "j", "y", "y"}, want: []string{"copy https://github.com/acme/app/pull/940"}},
		{name: "a verb on a fold does nothing", keys: []string{"G", "t", "x", "s", "a", "P", "o", "y", "ctrl+x", "u"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := tall(t).press(tc.keys...).typing(tc.typed).press(tc.then...)
			want := tc.want
			if want == nil {
				want = []string{}
			}
			calls := s.actions.calls
			if calls == nil {
				calls = []string{}
			}
			require.Equal(t, want, calls)
		})
	}
}

func TestQuit(t *testing.T) {
	for _, key := range []string{"q", "ctrl+c"} {
		t.Run(key, func(t *testing.T) {
			s := tall(t).press(key)
			require.NotNil(t, s.last)
			require.IsType(t, tea.QuitMsg{}, s.last())
		})
	}
}

func TestModal(t *testing.T) {
	quits := func(t *testing.T, s *screen) {
		t.Helper()
		require.NotNil(t, s.last)
		require.IsType(t, tea.QuitMsg{}, s.last())
	}
	stays := func(t *testing.T, s *screen) {
		t.Helper()
		require.Nil(t, s.last)
	}

	t.Run("Enter shows the agent and closes it", func(t *testing.T) {
		s := newModal(t, 30).press("enter")
		require.Equal(t, []string{"open a7f3c2"}, s.actions.calls)
		quits(t, s)
	})

	t.Run("a refused open keeps it up and says why", func(t *testing.T) {
		s := newModal(t, 30)
		s.actions.err = errors.New("no such tab")
		s.press("enter")
		stays(t, s)
		require.Contains(t, s.header(), "no such tab")
	})

	t.Run("Enter on a fold opens the fold", func(t *testing.T) {
		s := newModal(t, 30).press("G", "enter")
		require.Contains(t, s.selectedLine(), "update the README")
		stays(t, s)
	})

	for _, key := range []string{"esc", "q"} {
		t.Run(key+" closes it", func(t *testing.T) {
			quits(t, newModal(t, 30).press(key))
		})
	}

	t.Run("Esc clears a kept filter before it closes", func(t *testing.T) {
		s := newModal(t, 30).press("/").typing("billing").press("enter", "esc")
		stays(t, s)
		require.Contains(t, s.frame(), "release notes for v2")
		quits(t, s.press("esc"))
	})

	t.Run("Esc in the filter only leaves the filter", func(t *testing.T) {
		stays(t, newModal(t, 30).press("/").typing("billing").press("esc"))
	})

	t.Run("Esc in a dialog only closes the dialog", func(t *testing.T) {
		s := newModal(t, 30).press("s", "esc")
		stays(t, s)
		require.NotContains(t, s.frame(), "15 minutes")
	})

	t.Run("h still closes a fold", func(t *testing.T) {
		s := newModal(t, 80).press("G", "z", "o", "h")
		require.Contains(t, s.selectedLine(), "… 3 settled")
		stays(t, s)
	})

	t.Run("the left arrow does nothing", func(t *testing.T) {
		stays(t, newModal(t, 30).send(tea.KeyPressMsg{Code: tea.KeyLeft}))
	})

	t.Run("without --modal Esc and Enter leave the list up", func(t *testing.T) {
		stays(t, tall(t).press("esc"))
		stays(t, tall(t).press("enter"))
	})
}

func TestPromptsStartFromWhatIsSet(t *testing.T) {
	s := tall(t).press("tab", "tab", "j", "j", "a")
	require.Contains(t, s.frame(), "> deploy preview")

	s = tall(t).press("tab", "tab", "j", "j", "j", "j", "j", "P")
	require.Contains(t, s.frame(), "> https://github.com/acme/app/pull/77")
}

func TestATypedSnoozeItCannotReadKeepsThePromptUp(t *testing.T) {
	s := tall(t).press("s", "5").typing("later").press("enter")

	require.Empty(t, s.actions.calls)
	require.Contains(t, s.header(), "try 45m, 2h, 3d")
	require.Contains(t, s.frame(), "> later")

	s.press("esc")
	require.NotContains(t, s.frame(), "Wake at")
}

func TestARefusedLinkKeepsThePromptUp(t *testing.T) {
	s := tall(t)
	s.actions.err = errors.New("that's not a github.com pull request url")
	s.press("P").typing("x").press("enter")

	require.Contains(t, s.header(), "that's not a github.com pull request url")
	require.Contains(t, s.frame(), "Pull request URL")
}

func TestAnActionsErrorGoesToTheStatusLineForAWhile(t *testing.T) {
	s := newScreen(t, fixture(t), 100, 30)
	s.model = list.New(list.Config{
		Load:    func() (snapshot.Snapshot, time.Time, error) { return fixture(t), now, nil },
		Actions: list.NotWired(),
		Now:     func() time.Time { return s.clock },
	})
	s.send(tea.WindowSizeMsg{Width: 100, Height: 30}).press("t")
	require.Contains(t, s.header(), "pin is not wired yet")

	s.clock = s.clock.Add(5 * time.Second)
	s.send(list.ReloadMsg{})
	require.NotContains(t, s.header(), "not wired")
}

func TestASlowActionRunsBehindTheScreen(t *testing.T) {
	s := tall(t)
	s.actions.gate = make(chan struct{})

	s.send(keyPress("x"))
	require.Contains(t, s.header(), "settling…")
	require.Empty(t, s.actions.calls, "the key returns before the action runs")

	settle := s.last
	result := make(chan tea.Msg)
	go func() { result <- settle() }()
	s.press("j")
	require.Contains(t, s.selectedLine(), "fix the login redirect loop", "keys still move the cursor")
	s.press("t")
	require.Contains(t, s.header(), "settling…", "a second action waits for the first")

	close(s.actions.gate)
	s.send(<-result)
	require.Equal(t, []string{"settle a7f3c2"}, s.actions.calls)
	require.Contains(t, s.header(), "settled — u brings it back")
}

func TestNewRunsTheFormHereOnlyWhenItHasTo(t *testing.T) {
	t.Run("a form opened elsewhere leaves the list as it was", func(t *testing.T) {
		s := tall(t).press("n")
		require.Equal(t, []string{"new"}, s.actions.calls)
		require.Nil(t, s.last)
		require.NotContains(t, s.header(), "opening the form")
	})

	t.Run("a form to run here takes the terminal until it exits", func(t *testing.T) {
		s := tall(t)
		s.actions.form = exec.Command("true")
		s.send(keyPress("n"))
		s.send(s.last())
		require.NotNil(t, s.last, "the list hands the form a command to run")
		require.Contains(t, s.header(), "opening the form…")
	})
}

func TestAFinishedActionReadsTheSnapshotAgain(t *testing.T) {
	loads := 0
	s := &screen{t: t, actions: &recorder{}, clock: now}
	s.model = list.New(list.Config{
		Load: func() (snapshot.Snapshot, time.Time, error) {
			loads++
			return fixture(t), now, nil
		},
		Actions: s.actions,
		Now:     func() time.Time { return s.clock },
	})
	s.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	before := loads

	s.press("t")
	require.Equal(t, before+1, loads)

	s.actions.err = errNotWired
	s.press("t")
	require.Equal(t, before+1, loads, "a refused action changed nothing to read")
}

func TestTheCursorAfterARowLeaves(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		want string
	}{
		{"settling lands on the row below", []string{"tab", "tab", "j", "j", "j", "x"}, "bump dependencies"},
		{"settling the last row lands on the one above", []string{"G", "z", "o", "G", "x"}, "spike: websocket client"},
		{"snoozing from the menu lands on the row below", []string{"tab", "tab", "j", "j", "j", "s", "2"}, "bump dependencies"},
		{"snoozing until a typed time lands on the row below", []string{"tab", "tab", "j", "j", "j", "s", "5", "2", "h", "enter"}, "bump dependencies"},
		{"closing the menu without a choice stays put", []string{"tab", "tab", "j", "j", "j", "s", "esc"}, "🚀 launch checklist"},
		{"deleting lands on the row below", []string{"tab", "tab", "j", "j", "j", "ctrl+x", "y"}, "bump dependencies"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Contains(t, tall(t).press(tc.keys...).selectedLine(), tc.want)
		})
	}

	t.Run("a refused settle stays put", func(t *testing.T) {
		s := tall(t)
		s.actions.err = errNotWired
		require.Contains(t, s.press("x").selectedLine(), "release notes for v2")
	})
}

func TestFilter(t *testing.T) {
	t.Run("narrows the list as you type and keeps families whole", func(t *testing.T) {
		s := tall(t).press("/").typing("queue")
		frame := s.frame()
		require.Contains(t, frame, "migrate billing to the new queue")
		require.Contains(t, frame, "audit the old cron jobs")
		require.NotContains(t, frame, "release notes for v2")
		require.Contains(t, s.selectedLine(), "migrate billing")
	})

	t.Run("takes letters as text while it is open", func(t *testing.T) {
		s := tall(t).press("/").typing("jq").press("enter")
		require.Empty(t, s.actions.calls)
		require.Contains(t, s.lines()[len(s.lines())-1], "/jq  esc clears")
	})

	t.Run("finds an agent by its pull request number", func(t *testing.T) {
		frame := tall(t).press("/").typing("#941").frame()
		require.Contains(t, frame, "bump dependencies")
		require.NotContains(t, frame, "release notes")
	})

	t.Run("Enter keeps it and gives the keys back", func(t *testing.T) {
		s := tall(t).press("/").typing("billing").press("enter", "j")
		require.Contains(t, s.selectedLine(), "queue names")
	})

	t.Run("Esc clears it", func(t *testing.T) {
		s := tall(t).press("/").typing("billing").press("enter", "esc")
		require.Contains(t, s.frame(), "release notes for v2")
		require.Contains(t, s.lines()[len(s.lines())-1], "j/k move")
	})

	t.Run("a backspace from empty closes it", func(t *testing.T) {
		s := tall(t).press("/").typing("b").press("backspace")
		require.Contains(t, s.lines()[len(s.lines())-1], " /")
		s.press("backspace")
		require.Contains(t, s.lines()[len(s.lines())-1], "j/k move")
	})
}

func TestReloading(t *testing.T) {
	snap := fixture(t)
	var current snapshot.Snapshot
	writtenAt := now
	s := &screen{t: t, actions: &recorder{}, clock: now}
	current = snap
	s.model = list.New(list.Config{
		Load:    func() (snapshot.Snapshot, time.Time, error) { return current, writtenAt, nil },
		Actions: s.actions,
		Now:     func() time.Time { return s.clock },
	})
	s.send(tea.WindowSizeMsg{Width: 100, Height: 80}).press("j", "j")
	require.Contains(t, s.selectedLine(), "migrate billing")

	t.Run("the cursor stays on its row when rows move above it", func(t *testing.T) {
		current = snapshot.New("v1", map[triage.Section][]snapshot.Row{
			triage.SectionActive:   rowsOf(snap, triage.SectionActive),
			triage.SectionNeedsYou: rowsOf(snap, triage.SectionNeedsYou),
		}, snapshot.SortDefault)
		s.send(list.ReloadMsg{})
		require.Contains(t, s.selectedLine(), "migrate billing")
		require.NotContains(t, s.frame(), "release notes for v2")
	})

	t.Run("a row that has gone leaves the cursor on what took its place", func(t *testing.T) {
		needsYou := rowsOf(snap, triage.SectionNeedsYou)
		current = snapshot.New("v1", map[triage.Section][]snapshot.Row{
			triage.SectionNeedsYou: needsYou[4:],
		}, snapshot.SortDefault)
		s.send(list.ReloadMsg{})
		require.Contains(t, s.selectedLine(), "nightly import")
	})

	t.Run("a tick reads the snapshot again", func(t *testing.T) {
		current = snap
		s.send(list.ReloadMsg{})
		writtenAt = now.Add(-20 * time.Second)
		require.NotContains(t, s.header(), "old")
		reloadTick := s.model.Init()().(tea.BatchMsg)[0]
		s.send(reloadTick())
		require.Contains(t, s.header(), "⚠ snapshot 20s old")
	})

	t.Run("a failed read keeps the rows and says why", func(t *testing.T) {
		s.model = list.New(list.Config{
			Load: func() (snapshot.Snapshot, time.Time, error) {
				return snapshot.Snapshot{}, time.Time{}, errors.New("reading snapshot.json: no such file")
			},
			Actions: s.actions,
			Now:     func() time.Time { return s.clock },
		})
		s.send(tea.WindowSizeMsg{Width: 100, Height: 30})
		require.Contains(t, s.header(), "⚠ reading snapshot.json: no such file")
		require.Contains(t, s.frame(), "No snapshot yet.")
	})
}

func rowsOf(snap snapshot.Snapshot, section triage.Section) []snapshot.Row {
	for _, sec := range snap.Sections {
		if sec.Name == section {
			return sec.Rows
		}
	}
	return nil
}

func TestStaleness(t *testing.T) {
	s := tall(t)
	s.clock = now.Add(list.StaleAfter)
	s.send(list.ReloadMsg{})
	require.NotContains(t, s.header(), "old")

	s.clock = now.Add(list.StaleAfter + time.Second)
	s.send(list.ReloadMsg{})
	require.Contains(t, s.header(), "⚠ snapshot 16s old — is the watcher running?")
	require.NotContains(t, s.header(), "need you")
}

func TestAStaleSnapshotStartsTheWatcherAgain(t *testing.T) {
	writtenAt := now
	var restartErr error
	s := &screen{t: t, actions: &recorder{}, clock: now}
	s.model = list.New(list.Config{
		Load:    func() (snapshot.Snapshot, time.Time, error) { return fixture(t), writtenAt, nil },
		Actions: s.actions,
		Now:     func() time.Time { return s.clock },
		Restart: func() error {
			if restartErr == nil {
				writtenAt = s.clock
			}
			return restartErr
		},
	})
	s.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	reloadTick := s.model.Init()().(tea.BatchMsg)[0]()

	t.Run("a fresh snapshot leaves the watcher alone", func(t *testing.T) {
		s.send(reloadTick)
		require.NotNil(t, s.last)
		require.NotContains(t, s.header(), "watcher")
	})

	t.Run("a stale one starts it, and the header says so", func(t *testing.T) {
		s.clock = now.Add(list.StaleAfter + time.Second)
		s.send(list.ReloadMsg{})
		require.Contains(t, s.header(), "⚠ snapshot 16s old — starting the watcher again", "from the first frame it is stale")
		s.send(reloadTick)
		require.Contains(t, s.header(), "⚠ snapshot 16s old — starting the watcher again")

		restart := s.last().(tea.BatchMsg)[1]
		s.send(restart())
		require.NotContains(t, s.header(), "old")
		require.Contains(t, s.header(), "started the watcher again")
	})

	t.Run("one that won't start says why", func(t *testing.T) {
		restartErr = errors.New("the watcher did not start; see watcher.log")
		s.clock = s.clock.Add(list.StaleAfter + time.Second)
		s.send(reloadTick)
		restart := s.last().(tea.BatchMsg)[1]
		s.send(restart())
		require.Contains(t, s.header(), "the watcher did not start; see watcher.log")

		s.clock = s.clock.Add(5 * time.Second)
		s.send(list.ReloadMsg{})
		require.Contains(t, s.header(), "⚠ snapshot 21s old — the watcher did not start")
	})
}
