package triage

import (
	"slices"
	"time"
)

// Section is the part of the inbox a row is listed in.
type Section string

// The sections, in the order the section rule tries them.
const (
	SectionSnoozed  Section = "snoozed"
	SectionPinned   Section = "pinned"
	SectionSettled  Section = "settled"
	SectionNeedsYou Section = "needs_you"
	SectionWorking  Section = "working"
	SectionActive   Section = "active"
)

// Row is an agent paired with its entry, which is the zero Entry until one
// has been recorded.
type Row struct {
	Agent Agent
	Entry Entry
}

// Section says where the row is listed. With foldWorking, an agent at work
// goes under Working instead of Active.
func (r Row) Section(now time.Time, foldWorking bool) Section {
	switch {
	case r.Entry.Snoozed(now):
		return SectionSnoozed
	case r.Entry.Pinned():
		return SectionPinned
	case r.Settled():
		return SectionSettled
	case (r.Agent.NeedsYou() || r.PRNeedsYou()) && !r.Entry.Acknowledged():
		return SectionNeedsYou
	case foldWorking && r.Agent.Status == StatusWorking && !r.Agent.WaitingOnWork:
		return SectionWorking
	default:
		return SectionActive
	}
}

// Settled reports whether the agent has no block.
func (r Row) Settled() bool {
	return !r.Agent.HasBlock
}

// PRNeedsYou reports whether the headline pull request has failing checks or
// changes requested and the agent has stopped working on it.
func (r Row) PRNeedsYou() bool {
	pr, ok := r.Agent.headline()
	return ok && r.Agent.Status != StatusWorking && pr.NeedsYou()
}

// Unread reports whether the agent finished or started needing you since
// its tab was last seen.
func (r Row) Unread() bool {
	return r.markable() && !r.Entry.unchangedSince(r.Entry.LastVisitedAt)
}

// Read reports whether the agent finished or needs you and its tab has been
// seen since.
func (r Row) Read() bool {
	return r.markable() && !r.Unread()
}

// Only an agent with a block carries a read or unread mark, since seeing
// its tab is the only thing that clears one.
func (r Row) markable() bool {
	return r.Agent.HasBlock && (r.Agent.Finished() || r.Agent.NeedsYou())
}

// Revived reports whether the agent was woken or restored and none of its
// own pull requests has resolved since.
func (r Row) Revived() bool {
	if r.Entry.RevivedAt.IsZero() {
		return false
	}
	for _, pr := range r.Agent.ownPullRequests() {
		if pr.Resolved() && !slices.Contains(r.Entry.RevivedPRs, pr.URL) {
			return false
		}
	}
	return true
}

// ShouldAutoSettle reports whether the agent's tab should be closed because
// its work has landed. shown is whether a Rex app is showing that tab.
func (r Row) ShouldAutoSettle(now time.Time, shown bool) bool {
	if !r.Agent.HasBlock || shown || r.Entry.Pinned() || r.Entry.Snoozed(now) || r.Revived() {
		return false
	}
	// Blocked does not hold it: an agent that opens a pull request ends its
	// turn asking whether anything needs changing, and merging answers it.
	if !r.Agent.Finished() && r.Agent.Status != StatusBlocked {
		return false
	}
	// Without a start time every resolved pull request would count as the
	// agent's own, and closing a tab is not a guess to make.
	if r.Agent.Began.IsZero() {
		return false
	}
	own := r.Agent.ownPullRequests()
	if len(own) == 0 {
		return false
	}
	for _, pr := range own {
		if !pr.ResolvedSince(r.Agent.Began) {
			return false
		}
	}
	return true
}
