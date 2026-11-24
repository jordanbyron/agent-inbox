// Package triage holds the rules that sort agents into the inbox's sections.
// Everything here is pure: the caller observes the agent and passes the time.
package triage

import "time"

// Status is an agent's state in OSC 7501's words, plus starting.
type Status string

// The statuses an agent reports. Starting means it has reported none yet.
const (
	StatusStarting Status = "starting"
	StatusIdle     Status = "idle"
	StatusWorking  Status = "working"
	StatusDone     Status = "done"
	StatusBlocked  Status = "blocked"
	StatusError    Status = "error"
)

// Health is what gh last said is wrong with an open pull request.
type Health string

// The healths a pull request can have. HealthUnknown means gh has not said.
const (
	HealthUnknown          Health = ""
	HealthClean            Health = "CLEAN"
	HealthFailing          Health = "FAILING"
	HealthChangesRequested Health = "CHANGES_REQUESTED"
	HealthConflicting      Health = "CONFLICTING"
)

// PullRequest is what the rules need to know about one of an agent's pull
// requests.
type PullRequest struct {
	URL string
	// Known is whether anything has told us the pull request's state yet.
	Known  bool
	Health Health
	// ResolvedAt is when it was merged or closed, and zero while it is open
	// or gh has not said when.
	ResolvedAt time.Time
}

// NeedsYou reports whether the pull request wants you back. A conflict only
// waits on whoever rebases.
func (pr PullRequest) NeedsYou() bool {
	return pr.Health == HealthFailing || pr.Health == HealthChangesRequested
}

// Resolved reports whether the pull request was merged or closed, and gh
// has said when.
func (pr PullRequest) Resolved() bool {
	return !pr.ResolvedAt.IsZero()
}

// ResolvedBefore reports whether it resolved before t, so an agent that
// began at t can only have mentioned it.
func (pr PullRequest) ResolvedBefore(t time.Time) bool {
	return pr.Resolved() && pr.ResolvedAt.Before(t)
}

// ResolvedSince reports whether it resolved at or after t, the only kind
// of resolution that can answer an agent that began at t.
func (pr PullRequest) ResolvedSince(t time.Time) bool {
	return pr.Resolved() && !pr.ResolvedAt.Before(t)
}

// Agent is what the rules need to know about one agent at one moment.
type Agent struct {
	Status   Status
	HasBlock bool
	// WaitingOnWork is whether it is working only on something it started,
	// such as a sub-agent or a shell, and has gone quiet itself.
	WaitingOnWork bool
	Began         time.Time
	PullRequests  []PullRequest
}

// NeedsYou reports whether the agent is blocked or in error.
func (a Agent) NeedsYou() bool {
	return a.Status == StatusBlocked || a.Status == StatusError
}

// Finished reports whether the agent has stopped with nothing to ask.
func (a Agent) Finished() bool {
	return a.Status == StatusDone || a.Status == StatusIdle
}

func (a Agent) isMention(pr PullRequest) bool {
	return !pr.Known || pr.ResolvedBefore(a.Began)
}

func (a Agent) ownPullRequests() []PullRequest {
	var own []PullRequest
	for _, pr := range a.PullRequests {
		if !a.isMention(pr) {
			own = append(own, pr)
		}
	}
	return own
}

func (a Agent) headline() (PullRequest, bool) {
	rank := func(pr PullRequest) int {
		switch {
		case a.isMention(pr):
			return 4
		case pr.NeedsYou():
			return 0
		case pr.Health == HealthConflicting:
			return 1
		case !pr.Resolved():
			return 2
		default:
			return 3
		}
	}
	var best PullRequest
	found := false
	for _, pr := range a.PullRequests {
		if !found || rank(pr) < rank(best) {
			best, found = pr, true
		}
	}
	return best, found
}
