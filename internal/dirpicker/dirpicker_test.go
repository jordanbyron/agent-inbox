package dirpicker_test

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/dirpicker"
	"github.com/jordanbyron/agent-inbox/internal/editor"
)

const home = "/home"

type tree struct {
	dirs   map[string][]string
	repos  map[string]bool
	denied map[string]bool
}

func (t tree) Subdirs(dir string) ([]string, error) {
	if t.denied[dir] {
		return nil, fmt.Errorf("listing %s: %w", dir, &fs.PathError{Op: "open", Path: dir, Err: fs.ErrPermission})
	}
	names, ok := t.dirs[dir]
	if !ok {
		return nil, fmt.Errorf("listing %s: %w", dir, fs.ErrNotExist)
	}
	return names, nil
}

func (t tree) IsRepo(dir string) bool {
	return t.repos[dir]
}

func newTree() tree {
	t := tree{
		dirs: map[string][]string{
			"/":               {"home"},
			"/home":           {"code", "notes", "secret", ".config"},
			"/home/.config":   {},
			"/home/notes":     {},
			"/home/code":      {"zapp", "bap", "Apricot", "app", ".cache"},
			"/home/code/app":  {"src"},
			"/home/code/zapp": {},
		},
		repos:  map[string]bool{"/home/code/app": true},
		denied: map[string]bool{"/home/secret": true},
	}
	for _, dir := range []string{"bap", "Apricot", ".cache", "app/src"} {
		t.dirs["/home/code/"+dir] = []string{}
	}
	return t
}

// driver runs each read at once, where Bubble Tea would run it later.
type driver struct {
	t *testing.T
	p dirpicker.Picker
}

func open(t *testing.T, start string, recents ...string) *driver {
	t.Helper()
	d := &driver{t: t, p: dirpicker.New(start, home, recents, newTree())}
	d.sync()
	return d
}

func (d *driver) sync() {
	d.run(d.p.Sync(true))
}

func (d *driver) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			d.run(c)
		}
		return
	}
	d.p.Update(cmd())
}

func (d *driver) press(keys ...string) bool {
	handled := true
	for _, k := range keys {
		text := ""
		if len([]rune(k)) == 1 {
			text = k
		}
		handled = d.p.Press(k, text)
		d.sync()
	}
	return handled
}

func (d *driver) typing(text string) *driver {
	for _, r := range text {
		d.press(string(r))
	}
	return d
}

func (d *driver) text() string {
	return strings.TrimSpace(ansi.Strip(d.p.Row(60, editor.Paint{})))
}

// rows folds the padding, so "▶ app/   git" reads "▶ app/ git".
func (d *driver) rows() []string {
	var out []string
	for _, line := range d.p.Lines(60, 20) {
		out = append(out, strings.Join(strings.Fields(ansi.Strip(line)), " "))
	}
	return out
}

func TestRows(t *testing.T) {
	many := []string{"/home/code/zapp", "/home/code/bap", "/home/code/Apricot", "/home/code/app", "/home/notes", "/home/code/app/src"}
	cases := []struct {
		name    string
		start   string
		recents []string
		typed   string
		want    []string
	}{
		{
			name: "browsing lists .. then the directories sorted, case aside, without hidden ones", start: "/home/code",
			want: []string{"..", "app/ git", "Apricot/", "bap/", "zapp/"},
		},
		{
			name: "a fragment puts prefix matches before substring ones and highlights the first", start: "/home/code", typed: "/ap",
			want: []string{"› app/ git", "Apricot/", "bap/", "zapp/"},
		},
		{
			name: "a fragment matches whatever its case", start: "/home/code", typed: "/APR",
			want: []string{"› Apricot/"},
		},
		{
			name: "a fragment starting with a dot shows hidden directories", start: "/home/code", typed: "/.",
			want: []string{"› .cache/"},
		},
		{
			name: "the root has no ..", start: "/",
			want: []string{"home/"},
		},
		{
			name: "a directory that cannot be read says so", start: "/home/secret",
			want: []string{"can't read ~/secret: permission denied"},
		},
		{
			name: "a path that does not exist filters its deepest existing directory", start: "/home/code", typed: "/zz/y",
			want: []string{"nothing in ~/code matches zz"},
		},
		{
			name: "recents come first under a heading, skipping missing ones", start: "/home/code",
			recents: []string{"/home/notes", "/gone", "/home/code/app"},
			want:    []string{"recent", "~/notes", "~/code/app git", "..", "app/ git", "Apricot/", "bap/", "zapp/"},
		},
		{
			name: "recents stop at five", start: "/home/notes", recents: many,
			want: []string{"recent", "~/code/zapp", "~/code/bap", "~/code/Apricot", "~/code/app git", "~/notes", ".."},
		},
		{
			name: "recents go once the text changes", start: "/home/code", recents: []string{"/home/notes"}, typed: "/",
			want: []string{"..", "app/ git", "Apricot/", "bap/", "zapp/"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := open(t, tc.start, tc.recents...).typing(tc.typed)
			require.Equal(t, tc.want, d.rows())
		})
	}
}

