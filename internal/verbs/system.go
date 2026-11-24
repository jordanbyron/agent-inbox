package verbs

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// System runs the desktop's own programs: the URL opener and the clipboard.
type System interface {
	// Run runs a program with stdin as its input.
	Run(ctx context.Context, stdin, name string, args ...string) error
	// Has reports whether a program is on PATH.
	Has(name string) bool
}

// ExecSystem runs the programs on PATH.
type ExecSystem struct{}

// Run runs name with args, feeding it stdin. Its error carries stderr.
func (ExecSystem) Run(ctx context.Context, stdin, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("running %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Has reports whether name is on PATH.
func (ExecSystem) Has(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

var clipboards = [][]string{
	{"pbcopy"},
	{"wl-copy"},
	{"xclip", "-selection", "clipboard"},
}

// OpenPullRequest opens url in the browser.
func (in *Inbox) OpenPullRequest(ctx context.Context, url string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return in.System.Run(ctx, "", opener, url)
}

// CopyPullRequest puts url on the clipboard with whichever of pbcopy,
// wl-copy and xclip is installed.
func (in *Inbox) CopyPullRequest(ctx context.Context, url string) error {
	for _, argv := range clipboards {
		if in.System.Has(argv[0]) {
			return in.System.Run(ctx, url, argv[0], argv[1:]...)
		}
	}
	return errors.New("no clipboard program: install pbcopy, wl-copy or xclip")
}
