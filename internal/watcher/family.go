package watcher

import (
	"slices"

	"github.com/jordanbyron/agent-inbox/internal/triage"
)

// family is a root agent and every agent it started, however deep. An
// agent whose parent is not among the agents heads a family of its own.
type family struct {
	root agent
	// children are in key order.
	children []agent
}

func (f family) members() []agent {
	return append([]agent{f.root}, f.children...)
}

// families groups agents, which are in key order, under their roots.
func families(agents []agent) []family {
	byKey := map[string]agent{}
	for _, a := range agents {
		byKey[a.key] = a
	}
	var order []string
	children := map[string][]agent{}
	for _, a := range agents {
		root := rootOf(a, byKey)
		if root == a.key {
			order = append(order, root)
			continue
		}
		children[root] = append(children[root], a)
	}
	out := make([]family, 0, len(order))
	for _, key := range order {
		out = append(out, family{root: byKey[key], children: children[key]})
	}
	return out
}

// rootOf follows an agent's parents as far as they go among the agents. A
// loop leaves the agent at its own head.
func rootOf(a agent, byKey map[string]agent) string {
	seen := map[string]bool{a.key: true}
	key := a.key
	for {
		parent, ok := byKey[byKey[key].record.Parent]
		if !ok {
			return key
		}
		if seen[parent.key] {
			return a.key
		}
		seen[parent.key] = true
		key = parent.key
	}
}

// familySection is where a family is listed, given its root's own section
// and each member's. Snoozed, pinned and settled are the root's to decide;
// otherwise the most urgent member's section wins, and Active, which has
// something to look at, is more urgent than Working.
func familySection(root triage.Section, members []triage.Section) triage.Section {
	switch root {
	case triage.SectionSnoozed, triage.SectionPinned, triage.SectionSettled:
		return root
	}
	all := append([]triage.Section{root}, members...)
	for _, urgent := range []triage.Section{triage.SectionNeedsYou, triage.SectionActive} {
		if slices.Contains(all, urgent) {
			return urgent
		}
	}
	return root
}

// familyAgent is the family as auto-settle sees it: at work while any
// member with a block is, in error while any such member is, owning every
// member's pull requests, and dated from the root.
func familyAgent(root triage.Agent, children []triage.Agent) triage.Agent {
	fam := root
	fam.PullRequests = slices.Clone(root.PullRequests)
	for _, c := range children {
		switch {
		case !c.HasBlock:
		case c.Status == triage.StatusWorking:
			fam.Status = triage.StatusWorking
		case c.Status == triage.StatusError && fam.Status != triage.StatusWorking:
			fam.Status = triage.StatusError
		}
		for _, pr := range c.PullRequests {
			if !slices.ContainsFunc(fam.PullRequests, func(own triage.PullRequest) bool { return own.URL == pr.URL }) {
				fam.PullRequests = append(fam.PullRequests, pr)
			}
		}
	}
	return fam
}

// familyEntry is the root's entry as auto-settle sees it: revived when any
// member was, and holding for every pull request any revive remembered.
func familyEntry(root triage.Entry, children []triage.Entry) triage.Entry {
	fam := root
	fam.RevivedPRs = slices.Clone(root.RevivedPRs)
	for _, c := range children {
		if c.RevivedAt.After(fam.RevivedAt) {
			fam.RevivedAt = c.RevivedAt
		}
		fam.RevivedPRs = append(fam.RevivedPRs, c.RevivedPRs...)
	}
	return fam
}
