package watcher

import (
	"strings"

	"github.com/jordanbyron/agent-inbox/internal/recipe"
	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/state"
)

// LabelPrefix starts the label of a block agent-inbox launched an agent in;
// the agent's id follows it.
const LabelPrefix = "agent-inbox:"

// identity is how the watcher knows an agent.
type identity struct {
	// key is the agent id of a launched agent and the block id of a found one.
	key      string
	launched bool
	harness  string
}

// identify says whether a block is an agent, and which. A block labeled
// for a record we hold is that launched agent; any other block whose root
// status record names a recipe's app is a found agent. The rest, such as a
// build tool reporting its progress, are not agents.
func identify(blockID, label string, records []rex.StatusRecord, held map[string]state.Record, recipes recipe.Set) (identity, bool) {
	if id, ok := strings.CutPrefix(label, LabelPrefix); ok {
		if record, ok := held[id]; ok {
			return identity{key: id, launched: true, harness: record.Harness}, true
		}
	}
	root, ok := rootRecord(records)
	if !ok {
		return identity{}, false
	}
	app := root.App
	if app == "" {
		app = root.EffectiveApp
	}
	r, ok := recipes.ForApp(app)
	if !ok || app == "" {
		return identity{}, false
	}
	return identity{key: blockID, harness: r.Name}, true
}
