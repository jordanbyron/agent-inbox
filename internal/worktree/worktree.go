// Package worktree makes and removes the git worktrees agent-inbox owns,
// and reads which branches an agent has had checked out in one.
package worktree

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jordanbyron/agent-inbox/internal/config"
)

// Worktree is a worktree agent-inbox made for an agent, and its branch.
type Worktree struct {
	Path   string
	Branch string
}

// maxSuffix bounds the search for a free name, so a template that renders
// the same path for every name fails instead of looping.
const maxSuffix = 100

// Create makes a worktree for the agent called name, in the repository
// that holds dir, as cfg says. A taken branch or path gets -2, -3 and so
// on. On failure it leaves nothing behind.
func Create(ctx context.Context, git Git, dir, name string, cfg config.Worktree) (Worktree, error) {
	if name == "" {
		return Worktree{}, errors.New("creating a worktree: the agent has no name")
	}
	repo, err := findRepo(ctx, git, dir)
	if err != nil {
		return Worktree{}, fmt.Errorf("creating a worktree: %w", err)
	}
	start, err := startPoint(ctx, git, dir, repo, cfg.Base)
	if err != nil {
		return Worktree{}, fmt.Errorf("creating a worktree: %w", err)
	}
	wt, err := freeWorktree(ctx, git, repo, name, cfg)
	if err != nil {
		return Worktree{}, fmt.Errorf("creating a worktree: %w", err)
	}
	// --no-track, or a branch made from origin/main would push to main.
	_, err = git.Run(ctx, repo.root, "worktree", "add", "--no-track", "-b", wt.Branch, wt.Path, start)
	if err == nil {
		err = exclude(repo, wt.Path)
	}
	if err != nil {
		return Worktree{}, fmt.Errorf("creating a worktree: %w", errors.Join(err, undoCreate(ctx, git, repo, wt)))
	}
	return wt, nil
}

