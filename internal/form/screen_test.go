package form_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/dirpicker"
	"github.com/jordanbyron/agent-inbox/internal/form"
	"github.com/jordanbyron/agent-inbox/internal/recipe"
)

var now = time.Date(2026, 11, 23, 9, 30, 0, 0, time.UTC)

type fakeClipboard struct {
	image bool
	text  string
}

func (c *fakeClipboard) SaveImage(path string) (bool, error) {
	if !c.image {
		return false, nil
	}
	return true, os.WriteFile(path, []byte("png"), 0o600)
}

func (c *fakeClipboard) Text() (string, error) {
	return c.text, nil
}

// screen drives a form in a home directory of its own, so nothing reads
// the real one. Its starting directory, ~/code/app, is a git repository.
type screen struct {
	t     *testing.T
	home  string
	dir   string
	clip  *fakeClipboard
	repos map[string]bool
	opts  form.Options
	model form.Model
	quit  bool
}

// homeDirs browses the screen's home directory, with its repositories
// those the screen says are.
type homeDirs struct {
	dirpicker.OS
	s *screen
}

func (d homeDirs) IsRepo(dir string) bool {
	return d.s.repos[dir]
}

func newScreen(t *testing.T, width, height int, configure ...func(s *screen)) *screen {
	t.Helper()
	home := t.TempDir()
	s := &screen{t: t, home: home, dir: filepath.Join(home, "code", "app"), clip: &fakeClipboard{}}
	s.mkdir("code/app", "code/site", "notes")
	s.repos = map[string]bool{s.dir: true, filepath.Join(home, "code", "site"): true}
	s.opts = form.Options{
		Recipes:         recipe.Builtins(),
		Sessions:        []form.Session{{ID: "s1", Label: "work"}, {ID: "s2", Label: "side projects"}},
		CurrentSession:  "s1",
		Dir:             s.dir,
		WorktreeDefault: true,
		InRepo:          func(dir string) bool { return s.repos[dir] },
		Home:            home,
		Now:             func() time.Time { return now },
		Clipboard:       s.clip,
		ImageDir:        filepath.Join(home, "images"),
		DirFS:           homeDirs{s: s},
	}
	for _, c := range configure {
		c(s)
	}
	s.model = form.New(s.opts)
	s.send(tea.WindowSizeMsg{Width: width, Height: height})
	return s
}

func (s *screen) mkdir(dirs ...string) {
	for _, dir := range dirs {
		require.NoError(s.t, os.MkdirAll(filepath.Join(s.home, dir), 0o755))
	}
}

func (s *screen) write(path, body string) {
	full := filepath.Join(s.home, path)
	require.NoError(s.t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(s.t, os.WriteFile(full, []byte(body), 0o600))
}

func (s *screen) send(msgs ...tea.Msg) *screen {
	for _, msg := range msgs {
		next, cmd := s.model.Update(msg)
		s.model = next.(form.Model)
		s.run(cmd)
	}
	return s
}

// run does at once what Bubble Tea does with a command in the background.
func (s *screen) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.QuitMsg:
		s.quit = true
	case tea.BatchMsg:
		for _, c := range msg {
			s.run(c)
		}
	default:
		s.send(msg)
	}
}

func (s *screen) press(keys ...string) *screen {
	for _, k := range keys {
		s.send(keyPress(k))
	}
	return s
}

func (s *screen) typing(text string) *screen {
	for _, r := range text {
		if r == ' ' {
			s.send(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
			continue
		}
		s.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return s
}

func (s *screen) paste(text string) *screen {
	return s.send(tea.PasteMsg{Content: text})
}

func keyPress(k string) tea.KeyPressMsg {
	special := map[string]rune{
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "backspace": tea.KeyBackspace,
		"down": tea.KeyDown, "up": tea.KeyUp, "left": tea.KeyLeft, "right": tea.KeyRight,
		"home": tea.KeyHome, "end": tea.KeyEnd,
	}
	if code, ok := special[k]; ok {
		return tea.KeyPressMsg{Code: code}
	}
	if k == "shift+tab" {
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	}
	if k == "space" {
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	if rest, ok := strings.CutPrefix(k, "ctrl+"); ok {
		return tea.KeyPressMsg{Code: rune(rest[0]), Mod: tea.ModCtrl}
	}
	if rest, ok := strings.CutPrefix(k, "alt+"); ok {
		return tea.KeyPressMsg{Code: rune(rest[0]), Mod: tea.ModAlt}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

func (s *screen) frame() string {
	return ansi.Strip(s.model.View().Content) + "\n"
}

func (s *screen) lines() []string {
	return strings.Split(strings.TrimSuffix(s.frame(), "\n"), "\n")
}

func (s *screen) lineWith(text string) string {
	for _, l := range s.lines() {
		if strings.Contains(l, text) {
			return l
		}
	}
	return ""
}

func (s *screen) footer() string {
	lines := s.lines()
	return lines[len(lines)-1]
}

func (s *screen) focusedLabel() string {
	for _, l := range s.lines() {
		if _, rest, ok := strings.Cut(l, "▶"); ok {
			return strings.Fields(strings.TrimLeft(rest, " ["))[0]
		}
	}
	return ""
}

func (s *screen) result() form.Result {
	s.t.Helper()
	r, ok := s.model.Submitted()
	require.True(s.t, ok, "the form was not submitted")
	return r
}
