package state_test

import (
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/state"
)

func TestSnapshotRoundTrip(t *testing.T) {
	type body struct {
		Version string
		Rows    []string
	}
	store := state.New(t.TempDir())
	want := body{Version: "v1.0.0", Rows: []string{"a1", "a2"}}
	now := began.Add(1500 * time.Millisecond)

	require.NoError(t, store.WriteSnapshot(want, now))

	var got body
	writtenAt, err := store.ReadSnapshot(&got)
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Equal(t, began.Add(time.Second), writtenAt)
}

func TestReadMissingSnapshot(t *testing.T) {
	var got []string
	_, err := state.New(t.TempDir()).ReadSnapshot(&got)
	require.ErrorIs(t, err, fs.ErrNotExist)
}
