package form

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// completeDir reports false when there is nothing to complete, so that Tab
// moves on to the next field instead.
func (m *Model) completeDir() bool {
	if m.focus != fieldDir {
		return false
	}
	typed := strings.TrimSpace(m.dir.String())
	path := m.directory()
	listing := typed == "" || strings.HasSuffix(typed, "/")
	if !listing && isDir(path) {
		return false
	}
	parent, prefix := path, ""
	if !listing {
		parent, prefix = filepath.Dir(path), filepath.Base(path)
	}
	matches := subdirs(parent, prefix)
	switch len(matches) {
	case 0:
		return false
	case 1:
		m.dir.Replace(shortenHome(inDir(parent, matches[0]), m.opts.Home) + "/")
	default:
		m.candidates = matches
		m.dir.Replace(shortenHome(inDir(parent, commonPrefix(matches)), m.opts.Home))
	}
	return true
}

// subdirs follows symlinks, and leaves out hidden directories unless the
// prefix asks for them, as a shell's completion does.
func subdirs(parent, prefix string) []string {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || (strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".")) {
			continue
		}
		if isDir(filepath.Join(parent, name)) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// inDir keeps the slash after dir that filepath.Join would drop with an
// empty name, so listing a directory leaves the field as it was.
func inDir(dir, name string) string {
	return strings.TrimSuffix(dir, "/") + "/" + name
}

func commonPrefix(names []string) string {
	prefix := names[0]
	for _, name := range names[1:] {
		for !strings.HasPrefix(name, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}
