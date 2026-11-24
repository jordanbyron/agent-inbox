package verbs

import (
	"context"
	"fmt"
	"slices"

	"github.com/jordanbyron/agent-inbox/internal/reap"
)

// DeleteFamily is Delete for reap, which makes the inbox the deleter reap
// uses. Reap chose the family from a state read earlier, so it is checked
// again first: a family restored, pinned or snoozed since, or with a tab
// open, is kept.
func (in *Inbox) DeleteFamily(ctx context.Context, key string) error {
	st, err := in.Store.Read()
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(reap.Due(st, in.Now()), func(f reap.Family) bool { return f.Key == key }) {
		return fmt.Errorf("not reaping %s: it is no longer settled, or it was pinned or snoozed", key)
	}
	all, err := in.places(ctx)
	if err != nil {
		return err
	}
	for _, m := range family(st, agent{key: key, launched: true, record: st.Records[key]}) {
		if _, running := all.placeOf(m); running {
			return fmt.Errorf("not reaping %s: %s has a tab open", key, m.record.Name)
		}
	}
	_, _, err = in.Delete(ctx, Target{Key: key})
	return err
}
