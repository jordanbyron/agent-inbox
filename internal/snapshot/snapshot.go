// Package snapshot holds the rows as the list shows them: what the watcher
// writes to snapshot.json and every view reads.
package snapshot

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

// Order is the order the list draws its sections in.
var Order = []triage.Section{
	triage.SectionPinned,
	triage.SectionNeedsYou,
	triage.SectionActive,
	triage.SectionWorking,
	triage.SectionSnoozed,
	triage.SectionSettled,
}

// Snapshot is one moment of the inbox, as the watcher saw it.
type Snapshot struct {
	WatcherVersion string `json:"version"`
	// Latest is the newest agent-inbox release, as the watcher last heard.
	Latest string `json:"latest,omitempty"`
	// Sections are in Order, each with its rows already sorted.
	Sections []Section `json:"sections"`
}

// Section is one section of the list and its rows.
type Section struct {
	Name triage.Section `json:"name"`
	Rows []Row          `json:"rows"`
}

// New groups rows into every section in Order, sorting each by sort. A
// section with no rows is kept, empty.
func New(version string, rows map[triage.Section][]Row, sort Sort) Snapshot {
	s := Snapshot{WatcherVersion: version}
	for _, name := range Order {
		s.Sections = append(s.Sections, Section{Name: name, Rows: SortRows(name, rows[name], sort)})
	}
	return s
}

// Rows returns every row, section by section.
func (s Snapshot) Rows() []Row {
	var all []Row
	for _, sec := range s.Sections {
		all = append(all, sec.Rows...)
	}
	return all
}

// Find returns the row with the given key.
func (s Snapshot) Find(key string) (Row, bool) {
	for _, row := range s.Rows() {
		if row.Key == key {
			return row, true
		}
	}
	return Row{}, false
}

// Kind is what a blocked agent is waiting for.
type Kind string

// The kinds of block OSC 7501 reports.
const (
	KindNone       Kind = ""
	KindPermission Kind = "permission"
	KindQuestion   Kind = "question"
	KindAuth       Kind = "auth"
)

// Row is one agent as the list draws it.
type Row struct {
	// Key is the agent id of a launched agent, or the block id of a found one.
	Key string
	// Label is what the row is called: the alias if one is set, else the
	// agent's name.
	Label   string
	Alias   string
	Harness string
	Status  triage.Status
	Kind    Kind
	// Detail is the msg of the agent's last OSC 7501 report.
	Detail string
	Dir    string
	// RexSession, RexWindow and RexBlock are empty for an agent with no block.
	RexSession    string
	RexWindow     string
	RexBlock      string
	Unread        bool
	Read          bool
	PinnedAt      time.Time
	WakeAt        time.Time
	UntilWoken    bool
	StateSince    time.Time
	Began         time.Time
	WaitingOnWork bool
	// PullRequests has the headline pull request first.
	PullRequests []state.PullRequest
	// Launched is whether agent-inbox started it, so it has a record.
	Launched bool
	// Parent is the key of the agent that started this one, if any.
	Parent string
	// Closed marks a child whose block closed while its root lives.
	Closed bool
}

// Pinned reports whether the row is pinned.
func (r Row) Pinned() bool {
	return !r.PinnedAt.IsZero()
}

// HasBlock reports whether the agent is running in a Rex block.
func (r Row) HasBlock() bool {
	return r.RexBlock != ""
}

// Headline returns the pull request the row leads with.
func (r Row) Headline() (state.PullRequest, bool) {
	if len(r.PullRequests) == 0 {
		return state.PullRequest{}, false
	}
	return r.PullRequests[0], true
}

type rowJSON struct {
	Key           string              `json:"key"`
	Label         string              `json:"label"`
	Alias         string              `json:"alias,omitempty"`
	Harness       string              `json:"harness"`
	Status        triage.Status       `json:"status"`
	Kind          Kind                `json:"kind,omitempty"`
	Detail        string              `json:"detail,omitempty"`
	Dir           string              `json:"dir,omitempty"`
	RexSession    string              `json:"rex_session,omitempty"`
	RexWindow     string              `json:"rex_window,omitempty"`
	RexBlock      string              `json:"rex_block,omitempty"`
	Unread        bool                `json:"unread,omitempty"`
	Read          bool                `json:"read,omitempty"`
	PinnedAt      int64               `json:"pinned_at,omitempty"`
	WakeAt        int64               `json:"wake_at,omitempty"`
	UntilWoken    bool                `json:"until_woken,omitempty"`
	StateSince    int64               `json:"state_since,omitempty"`
	Began         int64               `json:"began,omitempty"`
	WaitingOnWork bool                `json:"waiting_on_work,omitempty"`
	PullRequests  []state.PullRequest `json:"pull_requests,omitempty"`
	Launched      bool                `json:"launched,omitempty"`
	Parent        string              `json:"parent,omitempty"`
	Closed        bool                `json:"closed,omitempty"`
}

// MarshalJSON writes the row with its times as Unix seconds, as
// triage.Entry does.
func (r Row) MarshalJSON() ([]byte, error) {
	return json.Marshal(rowJSON{
		Key:           r.Key,
		Label:         r.Label,
		Alias:         r.Alias,
		Harness:       r.Harness,
		Status:        r.Status,
		Kind:          r.Kind,
		Detail:        r.Detail,
		Dir:           r.Dir,
		RexSession:    r.RexSession,
		RexWindow:     r.RexWindow,
		RexBlock:      r.RexBlock,
		Unread:        r.Unread,
		Read:          r.Read,
		PinnedAt:      unixSeconds(r.PinnedAt),
		WakeAt:        unixSeconds(r.WakeAt),
		UntilWoken:    r.UntilWoken,
		StateSince:    unixSeconds(r.StateSince),
		Began:         unixSeconds(r.Began),
		WaitingOnWork: r.WaitingOnWork,
		PullRequests:  r.PullRequests,
		Launched:      r.Launched,
		Parent:        r.Parent,
		Closed:        r.Closed,
	})
}

// UnmarshalJSON reads a row written by MarshalJSON.
func (r *Row) UnmarshalJSON(data []byte) error {
	var j rowJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return fmt.Errorf("reading snapshot row: %w", err)
	}
	*r = Row{
		Key:           j.Key,
		Label:         j.Label,
		Alias:         j.Alias,
		Harness:       j.Harness,
		Status:        j.Status,
		Kind:          j.Kind,
		Detail:        j.Detail,
		Dir:           j.Dir,
		RexSession:    j.RexSession,
		RexWindow:     j.RexWindow,
		RexBlock:      j.RexBlock,
		Unread:        j.Unread,
		Read:          j.Read,
		PinnedAt:      fromUnixSeconds(j.PinnedAt),
		WakeAt:        fromUnixSeconds(j.WakeAt),
		UntilWoken:    j.UntilWoken,
		StateSince:    fromUnixSeconds(j.StateSince),
		Began:         fromUnixSeconds(j.Began),
		WaitingOnWork: j.WaitingOnWork,
		PullRequests:  j.PullRequests,
		Launched:      j.Launched,
		Parent:        j.Parent,
		Closed:        j.Closed,
	}
	return nil
}

func unixSeconds(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func fromUnixSeconds(s int64) time.Time {
	if s == 0 {
		return time.Time{}
	}
	return time.Unix(s, 0)
}
