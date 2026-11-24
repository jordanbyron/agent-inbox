package pulls

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jordanbyron/agent-inbox/internal/state"
)

var (
	pullURL  = regexp.MustCompile(`https://github\.com/([A-Za-z0-9-]+)/([A-Za-z0-9_.-]+)/pull/(\d+)\b`)
	linkURL  = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9-]+)/([A-Za-z0-9_.-]+)/pull/(\d+)(?:[/?#]\S*)?$`)
	linkRef  = regexp.MustCompile(`^([A-Za-z0-9-]+)/([A-Za-z0-9_.-]+)#(\d+)$`)
	linkBare = regexp.MustCompile(`^#?(\d+)$`)
)

// FindByBranch returns the pull requests of the given branches in the
// repository checked out at dir, newest first within each branch, asking gh
// once for all of them.
func FindByBranch(ctx context.Context, gh Runner, dir string, branches []string) ([]state.PullRequest, error) {
	if len(branches) == 0 {
		return nil, nil
	}
	// gh fills {owner} and {repo} from the repository at dir.
	args := []string{"api", "graphql", "-F", "owner={owner}", "-F", "name={repo}"}
	for i, b := range branches {
		args = append(args, "-f", fmt.Sprintf("b%d=%s", i, b))
	}
	args = append(args, "-f", "query="+branchQuery(len(branches)))

	fields, err := runQuery(ctx, gh, dir, args)
	if err != nil {
		return nil, fmt.Errorf("finding pull requests for branches in %s: %w", dir, err)
	}
	var found []state.PullRequest
	seen := map[string]bool{}
	for i := range branches {
		var conn struct {
			Nodes []prNode `json:"nodes"`
		}
		if raw, ok := fields[fmt.Sprintf("b%d", i)]; ok {
			if err := json.Unmarshal(raw, &conn); err != nil {
				return nil, fmt.Errorf("reading pull requests for %s: %w", branches[i], err)
			}
		}
		for _, n := range conn.Nodes {
			if !seen[n.URL] {
				seen[n.URL] = true
				found = append(found, n.pullRequest(state.SourceBranch))
			}
		}
	}
	return found, nil
}

// FindInText returns the GitHub pull request links in text, such as a
// terminal capture, in the order they first appear.
func FindInText(text string) []state.PullRequest {
	var found []state.PullRequest
	seen := map[string]bool{}
	for _, m := range pullURL.FindAllStringSubmatch(text, -1) {
		pr, ok := linkTo(m[1], m[2], m[3], state.SourceCapture)
		if ok && !seen[pr.URL] {
			seen[pr.URL] = true
			found = append(found, pr)
		}
	}
	return found
}

// Link reads a pull request typed by hand: a link, owner/repo#n, or #n or n
// in the repository checked out at dir.
func Link(ctx context.Context, gh Runner, dir, input string) (state.PullRequest, error) {
	input = strings.TrimSpace(input)
	if m := linkURL.FindStringSubmatch(input); m != nil {
		return checkedLink(m[1], m[2], m[3], input)
	}
	if m := linkRef.FindStringSubmatch(input); m != nil {
		return checkedLink(m[1], m[2], m[3], input)
	}
	m := linkBare.FindStringSubmatch(input)
	if m == nil {
		return state.PullRequest{}, fmt.Errorf("reading %q: %w", input, ErrNotALink)
	}
	out, err := gh.Run(ctx, dir, "repo", "view", "--json", "nameWithOwner", "--jq", ".nameWithOwner")
	if err != nil {
		return state.PullRequest{}, fmt.Errorf("finding the repository for %q: %w", input, err)
	}
	owner, repo, ok := strings.Cut(strings.TrimSpace(string(out)), "/")
	if !ok {
		return state.PullRequest{}, fmt.Errorf("finding the repository for %q: gh said %q", input, out)
	}
	return checkedLink(owner, repo, m[1], input)
}

// ErrNotALink is what Link returns for input that names no pull request.
var ErrNotALink = errors.New("not a pull request link")

func checkedLink(owner, repo, number, input string) (state.PullRequest, error) {
	pr, ok := linkTo(owner, repo, number, state.SourceLinked)
	if !ok {
		return state.PullRequest{}, fmt.Errorf("reading %q: %w", input, ErrNotALink)
	}
	return pr, nil
}

func linkTo(owner, repo, number string, source state.Source) (state.PullRequest, bool) {
	n, err := strconv.Atoi(number)
	if err != nil || n <= 0 {
		return state.PullRequest{}, false
	}
	return state.PullRequest{
		URL:    fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, repo, n),
		Number: n,
		Source: source,
	}, true
}