func TestBrowsing(t *testing.T) {
	t.Run("Enter descends into a row and highlights its first directory", func(t *testing.T) {
		d := open(t, "/home/code")
		d.press("down", "down", "enter")
		require.Equal(t, "~/code/app/", d.text())
		require.Equal(t, []string{"..", "▶ src/"}, d.rows())
		require.Equal(t, "/home/code/app", d.p.Dir())
	})

	t.Run("Right descends as Enter does", func(t *testing.T) {
		d := open(t, "/home/code")
		d.press("down", "down", "down", "right")
		require.Equal(t, "~/code/Apricot/", d.text())
	})

	t.Run("Left goes to the parent and highlights the directory it left", func(t *testing.T) {
		d := open(t, "/home/code/app")
		d.press("down", "left")
		require.Equal(t, "~/code/", d.text())
		require.Contains(t, d.rows(), "▶ app/ git")
	})

	t.Run("Enter on .. goes to the parent", func(t *testing.T) {
		d := open(t, "/home/code")
		d.press("down", "enter")
		require.Equal(t, "~/", d.text())
		require.Contains(t, d.rows(), "▶ code/")
	})

	t.Run("Backspace after a slash goes to the parent", func(t *testing.T) {
		d := open(t, "/home/code/app").typing("/")
		d.press("backspace")
		require.Equal(t, "~/code/", d.text())
	})

	t.Run("Backspace in a fragment deletes as usual", func(t *testing.T) {
		d := open(t, "/home/code").typing("/ap")
		d.press("backspace")
		require.Equal(t, "~/code/a", d.text())
	})

	t.Run("Down and Up move between the text and the list", func(t *testing.T) {
		d := open(t, "/home/code")
		d.press("down")
		require.True(t, d.p.InList())
		require.Equal(t, "▶ ..", d.rows()[0])
		d.press("down", "up", "up")
		require.False(t, d.p.InList())
		require.False(t, d.press("up"), "Up from the text is the form's")
	})

	t.Run("typing in the list goes back to the text", func(t *testing.T) {
		d := open(t, "/home/code")
		d.press("down", "/", "b")
		require.False(t, d.p.InList())
		require.Equal(t, "~/code/b", d.text())
	})
}

func TestTab(t *testing.T) {
	t.Run("completes a fragment to the highlighted row", func(t *testing.T) {
		d := open(t, "/home/code").typing("/apr")
		require.True(t, d.p.Completes())
		require.True(t, d.press("tab"))
		require.Equal(t, "~/code/Apricot/", d.text())
	})

	t.Run("moves on with no fragment and nothing highlighted", func(t *testing.T) {
		d := open(t, "/home/code")
		require.False(t, d.p.Completes())
		require.False(t, d.press("tab"))
	})

	t.Run("takes the highlighted row from the list", func(t *testing.T) {
		d := open(t, "/home/code")
		require.True(t, d.press("down", "down", "tab"))
		require.Equal(t, "~/code/app/", d.text())
		require.False(t, d.p.InList())
		require.False(t, d.press("tab"))
	})
}

func TestCtrlU(t *testing.T) {
	t.Run("jumps to the recents", func(t *testing.T) {
		d := open(t, "/home/code", "/home/notes")
		d.press("down", "down", "ctrl+u")
		require.Equal(t, "▶ ~/notes", d.rows()[1])
		require.False(t, d.press("enter"), "Enter on a recent is the form's, to move on")
		require.Equal(t, "~/notes/", d.text())
		require.False(t, d.p.InList())
	})

	t.Run("Right browses into a recent", func(t *testing.T) {
		d := open(t, "/home/code", "/home/notes")
		d.press("ctrl+u", "right")
		require.Equal(t, "~/notes/", d.text())
		require.True(t, d.p.InList())
	})

	t.Run("clears the text with no recents to show", func(t *testing.T) {
		d := open(t, "/home/code")
		d.press("ctrl+u")
		require.Empty(t, d.text())
	})
}

func TestEscape(t *testing.T) {
	d := open(t, "/home/code")
	d.press("down")
	require.True(t, d.press("esc"))
	require.False(t, d.p.InList())
	require.False(t, d.press("esc"), "Esc from the text is the form's")
}

func TestTypingAWholePath(t *testing.T) {
	cases := []struct {
		name  string
		typed string
		want  string
	}{
		{"// starts over at the root", "//", "/"},
		{"/~ starts over at home", "/~", "~"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, open(t, "/home/code").typing(tc.typed).text())
		})
	}

	t.Run("a pasted path replaces the text", func(t *testing.T) {
		d := open(t, "/home/code")
		d.p.Paste("/home/notes\n")
		require.Equal(t, "/home/notes", d.text())
	})

	t.Run("a pasted name is typed in", func(t *testing.T) {
		d := open(t, "/home/code").typing("/")
		d.p.Paste("app")
		require.Equal(t, "/home/code/app", d.p.Dir())
	})
}

func TestARelativePathIsFromTheStart(t *testing.T) {
	d := open(t, "/home/code")
	d.press("ctrl+u")
	d.typing("app/src")
	require.Equal(t, filepath.Join("/home/code", "app", "src"), d.p.Dir())
}

func TestAStaleReadIsIgnored(t *testing.T) {
	d := open(t, "/home/code")
	for _, k := range []string{"/", "a", "p", "p"} {
		d.p.Press(k, k)
	}
	stale := d.p.Sync(true)
	d.press("backspace", "backspace", "backspace")
	require.Equal(t, "~/code/", d.text())
	d.run(stale)
	require.Equal(t, []string{"..", "app/ git", "Apricot/", "bap/", "zapp/"}, d.rows())
}
