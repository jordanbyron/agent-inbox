package dirpicker

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// FS is the filesystem as the picker browses it.
type FS interface {
	// Subdirs names the directories in dir, following symlinks. An error
	// wrapping fs.ErrNotExist means dir is not a directory.
	Subdirs(dir string) ([]string, error)
	// IsRepo reports whether dir is the root of a git repository.
	IsRepo(dir string) bool
}

// OS is the real filesystem.
type OS struct{}

// Subdirs names the directories in dir.
func (OS) Subdirs(dir string) ([]string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("listing %s: %w", dir, fs.ErrNotExist)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if info, err := os.Stat(filepath.Join(dir, e.Name())); err == nil && info.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// IsRepo reports whether dir holds a .git, which is a file in a worktree.
func (OS) IsRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

type entry struct {
	name string
	path string
	repo bool
}

type listed struct {
	path    string
	dir     string
	entries []entry
	err     error
}

type recentsChecked []entry

func read(dirs FS, path string) tea.Cmd {
	return func() tea.Msg {
		dir := path
		for {
			names, err := dirs.Subdirs(dir)
			if err == nil {
				return listed{path: path, dir: dir, entries: entries(dirs, dir, names)}
			}
			if !errors.Is(err, fs.ErrNotExist) || dir == filepath.Dir(dir) {
				return listed{path: path, dir: dir, err: err}
			}
			dir = filepath.Dir(dir)
		}
	}
}

func entries(dirs FS, dir string, names []string) []entry {
	out := make([]entry, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		out = append(out, entry{name: name, path: path, repo: dirs.IsRepo(path)})
	}
	slices.SortFunc(out, func(a, b entry) int {
		if c := strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name)); c != 0 {
			return c
		}
		return strings.Compare(a.name, b.name)
	})
	return out
}

func checkRecents(dirs FS, recents []string, home string) tea.Cmd {
	return func() tea.Msg {
		var out recentsChecked
		seen := map[string]bool{}
		for _, dir := range recents {
			dir = filepath.Clean(dir)
			if len(out) == maxRecents || seen[dir] {
				continue
			}
			if _, err := dirs.Subdirs(dir); err != nil {
				continue
			}
			seen[dir] = true
			out = append(out, entry{name: shortenHome(dir, home), path: dir, repo: dirs.IsRepo(dir)})
		}
		return out
	}
}

// match hides dot directories unless the fragment starts with a dot, as a
// shell's completion does.
func match(all []entry, fragment string) []entry {
	want := strings.ToLower(fragment)
	var prefix, inside []entry
	for _, e := range all {
		name := strings.ToLower(e.name)
		switch {
		case strings.HasPrefix(e.name, ".") && !strings.HasPrefix(fragment, "."):
		case strings.HasPrefix(name, want):
			prefix = append(prefix, e)
		case strings.Contains(name, want):
			inside = append(inside, e)
		}
	}
	return append(prefix, inside...)
}

func expandPath(typed, base, home string) string {
	switch {
	case typed == "":
		return filepath.Clean(base)
	case typed == "~":
		return filepath.Clean(home)
	case strings.HasPrefix(typed, "~/"):
		return filepath.Join(home, typed[2:])
	case filepath.IsAbs(typed):
		return filepath.Clean(typed)
	default:
		return filepath.Join(base, typed)
	}
}

func shortenHome(dir, home string) string {
	if home != "" && (dir == home || strings.HasPrefix(dir, home+"/")) {
		return "~" + strings.TrimPrefix(dir, home)
	}
	return dir
}

// The trailing slash is what lets Backspace go to the parent.
func asTyped(dir, home string) string {
	short := shortenHome(dir, home)
	if strings.HasSuffix(short, "/") {
		return short
	}
	return short + "/"
}
