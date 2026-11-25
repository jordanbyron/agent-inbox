package worktree

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
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
	cmd.Env = append(withoutRepoEnv(os.Environ()), "GIT_TERMINAL_PROMPT=0")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// withoutRepoEnv drops the variables that point git at a repository. Under a
// git hook or `git rebase -x` they name the caller's repository, and every
// command here means the one in its own directory.
func withoutRepoEnv(env []string) []string {
	return slices.DeleteFunc(slices.Clone(env), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return slices.Contains(repoEnv, name)
	})
}

// The list `git rev-parse --local-env-vars` prints.
var repoEnv = []string{
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_CONFIG", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT",
	"GIT_OBJECT_DIRECTORY", "GIT_DIR", "GIT_WORK_TREE", "GIT_IMPLICIT_WORK_TREE", "GIT_GRAFT_FILE",
	"GIT_INDEX_FILE", "GIT_NO_REPLACE_OBJECTS", "GIT_REPLACE_REF_BASE", "GIT_PREFIX", "GIT_SHALLOW_FILE",
	"GIT_COMMON_DIR",
}
