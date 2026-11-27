// Package pulls finds the GitHub pull requests that belong to an agent and
// keeps them fresh through gh.
package pulls

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

// Runner runs gh with args in dir and returns what it printed to stdout.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) ([]byte, error)
}

// GH runs the gh on PATH.
type GH struct{}

// Run runs gh in dir. It returns stdout even when gh fails, since gh api
// graphql prints the partial answer and exits 1 when part of a query fails.
func (GH) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return out, fmt.Errorf("running gh: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
	}
	if err != nil {
		return out, fmt.Errorf("running gh: %w", err)
	}
	return out, nil
}

const prFragment = `fragment pr on PullRequest {
  number url title state isDraft closedAt reviewDecision mergeable
  commits(last: 1) { nodes { commit { statusCheckRollup { contexts(first: 100) { nodes {
    __typename
    ... on CheckRun { name conclusion startedAt checkSuite { workflowRun { workflow { name } } } }
    ... on StatusContext { context state createdAt }
  } } } } } }
}`

// The five pull requests claude-inbox's gh pr list --head --state all --limit 5 got.
const branchArgs = `first: 5, states: [OPEN, MERGED, CLOSED], orderBy: {field: CREATED_AT, direction: DESC}`

// Branch names go in as variables so none needs escaping.
func branchQuery(branches int) string {
	var vars, fields strings.Builder
	for i := range branches {
		fmt.Fprintf(&vars, ", $b%d: String!", i)
		fmt.Fprintf(&fields, "    b%d: pullRequests(headRefName: $b%d, %s) { nodes { ...pr } }\n", i, i, branchArgs)
	}
	return fmt.Sprintf("query($owner: String!, $name: String!%s) {\n  repository(owner: $owner, name: $name) {\n%s  }\n}\n%s",
		vars.String(), fields.String(), prFragment)
}

func numberQuery(numbers []int) string {
	var fields strings.Builder
	for _, n := range numbers {
		fmt.Fprintf(&fields, "    %s: pullRequest(number: %d) { ...pr }\n", numberAlias(n), n)
	}
	return fmt.Sprintf("query($owner: String!, $name: String!) {\n  repository(owner: $owner, name: $name) {\n%s  }\n}\n%s",
		fields.String(), prFragment)
}

func numberAlias(n int) string {
	return "pr" + strconv.Itoa(n)
}

func runQuery(ctx context.Context, gh Runner, dir string, args []string) (map[string]json.RawMessage, error) {
	out, runErr := gh.Run(ctx, dir, args...)
	if len(out) == 0 {
		return nil, runErr
	}
	var resp struct {
		Data struct {
			Repository map[string]json.RawMessage `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, errors.Join(runErr, fmt.Errorf("reading gh's answer: %w", err))
	}
	return resp.Data.Repository, runErr
}

type prNode struct {
	Number         int        `json:"number"`
	URL            string     `json:"url"`
	Title          string     `json:"title"`
	State          string     `json:"state"`
	IsDraft        bool       `json:"isDraft"`
	ClosedAt       *time.Time `json:"closedAt"`
	ReviewDecision string     `json:"reviewDecision"`
	Mergeable      string     `json:"mergeable"`
	Commits        struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *struct {
					Contexts struct {
						Nodes []checkNode `json:"nodes"`
					} `json:"contexts"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

type checkNode struct {
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
	StartedAt  string `json:"startedAt"`
	CheckSuite struct {
		WorkflowRun *struct {
			Workflow struct {
				Name string `json:"name"`
			} `json:"workflow"`
		} `json:"workflowRun"`
	} `json:"checkSuite"`
	Context   string `json:"context"`
	State     string `json:"state"`
	CreatedAt string `json:"createdAt"`
}

func (c checkNode) key() [2]string {
	workflow := ""
	if c.CheckSuite.WorkflowRun != nil {
		workflow = c.CheckSuite.WorkflowRun.Workflow.Name
	}
	return [2]string{workflow, c.Name + c.Context}
}

func (c checkNode) started() string {
	return c.StartedAt + c.CreatedAt
}

func (c checkNode) verdict() string {
	if c.Conclusion != "" {
		return c.Conclusion
	}
	return c.State
}

func (n prNode) checks() []checkNode {
	if len(n.Commits.Nodes) == 0 || n.Commits.Nodes[0].Commit.StatusCheckRollup == nil {
		return nil
	}
	return n.Commits.Nodes[0].Commit.StatusCheckRollup.Contexts.Nodes
}

func (n prNode) pullRequest(source state.Source) state.PullRequest {
	pr := state.PullRequest{
		URL:    n.URL,
		Number: n.Number,
		Title:  n.Title,
		State:  state.PRState(n.State),
		Source: source,
	}
	switch pr.State {
	case state.PRStateOpen:
		pr.Health = health(n)
		if n.IsDraft {
			pr.State = state.PRStateDraft
		}
	case state.PRStateMerged, state.PRStateClosed:
		if n.ClosedAt != nil {
			pr.ResolvedAt = *n.ClosedAt
		}
	}
	return pr
}

var failedChecks = map[string]bool{
	"FAILURE": true, "ERROR": true, "TIMED_OUT": true,
	"CANCELLED": true, "ACTION_REQUIRED": true, "STARTUP_FAILURE": true, //nolint:misspell // GitHub's spelling of the enum
}

// The rollup keeps every run of a check on the head commit, so a run a newer
// one canceled or replaced still shows its verdict there; only the latest
// run of each check counts.
func health(n prNode) triage.Health {
	latest := map[[2]string]checkNode{}
	for _, c := range n.checks() {
		if seen, ok := latest[c.key()]; !ok || c.started() > seen.started() {
			latest[c.key()] = c
		}
	}
	for _, c := range latest {
		if failedChecks[c.verdict()] {
			return triage.HealthFailing
		}
	}
	switch {
	case n.ReviewDecision == "CHANGES_REQUESTED":
		return triage.HealthChangesRequested
	case n.Mergeable == "CONFLICTING":
		return triage.HealthConflicting
	default:
		return triage.HealthClean
	}
}
