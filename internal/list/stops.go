package list

import (
	"slices"

	"github.com/jordanbyron/agent-inbox/internal/snapshot"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

type stop struct {
	key  string
	fold triage.Section
}

func rowStop(key string) stop { return stop{key: key} }

func foldStop(section triage.Section) stop { return stop{fold: section} }

func (s stop) isFold() bool { return s.fold != "" }

// sectionTab is the list's own lead group rather than a triage section:
// the agents in the Rex tab the list sits in, lifted out of their sections.
const sectionTab triage.Section = "tab"

// arrange lifts the rows whose block is in the tab into sectionTab, ahead
// of the rest, so a list parked beside an agent says which one it is.
func arrange(snap snapshot.Snapshot, tab string) snapshot.Snapshot {
	if tab == "" {
		return snap
	}
	here := snapshot.Section{Name: sectionTab}
	out := snapshot.Snapshot{WatcherVersion: snap.WatcherVersion, Sections: []snapshot.Section{{}}}
	for _, sec := range snap.Sections {
		rest := snapshot.Section{Name: sec.Name}
		for _, row := range sec.Rows {
			if row.RexWindow == tab {
				here.Rows = append(here.Rows, row)
			} else {
				rest.Rows = append(rest.Rows, row)
			}
		}
		out.Sections = append(out.Sections, rest)
	}
	out.Sections[0] = here
	return out
}

var foldable = []triage.Section{triage.SectionWorking, triage.SectionSnoozed, triage.SectionSettled}

func folded(section triage.Section, expanded map[triage.Section]bool) bool {
	return slices.Contains(foldable, section) && !expanded[section]
}

type sectionStops struct {
	section triage.Section
	stops   []stop
}

// stopsBySection leaves out a section with no rows, and so an empty fold,
// since there is nothing under it to open.
func stopsBySection(snap snapshot.Snapshot, expanded map[triage.Section]bool) []sectionStops {
	var out []sectionStops
	for _, sec := range snap.Sections {
		if len(sec.Rows) == 0 {
			continue
		}
		group := sectionStops{section: sec.Name}
		if folded(sec.Name, expanded) {
			group.stops = []stop{foldStop(sec.Name)}
		} else {
			for _, row := range sec.Rows {
				group.stops = append(group.stops, rowStop(row.Key))
			}
		}
		out = append(out, group)
	}
	return out
}

func allStops(snap snapshot.Snapshot, expanded map[triage.Section]bool) []stop {
	var out []stop
	for _, group := range stopsBySection(snap, expanded) {
		out = append(out, group.stops...)
	}
	return out
}

func sectionHeads(snap snapshot.Snapshot, expanded map[triage.Section]bool) []sectionStops {
	groups := stopsBySection(snap, expanded)
	for i := range groups {
		groups[i].stops = groups[i].stops[:1]
	}
	return groups
}

func sectionOf(snap snapshot.Snapshot, s stop) (triage.Section, bool) {
	if s.isFold() {
		return s.fold, true
	}
	for _, sec := range snap.Sections {
		for _, row := range sec.Rows {
			if row.Key == s.key {
				return sec.Name, true
			}
		}
	}
	return "", false
}
