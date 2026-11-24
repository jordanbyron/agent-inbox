package watcher

import (
	"maps"
	"slices"

	"github.com/jordanbyron/agent-inbox/internal/pulls"
	"github.com/jordanbyron/agent-inbox/internal/recipe"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

// agent is one agent at one moment: a block that is an agent, or a
// launched agent's record whose block has closed.
type agent struct {
	identity
	// record is the zero Record for a found agent.
	record state.Record
	// block is nil when a launched agent's block has closed.
	block     *block
	sessionID string
	status    rolled
	seen      bool
}

func (a agent) live() bool {
	return a.block != nil
}

// dir is where the agent works: its record says, or else Rex does.
func (a agent) dir() string {
	if a.record.Dir != "" {
		return a.record.Dir
	}
	if a.block != nil {
		return a.block.dir
	}
	return ""
}

// triage is what the rules need to know about the agent. One whose block
// has closed keeps the status it was last seen with.
func (a agent) triage(entry triage.Entry, prs []state.PullRequest) triage.Agent {
	status := a.status.status
	if !a.live() {
		status = entry.LastState
		if status == "" {
			status = triage.StatusStarting
		}
	}
	return triage.Agent{
		Status:        status,
		HasBlock:      a.live(),
		WaitingOnWork: a.live() && a.status.waitingOnWork,
		Began:         a.record.Began,
		PullRequests:  triagePulls(pulls.Effective(prs)),
	}
}

func triagePulls(prs []state.PullRequest) []triage.PullRequest {
	out := make([]triage.PullRequest, 0, len(prs))
	for _, pr := range prs {
		out = append(out, pr.Triage())
	}
	return out
}

// collectAgents lists every agent in the world and every launched agent in
// st, sorted by key. When two blocks carry the same agent's label, the
// first in the world's order speaks for it.
func collectAgents(w *world, st *state.State, recipes recipe.Set) []agent {
	byKey := map[string]agent{}
	for _, b := range w.blocks() {
		id, ok := identify(b.id, b.label, b.records, st.Records, recipes)
		if !ok {
			continue
		}
		if _, dup := byKey[id.key]; dup {
			continue
		}
		byKey[id.key] = agent{
			identity:  id,
			record:    st.Records[id.key],
			block:     b.block,
			sessionID: b.sessionID,
			status:    rollUp(b.records),
			seen:      w.shown(b.sessionID, b.windowID),
		}
	}
	for id, record := range st.Records {
		if _, ok := byKey[id]; !ok {
			a := agent{record: record, sessionID: record.RexSession}
			a.key, a.launched, a.harness = id, true, record.Harness
			byKey[id] = a
		}
	}
	agents := make([]agent, 0, len(byKey))
	for _, key := range slices.Sorted(maps.Keys(byKey)) {
		agents = append(agents, byKey[key])
	}
	return agents
}
