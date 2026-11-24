package list_test

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/golden"
	"github.com/jordanbyron/agent-inbox/internal/list"
	"github.com/jordanbyron/agent-inbox/internal/snapshot"
	"github.com/jordanbyron/agent-inbox/internal/state"
)

var now = time.Unix(1_789_600_000, 0)

func fixture(t *testing.T) snapshot.Snapshot {
	t.Helper()
	var snap snapshot.Snapshot
	_, err := state.ReadSnapshotFile("../snapshot/testdata/fixture.json", &snap)
	require.NoError(t, err)
	return snap
}

type screen struct {
	t       *testing.T
	model   list.Model
	actions *recorder
	clock   time.Time
	last    tea.Cmd
}

func newScreen(t *testing.T, snap snapshot.Snapshot, width, height int) *screen {
	t.Helper()
	return openScreen(t, snap, width, height, false)
}

// newModal is the list as list --modal shows it, 100 columns wide.
func newModal(t *testing.T, height int) *screen {
	t.Helper()
	return openScreen(t, fixture(t), 100, height, true)
}

func openScreen(t *testing.T, snap snapshot.Snapshot, width, height int, modal bool) *screen {
	t.Helper()
	s := &screen{t: t, actions: &recorder{}, clock: now}
	s.model = list.New(list.Config{
		Load:    func() (snapshot.Snapshot, time.Time, error) { return snap, now, nil },
		Actions: s.actions,
		Now:     func() time.Time { return s.clock },
		Home:    "/home/dev",
		Modal:   modal,
	})
	s.send(tea.WindowSizeMsg{Width: width, Height: height})
	return s
}

func (s *screen) send(msgs ...tea.Msg) *screen {
	for _, msg := range msgs {
		next, cmd := s.model.Update(msg)
		s.model, s.last = next.(list.Model), cmd
	}
	return s
}

// press sends each key and runs the command it returns, as the program
// would, so an action a key starts has finished before the next key.
func (s *screen) press(keys ...string) *screen {
	for _, k := range keys {
		s.send(keyPress(k)).finish()
	}
	return s
}

func (s *screen) finish() *screen {
	if s.last == nil {
		return s
	}
	switch msg := s.last().(type) {
	case nil, tea.QuitMsg, tea.BatchMsg:
	default:
		s.send(msg)
	}
	return s
}

func (s *screen) typing(text string) *screen {
	for _, r := range text {
		s.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return s
}

func keyPress(k string) tea.KeyPressMsg {
	special := map[string]rune{
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab,
		"backspace": tea.KeyBackspace, "down": tea.KeyDown, "up": tea.KeyUp,
	}
	if code, ok := special[k]; ok {
		return tea.KeyPressMsg{Code: code}
	}
	if k == "shift+tab" {
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	}
	if rest, ok := strings.CutPrefix(k, "ctrl+"); ok {
		return tea.KeyPressMsg{Code: rune(rest[0]), Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

func (s *screen) frame() string {
	return ansi.Strip(s.model.View().Content) + "\n"
}

func (s *screen) lines() []string {
	return strings.Split(strings.TrimSuffix(s.frame(), "\n"), "\n")
}

func (s *screen) selectedLine() string {
	for _, line := range s.lines()[1:] {
		if strings.HasPrefix(line, " ▶") {
			return line
		}
	}
	return ""
}

func (s *screen) header() string {
	return s.lines()[0]
}

func TestGoldenFrames(t *testing.T) {
	cases := []struct {
		name          string
		width, height int
		drive         func(s *screen)
	}{
		{name: "fixture_100x30", width: 100, height: 30},
		{name: "fixture_50x30", width: 50, height: 30},
		{name: "folded_100x60", width: 100, height: 60},
		{name: "expanded_100x60", width: 100, height: 60, drive: func(s *screen) {
			s.press("G", "z", "o", "shift+tab", "z", "o", "shift+tab", "z", "o")
		}},
		{name: "expanded_50x60", width: 50, height: 60, drive: func(s *screen) {
			s.press("G", "z", "o", "shift+tab", "z", "o", "shift+tab", "z", "o")
		}},
		{name: "end_100x30", width: 100, height: 30, drive: func(s *screen) { s.press("G") }},
		{name: "filtered_100x30", width: 100, height: 30, drive: func(s *screen) { s.press("/").typing("billing") }},
		{name: "filter_kept_100x30", width: 100, height: 30, drive: func(s *screen) { s.press("/").typing("#9").press("enter") }},
		{name: "filter_no_match_100x30", width: 100, height: 30, drive: func(s *screen) { s.press("/").typing("zzz") }},
		{name: "snooze_menu_100x30", width: 100, height: 30, drive: func(s *screen) { s.press("s") }},
		{name: "snooze_prompt_100x30", width: 100, height: 30, drive: func(s *screen) { s.press("s", "5").typing("fri 5pm") }},
		{name: "delete_confirm_100x30", width: 100, height: 30, drive: func(s *screen) { s.press("ctrl+x") }},
		{name: "delete_family_100x30", width: 100, height: 30, drive: func(s *screen) { s.press("j", "j", "ctrl+x") }},
		{name: "alias_prompt_100x30", width: 100, height: 30, drive: func(s *screen) { s.press("a").typing("login") }},
		{name: "link_prompt_100x30", width: 100, height: 30, drive: func(s *screen) { s.press("j", "P") }},
		{name: "pr_picker_100x30", width: 100, height: 30, drive: func(s *screen) {
			s.press("tab", "tab", "j", "j", "j", "j", "o")
		}},
		{name: "snooze_menu_50x30", width: 50, height: 30, drive: func(s *screen) { s.press("s") }},
		{name: "stale_100x30", width: 100, height: 30, drive: func(s *screen) {
			s.clock = now.Add(42 * time.Second)
			s.send(list.ReloadMsg{})
		}},
		{name: "error_status_100x30", width: 100, height: 30, drive: func(s *screen) { s.press("t") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newScreen(t, fixture(t), tc.width, tc.height)
			if tc.name == "error_status_100x30" {
				s.actions.err = errNotWired
			}
			if tc.drive != nil {
				tc.drive(s)
			}
			golden.Require(t, tc.name, s.frame())
		})
	}
}

func TestGoldenModal(t *testing.T) {
	golden.Require(t, "modal_100x30", newModal(t, 30).frame())
}

func TestGoldenEmptyInbox(t *testing.T) {
	empty := snapshot.New("v1", nil, snapshot.SortDefault)
	golden.Require(t, "empty_100x30", newScreen(t, empty, 100, 30).frame())
	golden.Require(t, "empty_50x30", newScreen(t, empty, 50, 30).frame())
}
