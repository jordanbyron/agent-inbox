package watcher

import (
	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

// rolled is what one row shows of a block's status records.
type rolled struct {
	status        triage.Status
	waitingOnWork bool
	anyWorking    bool
	kind          string
	msg           string
	title         string
}

// rollUp sums a block's status records into one row's status. Rex does no
// roll-up of child records, so this is the only place a sub-agent's state
// reaches its row. A block with no records yet is starting.
//
// The root record gives the kind, msg and title, except when a child is
// the one that needs you: an error outranks a block, and the root speaks
// first between equals.
func rollUp(records []rex.StatusRecord) rolled {
	root, hasRoot := rootRecord(records)
	r := rolled{status: triage.StatusStarting, kind: root.Kind, msg: root.Msg, title: root.Title}
	if hasRoot {
		r.status = stateOf(root)
	}

	var needsYou *rex.StatusRecord
	for i, rec := range records {
		switch stateOf(rec) {
		case triage.StatusWorking:
			r.anyWorking = true
		case triage.StatusBlocked, triage.StatusError:
			if needsYou == nil || outranks(rec, *needsYou) {
				needsYou = &records[i]
			}
		}
	}

	switch {
	case needsYou != nil:
		r.status = stateOf(*needsYou)
		if needsYou.ID != "" {
			r.kind, r.msg = needsYou.Kind, needsYou.Msg
		}
	case r.anyWorking:
		r.waitingOnWork = hasRoot && r.status != triage.StatusWorking
		r.status = triage.StatusWorking
	}
	return r
}

func outranks(rec, other rex.StatusRecord) bool {
	if stateOf(rec) != stateOf(other) {
		return stateOf(rec) == triage.StatusError
	}
	return rec.ID == ""
}

func rootRecord(records []rex.StatusRecord) (rex.StatusRecord, bool) {
	for _, rec := range records {
		if rec.ID == "" {
			return rec, true
		}
	}
	return rex.StatusRecord{}, false
}

// stateOf reads a record's state. OSC 7501 has only the five words; a state
// this build doesn't know reads as idle, which asks nothing of you.
func stateOf(rec rex.StatusRecord) triage.Status {
	switch s := triage.Status(rec.State); s {
	case triage.StatusIdle, triage.StatusWorking, triage.StatusDone, triage.StatusBlocked, triage.StatusError:
		return s
	default:
		return triage.StatusIdle
	}
}
