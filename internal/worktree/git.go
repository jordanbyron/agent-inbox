package worktree

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Git runs git commands.
type Git interface {
	// Run runs git with args in dir and returns its output, trimmed.
	Run(ctx context.Context, dir string, args ...string) (string, error)
}

// ExecGit runs the git on PATH.
type ExecGit struct{}

// Run runs git with args in dir. Its error carries git's stderr.
func (ExecGit) Run(ctx context.Context, dir string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// A fetch must fail rather than wait on a credentials prompt no one sees.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
