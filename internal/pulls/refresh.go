package pulls

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jordanbyron/agent-inbox/internal/state"
)

// RefreshAfter is how long a pull request goes before gh is asked about it
// again.
const RefreshAfter = 60 * time.Second

// ErrNotFound is what Refresh reports for a pull request gh could not
// describe.
var ErrNotFound = errors.New("gh did not describe the pull request")

// Refresher asks gh about remembered pull requests at most once per
// RefreshAfter each. It is not safe for concurrent use.
type Refresher struct {
	gh        Runner
	checkedAt map[string]time.Time
}

// NewRefresher returns a Refresher that has asked gh about nothing yet.
func NewRefresher(gh Runner) *Refresher {
	return &Refresher{gh: gh, checkedAt: map[string]time.Time{}}
}

// Due returns the pull requests that want asking about at now: those not
// asked about within RefreshAfter, except one gh has said resolved and when,
// since nothing more will happen to it.
func (r *Refresher) Due(prs []state.PullRequest, now time.Time) []state.PullRequest {
	var due []state.PullRequest
	for _, pr := range prs {
		if pr.ResolvedAt.IsZero() && now.Sub(r.checkedAt[pr.URL]) >= RefreshAfter {
			due = append(due, pr)
		}
	}
	return due
}

// Refresh returns prs, in the same order, with the due ones as gh now
// describes them, asking once per repository. One gh could not describe is
// returned as it was, and the reasons come back as the error.
func (r *Refresher) Refresh(ctx context.Context, prs []state.PullRequest, now time.Time) ([]state.PullRequest, error) {
	var repos []string
	numbers := map[string][]int{}
	for _, pr := range r.Due(prs, now) {
		ref, ok := parseURL(pr.URL)
		if !ok || slices.Contains(numbers[ref.repo], ref.number) {
			continue
		}
		r.checkedAt[pr.URL] = now
		if numbers[ref.repo] == nil {
			repos = append(repos, ref.repo)
		}
		numbers[ref.repo] = append(numbers[ref.repo], ref.number)
	}

	fresh := map[prRef]state.PullRequest{}
	var errs []error
	for _, repo := range repos {
		if err := r.fetch(ctx, repo, numbers[repo], fresh); err != nil {
			errs = append(errs, fmt.Errorf("refreshing pull requests in %s: %w", repo, err))
		}
	}

	out := slices.Clone(prs)
	for i, pr := range out {
		ref, _ := parseURL(pr.URL)
		if f, ok := fresh[ref]; ok {
			f.Source = pr.Source
			out[i] = f
		}
	}
	return out, errors.Join(errs...)
}

func (r *Refresher) fetch(ctx context.Context, repo string, numbers []int, into map[prRef]state.PullRequest) error {
	owner, name, _ := strings.Cut(repo, "/")
	args := []string{"api", "graphql", "-f", "owner=" + owner, "-f", "name=" + name, "-f", "query=" + numberQuery(numbers)}
	fields, runErr := runQuery(ctx, r.gh, "", args)
	var missing []error
	for _, n := range numbers {
		ref := prRef{repo: repo, number: n}
		var node *prNode
		if raw, ok := fields[numberAlias(n)]; ok {
			if err := json.Unmarshal(raw, &node); err != nil {
				return errors.Join(runErr, fmt.Errorf("reading %s#%d: %w", repo, n, err))
			}
		}
		if node == nil {
			missing = append(missing, fmt.Errorf("%s#%d: %w", repo, n, ErrNotFound))
			continue
		}
		into[ref] = node.pullRequest("")
	}
	if runErr != nil {
		return runErr
	}
	return errors.Join(missing...)
}

type prRef struct {
	repo   string
	number int
}

func parseURL(url string) (prRef, bool) {
	m := linkURL.FindStringSubmatch(url)
	if m == nil {
		return prRef{}, false
	}
	n, err := strconv.Atoi(m[3])
	return prRef{repo: m[1] + "/" + m[2], number: n}, err == nil
}