// undoCreate takes away whatever a failed Create made. A `worktree add`
// that fails during checkout has already made the branch, and the path
// was free before, so all of it is ours to delete.
func undoCreate(ctx context.Context, git Git, repo repository, wt Worktree) error {
	errs := []error{os.RemoveAll(wt.Path)}
	_, err := git.Run(ctx, repo.root, "worktree", "prune")
	errs = append(errs, err)
	if taken, _ := git.Run(ctx, repo.root, "branch", "--list", wt.Branch); taken != "" {
		_, err := git.Run(ctx, repo.root, "branch", "-D", wt.Branch)
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// Remove removes the worktree at path and deletes branch. It refuses as
// Removable does.
func Remove(ctx context.Context, git Git, path, branch string) error {
	if err := Removable(ctx, git, path, branch); err != nil {
		return err
	}
	repo, err := findRepo(ctx, git, path)
	if err != nil {
		return fmt.Errorf("removing worktree %s: %w", path, err)
	}
	if _, err := git.Run(ctx, repo.root, "worktree", "remove", path); err != nil {
		return fmt.Errorf("removing worktree %s: %w", path, err)
	}
	if branch == "" {
		return nil
	}
	if _, err := git.Run(ctx, repo.root, "branch", "-D", branch); err != nil {
		return fmt.Errorf("deleting branch %s: %w", branch, err)
	}
	return nil
}

// Removable reports, naming the worktree, why Remove would refuse it: it has
// uncommitted changes or commits that no other branch, local or remote,
// holds. It returns nil when Remove would go ahead.
func Removable(ctx context.Context, git Git, path, branch string) error {
	status, err := git.Run(ctx, path, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("checking worktree %s: %w", path, err)
	}
	if status != "" {
		return fmt.Errorf("worktree %s has uncommitted changes", path)
	}
	unpushed, err := commitsOnlyHere(ctx, git, path, branch)
	if err != nil {
		return fmt.Errorf("checking worktree %s: %w", path, err)
	}
	if unpushed {
		return fmt.Errorf("worktree %s has commits that are on no other branch", path)
	}
	return nil
}

// Branches returns every branch that has been checked out in the worktree
// at path, most recent first, read from the worktree's own HEAD reflog.
func Branches(ctx context.Context, git Git, path string) ([]string, error) {
	current, err := git.Run(ctx, path, "branch", "--show-current")
	if err != nil {
		return nil, fmt.Errorf("reading the branches of %s: %w", path, err)
	}
	log, err := git.Run(ctx, path, "reflog", "show", "--format=%gs", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("reading the branches of %s: %w", path, err)
	}
	var branches []string
	add := func(name string) {
		if isBranchName(name) && !slices.Contains(branches, name) {
			branches = append(branches, name)
		}
	}
	add(current)
	for line := range strings.Lines(log) {
		move, ok := strings.CutPrefix(strings.TrimSpace(line), "checkout: moving from ")
		if !ok {
			continue
		}
		from, to, _ := strings.Cut(move, " to ")
		add(to)
		add(from)
	}
	return branches, nil
}

// InRepo reports whether dir is inside a git repository's working tree.
func InRepo(ctx context.Context, git Git, dir string) bool {
	out, err := git.Run(ctx, dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

type repository struct {
	root   string
	gitDir string
}

// findRepo returns the main checkout of the repository holding dir, even
// when dir is in one of its worktrees, so an agent started from another
// agent's worktree gets a worktree beside it rather than inside it.
func findRepo(ctx context.Context, git Git, dir string) (repository, error) {
	gitDir, err := git.Run(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return repository{}, fmt.Errorf("finding the repository of %s: %w", dir, err)
	}
	if filepath.Base(gitDir) == ".git" {
		return repository{root: filepath.Dir(gitDir), gitDir: gitDir}, nil
	}
	root, err := git.Run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return repository{}, fmt.Errorf("finding the repository of %s: %w", dir, err)
	}
	return repository{root: root, gitDir: gitDir}, nil
}

func startPoint(ctx context.Context, git Git, dir string, repo repository, base string) (string, error) {
	if base == config.BaseFresh {
		remotes, err := git.Run(ctx, repo.root, "remote")
		if err != nil {
			return "", err
		}
		if slices.Contains(strings.Fields(remotes), "origin") {
			return originDefault(ctx, git, repo)
		}
	}
	head, err := git.Run(ctx, dir, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", fmt.Errorf("%s has no commit to branch from: %w", dir, err)
	}
	return head, nil
}

// originDefault fetches origin and returns its default branch. A clone
// knows it as origin/HEAD; a remote added by hand has to be asked.
func originDefault(ctx context.Context, git Git, repo repository) (string, error) {
	if _, err := git.Run(ctx, repo.root, "fetch", "--quiet", "origin"); err != nil {
		return "", err
	}
	if ref, err := git.Run(ctx, repo.root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return ref, nil
	}
	out, err := git.Run(ctx, repo.root, "ls-remote", "--symref", "origin", "HEAD")
	if err != nil {
		return "", err
	}
	for line := range strings.Lines(out) {
		ref, ok := strings.CutPrefix(line, "ref: refs/heads/")
		if ok {
			branch, _, _ := strings.Cut(ref, "\t")
			return "origin/" + branch, nil
		}
	}
	return "", errors.New("origin has no default branch")
}

func freeWorktree(ctx context.Context, git Git, repo repository, name string, cfg config.Worktree) (Worktree, error) {
	for n := 1; n <= maxSuffix; n++ {
		candidate := name
		if n > 1 {
			candidate = fmt.Sprintf("%s-%d", name, n)
		}
		wt, err := render(repo, candidate, cfg)
		if err != nil {
			return Worktree{}, err
		}
		if _, err := git.Run(ctx, repo.root, "check-ref-format", "--branch", wt.Branch); err != nil {
			return Worktree{}, fmt.Errorf("%q is not a branch name: %w", wt.Branch, err)
		}
		taken, err := git.Run(ctx, repo.root, "branch", "--list", wt.Branch)
		if err != nil {
			return Worktree{}, err
		}
		if _, err := os.Lstat(wt.Path); taken == "" && errors.Is(err, fs.ErrNotExist) {
			return wt, nil
		}
	}
	return Worktree{}, fmt.Errorf("no free worktree name for %q after %d tries", name, maxSuffix)
}

func render(repo repository, name string, cfg config.Worktree) (Worktree, error) {
	path := strings.NewReplacer("{repo}", repo.root, "{name}", name).Replace(cfg.Path)
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return Worktree{}, err
		}
		path = filepath.Join(home, rest)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(repo.root, path)
	}
	return Worktree{
		Path:   filepath.Clean(path),
		Branch: strings.ReplaceAll(cfg.Branch, "{name}", name),
	}, nil
}

// exclude keeps a worktree inside the repository out of git status. It
// excludes the top-level directory holding it, so .worktrees/ is written
// once however many agents there are.
func exclude(repo repository, path string) error {
	rel, err := filepath.Rel(repo.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}
	top, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
	pattern := "/" + top + "/"
	file := filepath.Join(repo.gitDir, "info", "exclude")
	existing, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", file, err)
	}
	for line := range strings.Lines(string(existing)) {
		if strings.TrimSpace(line) == pattern {
			return nil
		}
	}
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		pattern = "\n" + pattern
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return fmt.Errorf("writing %s: %w", file, err)
	}
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("writing %s: %w", file, err)
	}
	if _, err := f.WriteString(pattern + "\n"); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing %s: %w", file, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", file, err)
	}
	return nil
}

// commitsOnlyHere reports whether the worktree's HEAD has commits that no
// branch holds besides its own. A repository with no remote can still be
// cleaned up once its work is merged locally.
func commitsOnlyHere(ctx context.Context, git Git, path, branch string) (bool, error) {
	current, err := git.Run(ctx, path, "branch", "--show-current")
	if err != nil {
		return false, err
	}
	args := []string{"rev-list", "--max-count=1", "HEAD", "--not"}
	for _, own := range []string{branch, current} {
		if own != "" {
			args = append(args, "--exclude="+own)
		}
	}
	out, err := git.Run(ctx, path, append(args, "--branches", "--remotes")...)
	if err != nil {
		return false, err
	}
	return out != "", nil
}

func isBranchName(name string) bool {
	return name != "" && name != "HEAD" && !isCommitID(name)
}

func isCommitID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	return strings.Trim(s, "0123456789abcdef") == ""
}
