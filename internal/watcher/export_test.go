package watcher

import (
	"testing"
	"time"

	"github.com/jordanbyron/agent-inbox/internal/recipe"
	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

type Identity struct {
	Key      string
	Launched bool
	Harness  string
}

func Identify(blockID, label string, records []rex.StatusRecord, held map[string]state.Record, recipes recipe.Set) (Identity, bool) {
	id, ok := identify(blockID, label, records, held, recipes)
	return Identity{Key: id.key, Launched: id.launched, Harness: id.harness}, ok
}

type Rolled struct {
	Status        triage.Status
	WaitingOnWork bool
	Kind          string
	Msg           string
	Title         string
}

func RollUp(records []rex.StatusRecord) Rolled {
	r := rollUp(records)
	return Rolled{Status: r.status, WaitingOnWork: r.waitingOnWork, Kind: r.kind, Msg: r.msg, Title: r.title}
}

var (
	FamilySection = familySection
	FamilyAgent   = familyAgent
	Label         = label
)

// ShortenTimeouts bounds the watcher's gh, git and reap work by d for the
// rest of the test.
func ShortenTimeouts(t *testing.T, d time.Duration) {
	find, reap := findTimeout, reapTimeout
	findTimeout, reapTimeout = d, d
	t.Cleanup(func() { findTimeout, reapTimeout = find, reap })
}

const ServerProbes = serverProbes
