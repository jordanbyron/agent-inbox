package state_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

var began = time.Unix(1_790_000_000, 0)

func fullState() *state.State {
	return &state.State{
		Records: map[string]state.Record{
			"a1": {
				Harness:    "claude",
				SessionID:  "0b6c7f9e",
				Name:       "fix the flaky spec",
				Title:      "Fix the flaky billing spec",
				Dir:        "/src/app",
				Worktree:   "/src/app/.worktrees/fix-the-flaky-spec",
				Branch:     "fix-the-flaky-spec",
				RexSession: "work",
				Began:      began,
			},
			"a2": {
				Harness:    "codex",
				Name:       "write the docs",
				Dir:        "/src/app",
				Parent:     "a1",
				RexSession: "work",
				Began:      began.Add(time.Minute),
			},
		},
		Entries: map[string]triage.Entry{
			"a1": {
				Alias:      "flaky",
				WakeAt:     began.Add(time.Hour),
				LastState:  triage.StatusDone,
				StateSince: began.Add(10 * time.Minute),
				RevivedAt:  began.Add(20 * time.Minute),
				RevivedPRs: []string{"https://github.com/o/r/pull/7"},
				PRHealth:   triage.HealthFailing,
			},
		},
		PullRequests: map[string][]state.PullRequest{
			"a1": {
				{
					URL:    "https://github.com/o/r/pull/8",
					Number: 8,
					Title:  "Fix the flaky spec",
					State:  state.PRStateOpen,
					Health: triage.HealthFailing,
					Source: state.SourceBranch,
				},
				{
					URL:        "https://github.com/o/r/pull/7",
					Number:     7,
					Title:      "Retry the upload",
					State:      state.PRStateMerged,
					Health:     triage.HealthClean,
					ResolvedAt: began.Add(5 * time.Minute),
					Source:     state.SourceCapture,
				},
			},
		},
	}
}

func replaceWith(want *state.State) func(*state.State) error {
	return func(s *state.State) error {
		*s = *want
		return nil
	}
}

func TestRoundTrip(t *testing.T) {
	store := state.New(t.TempDir())
	want := fullState()

	require.NoError(t, store.Update(replaceWith(want)))

	got, err := store.Read()
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestFileFormat(t *testing.T) {
	dir := t.TempDir()
	st := &state.State{
		Records: map[string]state.Record{"a1": {Harness: "claude", Name: "n", Dir: "/d", RexSession: "work", Began: began}},
		PullRequests: map[string][]state.PullRequest{
			"a1": {{URL: "https://github.com/o/r/pull/7", Number: 7, State: state.PRStateMerged, ResolvedAt: began, Source: state.SourceLinked}},
		},
	}
	require.NoError(t, state.New(dir).Update(replaceWith(st)))

	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	require.NoError(t, err)
	require.JSONEq(t, `{
		"version": 1,
		"records": {"a1": {"harness": "claude", "name": "n", "dir": "/d", "rex_session": "work", "began": 1790000000}},
		"entries": {},
		"pull_requests": {"a1": [{"url": "https://github.com/o/r/pull/7", "number": 7, "state": "MERGED", "resolved_at": 1790000000, "source": "linked"}]}
	}`, string(data))
}

func TestPullRequestTriage(t *testing.T) {
	cases := []struct {
		name string
		pr   state.PullRequest
		want triage.PullRequest
	}{
		{
			name: "one gh has not looked at yet is unknown",
			pr:   state.PullRequest{URL: "u", Source: state.SourceCapture},
			want: triage.PullRequest{URL: "u"},
		},
		{
			name: "one gh has looked at is known, with its health and resolution",
			pr:   state.PullRequest{URL: "u", State: state.PRStateMerged, Health: triage.HealthClean, ResolvedAt: began},
			want: triage.PullRequest{URL: "u", Known: true, Health: triage.HealthClean, ResolvedAt: began},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, c.pr.Triage())
		})
	}
}

