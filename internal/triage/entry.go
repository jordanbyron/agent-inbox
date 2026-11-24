package triage

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// Entry is what the inbox remembers about one agent between observations.
// The zero Entry is one nothing has been recorded for yet.
type Entry struct {
	Alias      string
	LinkedPR   string
	WakeAt     time.Time
	UntilWoken bool
	LastState  Status
	StateSince time.Time
	PinnedAt   time.Time
	SettledAt  time.Time
	// AcknowledgedAt is when the agent's tab was last seen, and lapses when
	// its state changes.
	AcknowledgedAt time.Time
	// LastVisitedAt is when the agent's tab was last seen, and never lapses.
	LastVisitedAt time.Time
	RevivedAt     time.Time
	// RevivedPRs are the URLs of the pull requests that had resolved when
	// the agent was last woken or restored.
	RevivedPRs []string
	// PRHealth is empty while nothing is wrong.
	PRHealth Health
}

// Observe records what the agent is doing now: a change of state or of
// pull request trouble, and the end of any snooze that change or the clock
// brings.
func (e *Entry) Observe(a Agent, now time.Time) {
	at := stamp(now)
	if e.LastState != a.Status {
		// Only becoming blocked or failing wakes a snooze, not PR trouble:
		// an agent blocked when you snoozed it stays snoozed.
		if e.LastState != "" && a.NeedsYou() {
			e.clearSnooze()
		}
		e.LastState = a.Status
		e.StateSince = at
	}
	if pr, ok := a.headline(); ok && pr.Health != HealthUnknown && pr.Health != e.prHealthOrClean() {
		e.PRHealth = pr.Health
		if pr.Health == HealthClean {
			e.PRHealth = HealthUnknown
		}
		e.StateSince = at
	}
	if e.Woken(now) {
		e.clearSnooze()
	}
	if !e.Acknowledged() {
		e.AcknowledgedAt = time.Time{}
	}
}

// Snooze hides the agent until the given time.
func (e *Entry) Snooze(until time.Time) {
	e.WakeAt = stamp(until)
	e.UntilWoken = false
}

// SnoozeUntilWoken hides the agent until it is woken or needs you.
func (e *Entry) SnoozeUntilWoken() {
	e.WakeAt = time.Time{}
	e.UntilWoken = true
}

// Wake ends a snooze and a settle, and holds off auto-settle until a pull
// request resolves that had not resolved by now.
func (e *Entry) Wake(a Agent, now time.Time) {
	e.clearSnooze()
	e.SettledAt = time.Time{}
	e.RevivedAt = stamp(now)
	e.RevivedPRs = nil
	for _, pr := range a.PullRequests {
		if pr.Resolved() && !slices.Contains(e.RevivedPRs, pr.URL) {
			e.RevivedPRs = append(e.RevivedPRs, pr.URL)
		}
	}
}

// Settle records when the agent was settled, ending any snooze and any
// earlier wake. The section follows the block, which the caller closes.
func (e *Entry) Settle(now time.Time) {
	e.SettledAt = stamp(now)
	e.clearSnooze()
	e.RevivedAt = time.Time{}
	e.RevivedPRs = nil
}

// Acknowledge records that the agent's tab was seen.
func (e *Entry) Acknowledge(now time.Time) {
	e.AcknowledgedAt = stamp(now)
	e.LastVisitedAt = stamp(now)
}

// TogglePin pins the agent, or unpins it if it was pinned.
func (e *Entry) TogglePin(now time.Time) {
	if e.Pinned() {
		e.PinnedAt = time.Time{}
	} else {
		e.PinnedAt = stamp(now)
	}
}

// Pinned reports whether the agent is pinned.
func (e Entry) Pinned() bool {
	return !e.PinnedAt.IsZero()
}

// Woken reports whether a timed snooze has run out.
func (e Entry) Woken(now time.Time) bool {
	return !e.WakeAt.IsZero() && !now.Before(e.WakeAt)
}

// Snoozed reports whether the agent is snoozed at now.
func (e Entry) Snoozed(now time.Time) bool {
	return e.UntilWoken || (!e.WakeAt.IsZero() && !e.Woken(now))
}

// Acknowledged reports whether the agent's tab was seen since its state
// last changed.
func (e Entry) Acknowledged() bool {
	return e.unchangedSince(e.AcknowledgedAt)
}

func (e Entry) unchangedSince(at time.Time) bool {
	return !at.IsZero() && !e.StateSince.After(at)
}

func (e Entry) prHealthOrClean() Health {
	if e.PRHealth == HealthUnknown {
		return HealthClean
	}
	return e.PRHealth
}

func (e *Entry) clearSnooze() {
	e.WakeAt = time.Time{}
	e.UntilWoken = false
}

// stamp keeps whole seconds, as the JSON does, so a rule that compares two
// stamps reads the same before and after a save.
func stamp(t time.Time) time.Time {
	return t.Truncate(time.Second)
}

type entryJSON struct {
	Alias          string   `json:"alias,omitempty"`
	PR             string   `json:"pr,omitempty"`
	WakeAt         int64    `json:"wake_at,omitempty"`
	UntilWoken     bool     `json:"until_woken,omitempty"`
	LastState      Status   `json:"last_state,omitempty"`
	StateSince     int64    `json:"state_since,omitempty"`
	PinnedAt       int64    `json:"pinned_at,omitempty"`
	SettledAt      int64    `json:"settled_at,omitempty"`
	AcknowledgedAt int64    `json:"acknowledged_at,omitempty"`
	LastVisitedAt  int64    `json:"last_visited_at,omitempty"`
	RevivedAt      int64    `json:"revived_at,omitempty"`
	RevivedPRs     []string `json:"revived_prs,omitempty"`
	PRHealth       Health   `json:"pr_health,omitempty"`
}

// MarshalJSON writes the entry with its times as Unix seconds.
func (e Entry) MarshalJSON() ([]byte, error) {
	return json.Marshal(entryJSON{
		Alias:          e.Alias,
		PR:             e.LinkedPR,
		WakeAt:         unixSeconds(e.WakeAt),
		UntilWoken:     e.UntilWoken,
		LastState:      e.LastState,
		StateSince:     unixSeconds(e.StateSince),
		PinnedAt:       unixSeconds(e.PinnedAt),
		SettledAt:      unixSeconds(e.SettledAt),
		AcknowledgedAt: unixSeconds(e.AcknowledgedAt),
		LastVisitedAt:  unixSeconds(e.LastVisitedAt),
		RevivedAt:      unixSeconds(e.RevivedAt),
		RevivedPRs:     e.RevivedPRs,
		PRHealth:       e.PRHealth,
	})
}

// UnmarshalJSON reads an entry written by MarshalJSON.
func (e *Entry) UnmarshalJSON(data []byte) error {
	var j entryJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return fmt.Errorf("reading triage entry: %w", err)
	}
	*e = Entry{
		Alias:          j.Alias,
		LinkedPR:       j.PR,
		WakeAt:         fromUnixSeconds(j.WakeAt),
		UntilWoken:     j.UntilWoken,
		LastState:      j.LastState,
		StateSince:     fromUnixSeconds(j.StateSince),
		PinnedAt:       fromUnixSeconds(j.PinnedAt),
		SettledAt:      fromUnixSeconds(j.SettledAt),
		AcknowledgedAt: fromUnixSeconds(j.AcknowledgedAt),
		LastVisitedAt:  fromUnixSeconds(j.LastVisitedAt),
		RevivedAt:      fromUnixSeconds(j.RevivedAt),
		RevivedPRs:     j.RevivedPRs,
		PRHealth:       j.PRHealth,
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
