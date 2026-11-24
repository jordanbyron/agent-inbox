package snapshot_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/snapshot"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

var now = time.Unix(1_789_600_000, 0)

func at(seconds int) time.Time {
	return now.Add(time.Duration(seconds) * time.Second)
}

func keys(rows []snapshot.Row) []string {
	out := []string{}
	for _, r := range rows {
		out = append(out, r.Key)
	}
	return out
}

func TestSortRows(t *testing.T) {
	cases := []struct {
		name    string
		section triage.Section
		sort    snapshot.Sort
		rows    []snapshot.Row
		want    []string
	}{
		{
			name:    "pinned rows lead with the latest pin",
			section: triage.SectionPinned,
			rows:    []snapshot.Row{{Key: "a", PinnedAt: at(-30)}, {Key: "b", PinnedAt: at(-10)}, {Key: "c", PinnedAt: at(-20)}},
			want:    []string{"b", "c", "a"},
		},
		{
			name:    "needs you leads with the latest change",
			section: triage.SectionNeedsYou,
			rows:    []snapshot.Row{{Key: "a", StateSince: at(-30)}, {Key: "b", StateSince: at(-10)}},
			want:    []string{"b", "a"},
		},
		{
			name:    "active puts finished agents last, then the newest first",
			section: triage.SectionActive,
			rows: []snapshot.Row{
				{Key: "done-new", Status: triage.StatusDone, Began: at(-10)},
				{Key: "working-old", Status: triage.StatusWorking, Began: at(-50)},
				{Key: "idle-old", Status: triage.StatusIdle, Began: at(-40)},
				{Key: "blocked-new", Status: triage.StatusBlocked, Began: at(-5)},
			},
			want: []string{"blocked-new", "working-old", "done-new", "idle-old"},
		},
		{
			name:    "working leads with the newest",
			section: triage.SectionWorking,
			rows:    []snapshot.Row{{Key: "a", Began: at(-30)}, {Key: "b", Began: at(-10)}},
			want:    []string{"b", "a"},
		},
		{
			name:    "snoozed wakes soonest first, and until woken last",
			section: triage.SectionSnoozed,
			rows: []snapshot.Row{
				{Key: "woken", UntilWoken: true},
				{Key: "later", WakeAt: at(3600)},
				{Key: "soon", WakeAt: at(60)},
			},
			want: []string{"soon", "later", "woken"},
		},
		{
			name:    "settled leads with the latest change",
			section: triage.SectionSettled,
			rows:    []snapshot.Row{{Key: "a", StateSince: at(-300)}, {Key: "b", StateSince: at(-100)}},
			want:    []string{"b", "a"},
		},
		{
			name:    "the activity sort ranks every live section by the latest change",
			section: triage.SectionActive,
			sort:    snapshot.SortActivity,
			rows: []snapshot.Row{
				{Key: "working", Status: triage.StatusWorking, StateSince: at(-50), Began: at(-5)},
				{Key: "done", Status: triage.StatusDone, StateSince: at(-10), Began: at(-90)},
			},
			want: []string{"done", "working"},
		},
		{
			name:    "the activity sort leaves Snoozed in wake order",
			section: triage.SectionSnoozed,
			sort:    snapshot.SortActivity,
			rows:    []snapshot.Row{{Key: "later", WakeAt: at(3600), StateSince: at(-1)}, {Key: "soon", WakeAt: at(60), StateSince: at(-99)}},
			want:    []string{"soon", "later"},
		},
		{
			name:    "ties fall back to the key, so the order never flickers",
			section: triage.SectionWorking,
			rows:    []snapshot.Row{{Key: "b", Began: at(-1)}, {Key: "a", Began: at(-1)}},
			want:    []string{"a", "b"},
		},
		{
			name:    "children follow their root, oldest first, however deep",
			section: triage.SectionWorking,
			rows: []snapshot.Row{
				{Key: "grandchild", Parent: "child", Began: at(-20)},
				{Key: "other", Began: at(-5)},
				{Key: "child", Parent: "root", Began: at(-30)},
				{Key: "root", Began: at(-40)},
				{Key: "second", Parent: "root", Began: at(-25)},
			},
			want: []string{"other", "root", "child", "second", "grandchild"},
		},
		{
			name:    "a family ranks by its latest change, so a child just blocked lifts it",
			section: triage.SectionNeedsYou,
			rows: []snapshot.Row{
				{Key: "alone", StateSince: at(-20)},
				{Key: "root", StateSince: at(-90)},
				{Key: "child", Parent: "root", StateSince: at(-1)},
			},
			want: []string{"root", "child", "alone"},
		},
		{
			name:    "a child whose parent is listed elsewhere heads its own family",
			section: triage.SectionWorking,
			rows:    []snapshot.Row{{Key: "orphan", Parent: "gone", Began: at(-10)}, {Key: "a", Began: at(-20)}},
			want:    []string{"orphan", "a"},
		},
		{
			name:    "a loop of parents does not hang",
			section: triage.SectionWorking,
			rows:    []snapshot.Row{{Key: "a", Parent: "b", Began: at(-10)}, {Key: "b", Parent: "a", Began: at(-20)}},
			want:    []string{"a", "b"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sort := tc.sort
			if sort == "" {
				sort = snapshot.SortDefault
			}
			require.Equal(t, tc.want, keys(snapshot.SortRows(tc.section, tc.rows, sort)))
		})
	}
}

