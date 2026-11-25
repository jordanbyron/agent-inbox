package watcher

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/jordanbyron/agent-inbox/internal/pulls"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/worktree"
)

// finder finds and refreshes agents' pull requests through git, gh and
// Rex captures. Those calls can take seconds, so it runs on a goroutine of
// its own, which alone uses the Refresher.
type finder struct {
	rex       Rex
	gh        pulls.Runner
	git       worktree.Git
	refresher *pulls.Refresher
	log       *slog.Logger
}

// job is one piece of work for the finder.
type job interface {
	// id is the same for two jobs that would do the same work, so the
	// queue keeps only one.
	id() string
	run(ctx context.Context, f *finder) finding
}

// finding is what a job found, for the loop to merge into the state.
type finding struct {
	// key is the agent the pull requests are for, and empty for a refresh.
	key string
	prs []state.PullRequest
}

// work runs jobs until ctx ends.
func (f *finder) work(ctx context.Context, jobs <-chan job, findings chan<- finding) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-jobs:
			found := f.run(ctx, j)
			select {
			case findings <- found:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (f *finder) run(ctx context.Context, j job) finding {
	ctx, cancel := context.WithTimeout(ctx, findTimeout)
	defer cancel()
	return j.run(ctx, f)
}

// warn logs a failed gh, git or capture call, unless the watcher is
// stopping and the failure is only that.
func (f *finder) warn(ctx context.Context, msg string, args ...any) {
	if !errors.Is(ctx.Err(), context.Canceled) {
		f.log.Warn(msg, args...)
	}
}

// mergeInto adds what was found to the pull requests remembered in st.
func (fd finding) mergeInto(st *state.State) {
	if fd.key != "" {
		if len(fd.prs) > 0 {
			st.PullRequests[fd.key] = pulls.Merge(st.PullRequests[fd.key], fd.prs)
		}
		return
	}
	fresh := map[string]state.PullRequest{}
	for _, pr := range fd.prs {
		fresh[pr.URL] = pr
	}
	for key, prs := range st.PullRequests {
		var update []state.PullRequest
		for _, pr := range prs {
			if f, ok := fresh[pr.URL]; ok {
				update = append(update, f)
			}
		}
		if update != nil {
			st.PullRequests[key] = pulls.Merge(prs, update)
		}
	}
}

// branchJob finds the pull requests of every branch checked out in a
// launched agent's worktree.
type branchJob struct {
	key      string
	worktree string
}

func (j branchJob) id() string { return "branches " + j.key }

func (j branchJob) run(ctx context.Context, f *finder) finding {
	branches, err := worktree.Branches(ctx, f.git, j.worktree)
	if err != nil {
		f.warn(ctx, "reading a worktree's branches", "agent", j.key, "err", err)
		return finding{key: j.key}
	}
	skip := defaultBranches(ctx, f.git, j.worktree)
	branches = slices.DeleteFunc(branches, func(b string) bool { return slices.Contains(skip, b) })
	prs, err := pulls.FindByBranch(ctx, f.gh, j.worktree, branches)
	if err != nil {
		f.warn(ctx, "finding pull requests by branch", "agent", j.key, "err", err)
	}
	return finding{key: j.key, prs: prs}
}

// defaultBranches are the branches whose pull requests are never an
// agent's own: origin's default, or main and master when origin has none.
func defaultBranches(ctx context.Context, git worktree.Git, dir string) []string {
	if ref, err := git.Run(ctx, dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return []string{strings.TrimPrefix(ref, "origin/")}
	}
	return []string{"main", "master"}
}

// captureJob reads the pull request links on an agent's screen.
type captureJob struct {
	key       string
	sessionID string
	blockID   string
}

func (j captureJob) id() string { return "capture " + j.key }

func (j captureJob) run(ctx context.Context, f *finder) finding {
	text, err := f.rex.Capture(ctx, j.sessionID, j.blockID)
	if err != nil {
		f.warn(ctx, "capturing a block", "agent", j.key, "err", err)
		return finding{key: j.key}
	}
	return finding{key: j.key, prs: pulls.FindInText(text)}
}

// refreshJob asks gh about the remembered pull requests that are due.
type refreshJob struct {
	prs []state.PullRequest
	now time.Time
}

func (refreshJob) id() string { return "refresh" }

func (j refreshJob) run(ctx context.Context, f *finder) finding {
	prs, err := f.refresher.Refresh(ctx, j.prs, j.now)
	if err != nil {
		f.warn(ctx, "refreshing pull requests", "err", err)
	}
	return finding{prs: prs}
}
