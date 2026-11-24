// Package state keeps what agent-inbox remembers on disk for one Rex server:
// the state file and its lock, the watcher's snapshot and the watcher lock.
package state

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jordanbyron/agent-inbox/internal/triage"
)

// Version is the format of state.json this build reads and writes.
const Version = 1

// State is everything state.json holds. Read and Update return one with
// every map made, so a caller can add to them straight away.
type State struct {
	// Records are the launched agents, keyed by agent id.
	Records map[string]Record `json:"records"`
	// Entries are the triage entries, keyed by agent key.
	Entries map[string]triage.Entry `json:"entries"`
	// PullRequests are the pull requests found for each agent, keyed by
	// agent key.
	PullRequests map[string][]PullRequest `json:"pull_requests"`
}

func emptyState() *State {
	s := &State{}
	s.makeMaps()
	return s
}

func (s *State) makeMaps() {
	if s.Records == nil {
		s.Records = map[string]Record{}
	}
	if s.Entries == nil {
		s.Entries = map[string]triage.Entry{}
	}
	if s.PullRequests == nil {
		s.PullRequests = map[string][]PullRequest{}
	}
}

type stateFile struct {
	Version int `json:"version"`
	State
}

// Record is what agent-inbox remembers about an agent it launched.
type Record struct {
	Harness string
	// SessionID is the harness's own id for the conversation, which its
	// resume command takes.
	SessionID string
	Name      string
	Dir       string
	// Worktree and Branch are empty for an agent started without a worktree.
	Worktree string
	Branch   string
	// Parent is the id of the agent that launched this one, if any.
	Parent     string
	RexSession string
	Began      time.Time
}

type recordJSON struct {
	Harness    string `json:"harness"`
	SessionID  string `json:"session_id,omitempty"`
	Name       string `json:"name"`
	Dir        string `json:"dir"`
	Worktree   string `json:"worktree,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Parent     string `json:"parent,omitempty"`
	RexSession string `json:"rex_session"`
	Began      int64  `json:"began,omitempty"`
}

// MarshalJSON writes the record with its time as Unix seconds, as
// triage.Entry does.
func (r Record) MarshalJSON() ([]byte, error) {
	return json.Marshal(recordJSON{
		Harness:    r.Harness,
		SessionID:  r.SessionID,
		Name:       r.Name,
		Dir:        r.Dir,
		Worktree:   r.Worktree,
		Branch:     r.Branch,
		Parent:     r.Parent,
		RexSession: r.RexSession,
		Began:      unixSeconds(r.Began),
	})
}

// UnmarshalJSON reads a record written by MarshalJSON.
func (r *Record) UnmarshalJSON(data []byte) error {
	var j recordJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return fmt.Errorf("reading record: %w", err)
	}
	*r = Record{
		Harness:    j.Harness,
		SessionID:  j.SessionID,
		Name:       j.Name,
		Dir:        j.Dir,
		Worktree:   j.Worktree,
		Branch:     j.Branch,
		Parent:     j.Parent,
		RexSession: j.RexSession,
		Began:      fromUnixSeconds(j.Began),
	}
	return nil
}

// PRState is gh's word for whether a pull request is open, merged or closed.
type PRState string

// The states a pull request can be in. PRStateUnknown means gh has not said.
// gh reports a draft as OPEN plus isDraft; it is kept as DRAFT here.
const (
	PRStateUnknown PRState = ""
	PRStateOpen    PRState = "OPEN"
	PRStateDraft   PRState = "DRAFT"
	PRStateMerged  PRState = "MERGED"
	PRStateClosed  PRState = "CLOSED"
)

// Source is how a pull request came to be an agent's.
type Source string

const (
	// SourceBranch is a branch checked out in the agent's worktree.
	SourceBranch Source = "branch"
	// SourceCapture is a link read from the agent's terminal.
	SourceCapture Source = "capture"
	// SourceLinked is a link you gave it by hand.
	SourceLinked Source = "linked"
)

// PullRequest is what is remembered about one of an agent's pull requests,
// so it outlives the block it was found in.
type PullRequest struct {
	URL    string
	Number int
	Title  string
	State  PRState
	Health triage.Health
	// ResolvedAt is when it was merged or closed, and zero while it is open
	// or gh has not said when.
	ResolvedAt time.Time
	Source     Source
}

// Triage returns what the triage rules need to know about the pull request.
func (pr PullRequest) Triage() triage.PullRequest {
	return triage.PullRequest{
		URL:        pr.URL,
		Known:      pr.State != PRStateUnknown,
		Health:     pr.Health,
		ResolvedAt: pr.ResolvedAt,
	}
}

type pullRequestJSON struct {
	URL        string        `json:"url"`
	Number     int           `json:"number,omitempty"`
	Title      string        `json:"title,omitempty"`
	State      PRState       `json:"state,omitempty"`
	Health     triage.Health `json:"health,omitempty"`
	ResolvedAt int64         `json:"resolved_at,omitempty"`
	Source     Source        `json:"source"`
}

// MarshalJSON writes the pull request with its time as Unix seconds.
func (pr PullRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(pullRequestJSON{
		URL:        pr.URL,
		Number:     pr.Number,
		Title:      pr.Title,
		State:      pr.State,
		Health:     pr.Health,
		ResolvedAt: unixSeconds(pr.ResolvedAt),
		Source:     pr.Source,
	})
}

// UnmarshalJSON reads a pull request written by MarshalJSON.
func (pr *PullRequest) UnmarshalJSON(data []byte) error {
	var j pullRequestJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return fmt.Errorf("reading pull request: %w", err)
	}
	*pr = PullRequest{
		URL:        j.URL,
		Number:     j.Number,
		Title:      j.Title,
		State:      j.State,
		Health:     j.Health,
		ResolvedAt: fromUnixSeconds(j.ResolvedAt),
		Source:     j.Source,
	}
	return nil
}

func unixSeconds(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func fromUnixSeconds(s int64) time.Time {
	if s == 0 {
		return time.Time{}
	}
	return time.Unix(s, 0)
}