func TestNewListsEverySectionInOrder(t *testing.T) {
	snap := snapshot.New("v1", map[triage.Section][]snapshot.Row{
		triage.SectionSettled: {{Key: "s"}},
		triage.SectionPinned:  {{Key: "p"}},
		triage.SectionSnoozed: {{Key: "late", WakeAt: at(90)}, {Key: "early", WakeAt: at(30)}},
	}, snapshot.SortDefault)

	var names []triage.Section
	for _, sec := range snap.Sections {
		names = append(names, sec.Name)
	}
	require.Equal(t, []triage.Section{
		triage.SectionPinned, triage.SectionNeedsYou, triage.SectionActive,
		triage.SectionWorking, triage.SectionSnoozed, triage.SectionSettled,
	}, names)
	require.Equal(t, "v1", snap.WatcherVersion)
	require.Empty(t, snap.Sections[1].Rows)
	require.Equal(t, []string{"early", "late"}, keys(snap.Sections[4].Rows))
	require.Equal(t, []string{"p", "early", "late", "s"}, keys(snap.Rows()))
}

func TestFilter(t *testing.T) {
	snap := snapshot.New("v1", map[triage.Section][]snapshot.Row{
		triage.SectionActive: {
			{Key: "login", Label: "Fix the Login loop", Dir: "/srv/app", Began: at(-10)},
			{Key: "docs", Label: "docs", Dir: "/srv/site", Detail: "Reading README.md", Began: at(-20), PullRequests: []state.PullRequest{
				{URL: "https://github.com/acme/site/pull/885", Number: 885, Title: "Rewrite the install guide"},
			}},
			{Key: "root", Label: "billing", Dir: "/srv/billing", Began: at(-30)},
			{Key: "child", Label: "queue names", Dir: "/srv/billing/q", Parent: "root", Began: at(-25)},
		},
		triage.SectionSettled: {{Key: "old", Label: "old login work", Dir: "/srv/app"}},
	}, snapshot.SortDefault)

	cases := []struct {
		query string
		want  []string
	}{
		{"", []string{"login", "docs", "root", "child", "old"}},
		{"LOGIN", []string{"login", "old"}},
		{"/srv/site", []string{"docs"}},
		{"readme", []string{"docs"}},
		{"#885", []string{"docs"}},
		{"acme/site/pull", []string{"docs"}},
		{"install guide", []string{"docs"}},
		{"queue", []string{"root", "child"}},
		{"billing", []string{"root", "child"}},
		{"nothing like it", []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			filtered := snap.Filter(tc.query)
			require.Equal(t, tc.want, keys(filtered.Rows()))
			require.Len(t, filtered.Sections, len(snap.Sections))
		})
	}
}