func TestReadMissingFile(t *testing.T) {
	got, err := state.New(t.TempDir()).Read()
	require.NoError(t, err)
	require.Empty(t, got.Records)
	require.Empty(t, got.Entries)
	require.Empty(t, got.PullRequests)

	got.Records["a1"] = state.Record{Name: "maps are ready to add to"}
}

func TestReadBadFile(t *testing.T) {
	cases := []struct {
		name     string
		contents string
	}{
		{name: "not JSON", contents: `{"version": 1, "records": `},
		{name: "no version", contents: `{"records": {}}`},
		{name: "a newer version", contents: `{"version": 2}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state.json")
			require.NoError(t, os.WriteFile(path, []byte(c.contents), 0o600))

			_, err := state.New(dir).Read()
			require.ErrorContains(t, err, path)
		})
	}
}

func TestFailedUpdateWritesNothing(t *testing.T) {
	dir := t.TempDir()
	store := state.New(dir)
	require.NoError(t, store.Update(replaceWith(fullState())))
	before, err := os.ReadFile(filepath.Join(dir, "state.json"))
	require.NoError(t, err)
	refused := errors.New("refused")

	err = store.Update(func(s *state.State) error {
		s.Records["a3"] = state.Record{Name: "never saved"}
		return refused
	})

	require.ErrorIs(t, err, refused)
	after, err := os.ReadFile(filepath.Join(dir, "state.json"))
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, files, 2, "only state.json and state.lock, no temp file")
}

func TestConcurrentUpdatesLoseNothing(t *testing.T) {
	dir := t.TempDir()
	const writers, each = 8, 20
	errs := make([]error, writers)
	var wg sync.WaitGroup
	for w := range writers {
		wg.Go(func() {
			errs[w] = addRecords(state.New(dir), fmt.Sprint("writer", w), each)
		})
	}
	wg.Wait()
	require.NoError(t, errors.Join(errs...))

	got, err := state.New(dir).Read()
	require.NoError(t, err)
	require.Len(t, got.Records, writers*each)
}

func TestUpdatesAcrossProcessesLoseNothing(t *testing.T) {
	dir := t.TempDir()
	const writers, each = 4, 20
	cmds := make([]*os.Process, 0, writers)
	for w := range writers {
		cmd := helperCommand(t, "add-records",
			"STATE_TEST_DIR="+dir, fmt.Sprint("STATE_TEST_WRITER=process", w), fmt.Sprint("STATE_TEST_COUNT=", each))
		require.NoError(t, cmd.Start())
		cmds = append(cmds, cmd.Process)
	}
	for _, p := range cmds {
		status, err := p.Wait()
		require.NoError(t, err)
		require.True(t, status.Success(), "helper exited with %v", status)
	}

	got, err := state.New(dir).Read()
	require.NoError(t, err)
	require.Len(t, got.Records, writers*each)
}

func TestReaderNeverSeesPartialFile(t *testing.T) {
	dir := t.TempDir()
	const writers, each = 4, 25
	var writing sync.WaitGroup
	writeErrs := make([]error, writers)
	for w := range writers {
		writing.Go(func() {
			for i := range each {
				writeErrs[w] = state.New(dir).Update(func(s *state.State) error {
					s.Records[fmt.Sprint(w, "-", i)] = state.Record{Name: strings.Repeat("x", 500*i)}
					return nil
				})
				if writeErrs[w] != nil {
					return
				}
			}
		})
	}
	done := make(chan struct{})
	var readErr error
	var reads int
	var reading sync.WaitGroup
	reading.Go(func() {
		for {
			select {
			case <-done:
				return
			default:
			}
			if _, readErr = state.New(dir).Read(); readErr != nil {
				return
			}
			reads++
		}
	})

	writing.Wait()
	close(done)
	reading.Wait()

	require.NoError(t, errors.Join(writeErrs...))
	require.NoError(t, readErr)
	require.Positive(t, reads)
}
