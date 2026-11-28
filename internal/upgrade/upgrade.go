// Package upgrade notices a different agent-inbox installed over the
// running one, as `brew upgrade` or `go install` leaves it.
package upgrade

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Asker reports the version of the binary at path.
type Asker func(path string) (string, error)

// AskBinary runs the binary at path with `version`.
func AskBinary(path string) (string, error) {
	// Cobra's Println writes to stderr, so the version may be on either.
	out, err := exec.Command(path, "version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("asking %s for its version: %w", path, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Installed is the binary at a path, which starts out as the running one.
type Installed struct {
	path    string
	ask     Asker
	seen    os.FileInfo
	version string
}

// New watches the binary at path, which is version running for now.
func New(path, running string, ask Asker) *Installed {
	seen, _ := os.Stat(path)
	return &Installed{path: path, ask: ask, seen: seen, version: running}
}

// Version is the version of the binary now at the path, asking it only
// once the file changes.
func (i *Installed) Version() (string, error) {
	now, err := os.Stat(i.path)
	if err != nil {
		return "", fmt.Errorf("checking %s: %w", i.path, err)
	}
	if unchanged(i.seen, now) {
		return i.version, nil
	}
	version, err := i.ask(i.path)
	if err != nil {
		return "", err
	}
	i.seen, i.version = now, version
	return version, nil
}

// Homebrew points its symlink at a new file; go install writes over the old one.
func unchanged(a, b os.FileInfo) bool {
	return a != nil && os.SameFile(a, b) && a.ModTime().Equal(b.ModTime()) && a.Size() == b.Size()
}