func TestFamily(t *testing.T) {
	snap := snapshot.New("v1", map[triage.Section][]snapshot.Row{
		triage.SectionActive: {
			{Key: "root", Began: at(-30)},
			{Key: "grandchild", Parent: "child", Began: at(-10)},
			{Key: "child", Parent: "root", Began: at(-20)},
			{Key: "alone", Began: at(-40)},
		},
	}, snapshot.SortDefault)

	require.Equal(t, []string{"root", "child", "grandchild"}, keys(snap.Family("grandchild")))
	require.Equal(t, []string{"root", "child", "grandchild"}, keys(snap.Family("root")))
	require.Equal(t, []string{"alone"}, keys(snap.Family("alone")))
	require.Nil(t, snap.Family("missing"))
}

func TestFixtureIsSortedAsTheWatcherWouldSortIt(t *testing.T) {
	var snap snapshot.Snapshot
	_, err := state.ReadSnapshotFile("testdata/fixture.json", &snap)
	require.NoError(t, err)

	for _, sec := range snap.Sections {
		require.Equal(t, keys(snapshot.SortRows(sec.Name, sec.Rows, snapshot.SortDefault)), keys(sec.Rows), sec.Name)
	}
}

func TestRowJSON(t *testing.T) {
	row := snapshot.Row{
		Key:           "a1",
		Label:         "flaky",
		Alias:         "flaky",
		Harness:       "codex",
		Status:        triage.StatusBlocked,
		Kind:          snapshot.KindPermission,
		Detail:        "Bash: make test",
		Dir:           "/srv/app",
		RexSession:    "s1",
		RexWindow:     "w1",
		RexBlock:      "b1",
		Unread:        true,
		Read:          true,
		PinnedAt:      at(1),
		WakeAt:        at(2),
		UntilWoken:    true,
		StateSince:    at(3),
		Began:         at(4),
		WaitingOnWork: true,
		PullRequests:  []state.PullRequest{{URL: "https://github.com/acme/app/pull/7", Number: 7, Source: state.SourceBranch}},
		Launched:      true,
		Parent:        "root",
		Closed:        true,
	}

	data, err := json.Marshal(row)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"key": "a1",
		"label": "flaky",
		"alias": "flaky",
		"harness": "codex",
		"status": "blocked",
		"kind": "permission",
		"detail": "Bash: make test",
		"dir": "/srv/app",
		"rex_session": "s1",
		"rex_window": "w1",
		"rex_block": "b1",
		"unread": true,
		"read": true,
		"pinned_at": 1789600001,
		"wake_at": 1789600002,
		"until_woken": true,
		"state_since": 1789600003,
		"began": 1789600004,
		"waiting_on_work": true,
		"pull_requests": [{"url": "https://github.com/acme/app/pull/7", "number": 7, "source": "branch"}],
		"launched": true,
		"parent": "root",
		"closed": true
	}`, string(data))

	var back snapshot.Row
	require.NoError(t, json.Unmarshal(data, &back))
	require.Equal(t, row, back)
}

func TestRowJSONLeavesOutWhatIsUnset(t *testing.T) {
	data, err := json.Marshal(snapshot.Row{Key: "b2", Label: "b2", Harness: "claude", Status: triage.StatusStarting})
	require.NoError(t, err)
	require.JSONEq(t, `{"key": "b2", "label": "b2", "harness": "claude", "status": "starting"}`, string(data))
}

func TestRowJSONRejectsAMalformedRow(t *testing.T) {
	var row snapshot.Row
	err := json.Unmarshal([]byte(`{"began": "yesterday"}`), &row)
	require.ErrorContains(t, err, "reading snapshot row")
}

func TestSnapshotJSON(t *testing.T) {
	snap := snapshot.New("v1.2.3", map[triage.Section][]snapshot.Row{
		triage.SectionActive: {{Key: "a", Label: "a", Harness: "claude", Status: triage.StatusIdle}},
	}, snapshot.SortDefault)

	data, err := json.Marshal(snap)
	require.NoError(t, err)
	require.JSONEq(t, `{"version": "v1.2.3", "sections": [
		{"name": "pinned", "rows": []},
		{"name": "needs_you", "rows": []},
		{"name": "active", "rows": [{"key": "a", "label": "a", "harness": "claude", "status": "idle"}]},
		{"name": "working", "rows": []},
		{"name": "snoozed", "rows": []},
		{"name": "settled", "rows": []}
	]}`, string(data))
}
