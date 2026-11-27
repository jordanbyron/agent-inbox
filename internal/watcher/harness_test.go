package watcher_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/config"
	"github.com/jordanbyron/agent-inbox/internal/reap"
	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/rex/rextest"
	"github.com/jordanbyron/agent-inbox/internal/snapshot"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
	"github.com/jordanbyron/agent-inbox/internal/watcher"
	"github.com/jordanbyron/agent-inbox/internal/worktree"
)

var t0 = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

const waitFor = 5 * time.Second

// manualClock moves only when a test advances it, and fires every ticker
// once each time it does.
type manualClock struct {
	mu      sync.Mutex
	now     time.Time
	tickers []*manualTicker
}

type manualTicker struct {
	c chan time.Time
}

func (t *manualTicker) C() <-chan time.Time { return t.c }
func (t *manualTicker) Stop()               {}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) NewTicker(time.Duration) watcher.Ticker {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &manualTicker{c: make(chan time.Time, 1)}
	c.tickers = append(c.tickers, t)
	return t
}

func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	for _, t := range c.tickers {
		select {
		case t.c <- c.now:
		default:
		}
	}
}

// ghPR is a pull request as the fake gh describes it.
type ghPR struct {
	state    string
	closedAt time.Time
}

// fakeGH answers the two graphql queries pulls sends, from pull requests a
// test sets, all in repository o/r.
type fakeGH struct {
	mu       sync.Mutex
	branches map[string]int
	prs      map[int]ghPR
	// hangs is how many calls, from the next, hang until their context ends.
	hangs int
}

func newFakeGH() *fakeGH {
	return &fakeGH{branches: map[string]int{}, prs: map[int]ghPR{}}
}

func (g *fakeGH) set(number int, pr ghPR, branch string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.prs[number] = pr
	if branch != "" {
		g.branches[branch] = number
	}
}

var (
	branchArg   = regexp.MustCompile(`^b(\d+)=(.*)$`)
	numberField = regexp.MustCompile(`pr(\d+): pullRequest\(number: \d+\)`)
)

func (g *fakeGH) Run(ctx context.Context, _ string, args ...string) ([]byte, error) {
	g.mu.Lock()
	if g.hangs > 0 {
		g.hangs--
		g.mu.Unlock()
		<-ctx.Done()
		return nil, ctx.Err()
	}
	defer g.mu.Unlock()
	fields := map[string]any{}
	for _, arg := range args {
		if m := branchArg.FindStringSubmatch(arg); m != nil {
			var nodes []any
			if n, ok := g.branches[m[2]]; ok {
				nodes = append(nodes, g.node(n))
			}
			fields["b"+m[1]] = map[string]any{"nodes": nodes}
		}
		if strings.HasPrefix(arg, "query=") {
			for _, m := range numberField.FindAllStringSubmatch(arg, -1) {
				n, _ := strconv.Atoi(m[1])
				if _, ok := g.prs[n]; ok {
					fields["pr"+m[1]] = g.node(n)
				}
			}
		}
	}
	return json.Marshal(map[string]any{"data": map[string]any{"repository": fields}})
}

func (g *fakeGH) node(n int) map[string]any {
	pr := g.prs[n]
	node := map[string]any{
		"number": n, "url": prURL(n), "title": "PR " + strconv.Itoa(n),
		"state": pr.state, "mergeable": "MERGEABLE",
	}
	if !pr.closedAt.IsZero() {
		node["closedAt"] = pr.closedAt.Format(time.RFC3339)
	}
	return node
}

func prURL(n int) string {
	return fmt.Sprintf("https://github.com/o/r/pull/%d", n)
}

// syncBuffer is a log a test can read while the watcher writes it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// harness is one watcher running against a fake Rex server, a fake gh, a
// manual clock and a temporary state directory.
type harness struct {
	t       *testing.T
	rex     *rextest.Fake
	gh      *fakeGH
	clock   *manualClock
	dir     string
	home    string
	store   *state.Store
	log     *syncBuffer
	done    chan error
	cfg     config.Config
	deleter reap.Deleter
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	return &harness{
		t:     t,
		rex:   rextest.New(rex.Server{HostID: "host"}),
		gh:    newFakeGH(),
		clock: &manualClock{now: t0},
		dir:   dir,
		home:  t.TempDir(),
		store: state.New(dir),
		log:   &syncBuffer{},
		cfg:   config.Default(),
	}
}

// start runs the watcher and waits for its first snapshot.
func (h *harness) start() {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h.done = make(chan error, 1)
	opts := watcher.Options{
		Rex:      h.rex,
		GH:       h.gh,
		Git:      worktree.ExecGit{},
		Clock:    h.clock,
		StateDir: h.dir,
		Home:     h.home,
		Config:   h.cfg,
		Version:  "v-test",
		Log:      slog.New(slog.NewTextHandler(h.log, nil)),
		Deleter:  h.deleter,
	}
	go func() { h.done <- watcher.Run(ctx, opts) }()
	h.t.Cleanup(func() {
		cancel()
		select {
		case err := <-h.done:
			require.NoError(h.t, err)
		case <-time.After(waitFor):
			h.t.Error("the watcher did not stop")
		}
	})
	h.waitSnapshot(func(snapshot.Snapshot) bool { return true })
}

