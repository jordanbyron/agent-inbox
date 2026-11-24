package verbs

import "context"

// DeleteFamily is Delete aimed at an agent's key, which makes the inbox the
// deleter reap uses.
func (in *Inbox) DeleteFamily(ctx context.Context, key string) error {
	_, _, err := in.Delete(ctx, Target{Key: key})
	return err
}