func (h *harness) readSnapshot() (snapshot.Snapshot, time.Time, bool) {
	var snap snapshot.Snapshot
	writtenAt, err := h.store.ReadSnapshot(&snap)
	return snap, writtenAt, err == nil
}

// waitSnapshot waits for a snapshot that satisfies ok, and returns it.
func (h *harness) waitSnapshot(ok func(snapshot.Snapshot) bool) snapshot.Snapshot {
	h.t.Helper()
	var last snapshot.Snapshot
	require.Eventually(h.t, func() bool {
		snap, _, read := h.readSnapshot()
		last = snap
		return read && ok(snap)
	}, waitFor, time.Millisecond, "the snapshot never got there; the last one:\n%s\nlog:\n%s", describe(last), h.log)
	return last
}

// waitRow waits for the row with key to satisfy ok, and returns it with
// the section it is in.
func (h *harness) waitRow(key string, ok func(snapshot.Row) bool) (snapshot.Row, string) {
	h.t.Helper()
	var row snapshot.Row
	var section string
	h.waitSnapshot(func(snap snapshot.Snapshot) bool {
		for _, sec := range snap.Sections {
			for _, r := range sec.Rows {
				if r.Key == key && ok(r) {
					row, section = r, string(sec.Name)
					return true
				}
			}
		}
		return false
	})
	return row, section
}

// waitSection waits for the row with key to be listed in section.
func (h *harness) waitSection(key, section string) snapshot.Row {
	h.t.Helper()
	var row snapshot.Row
	h.waitSnapshot(func(snap snapshot.Snapshot) bool {
		for _, sec := range snap.Sections {
			if string(sec.Name) != section {
				continue
			}
			for _, r := range sec.Rows {
				if r.Key == key {
					row = r
					return true
				}
			}
		}
		return false
	})
	return row
}

func (h *harness) waitLog(text string) {
	h.t.Helper()
	require.Eventually(h.t, func() bool { return strings.Contains(h.log.String(), text) },
		waitFor, time.Millisecond, "the log never said %q:\n%s", text, h.log)
}

func (h *harness) state() *state.State {
	h.t.Helper()
	st, err := h.store.Read()
	require.NoError(h.t, err)
	return st
}

// edit changes state.json as a verb would.
func (h *harness) edit(change func(*state.State)) {
	h.t.Helper()
	require.NoError(h.t, h.store.Update(func(st *state.State) error {
		change(st)
		return nil
	}))
}

func (h *harness) entry(key string, change func(e *triage.Entry)) {
	h.t.Helper()
	h.edit(func(st *state.State) {
		e := st.Entries[key]
		change(&e)
		st.Entries[key] = e
	})
}

// tab adds a window holding one block with label, and returns both ids.
func (h *harness) tab(session, label string) (window, block string) {
	h.t.Helper()
	window, err := h.rex.AddWindow(session, label)
	require.NoError(h.t, err)
	block, err = h.rex.AddBlock(session, window, label)
	require.NoError(h.t, err)
	return window, block
}

func (h *harness) status(session, block string, record rex.StatusRecord) {
	h.t.Helper()
	require.NoError(h.t, h.rex.SetStatus(session, block, record))
}

func describe(snap snapshot.Snapshot) string {
	var b strings.Builder
	for _, sec := range snap.Sections {
		for _, r := range sec.Rows {
			fmt.Fprintf(&b, "  %s: %s %q %s prs=%d closed=%v\n", sec.Name, r.Key, r.Label, r.Status, len(r.PullRequests), r.Closed)
		}
	}
	return b.String()
}

// newRepo makes a git repository on main with one commit, then checks out
// branch, as an agent in a fresh worktree would.
func newRepo(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet", "-b", "main"},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "--quiet", "--allow-empty", "-m", "first"},
		{"checkout", "--quiet", "-b", branch},
	} {
		_, err := worktree.ExecGit{}.Run(t.Context(), dir, args...)
		require.NoError(t, err)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	return resolved
}

func fileInfo(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info
}

// heardApp is what the watcher logs once it knows which tab an app shows.
const heardApp = "heard which tab a Rex app shows"

// heartbeat moves the clock to the next heartbeat and waits for its
// snapshot, which comes after everything the watcher had heard by then.
func (h *harness) heartbeat() {
	h.t.Helper()
	h.clock.Advance(watcher.HeartbeatEvery)
	now := h.clock.Now()
	require.Eventually(h.t, func() bool {
		_, at, ok := h.readSnapshot()
		return ok && !at.Before(now)
	}, waitFor, time.Millisecond, "no heartbeat at %s", now)
}
