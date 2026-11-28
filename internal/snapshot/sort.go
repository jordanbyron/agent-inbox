package snapshot

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jordanbyron/agent-inbox/internal/triage"
)

// Sort is the config's `sort` setting.
type Sort string

// The sorts the config offers.
const (
	SortDefault  Sort = "default"
	SortActivity Sort = "activity"
)

// SortRows returns one section's rows in the order the list shows them.
// A family moves as one: its root, then every other member oldest first,
// however deep the family goes.
func SortRows(section triage.Section, rows []Row, sort Sort) []Row {
	families := familiesOf(rows)
	slices.SortStableFunc(families, func(a, b family) int {
		return cmp.Or(compareFamilies(section, sort, a, b), cmp.Compare(a.root().Key, b.root().Key))
	})
	sorted := make([]Row, 0, len(rows))
	for _, f := range families {
		sorted = append(sorted, f.members...)
	}
	return sorted
}

func compareFamilies(section triage.Section, sort Sort, a, b family) int {
	ra, rb := a.root(), b.root()
	if sort == SortActivity && section != triage.SectionSnoozed {
		return newestFirst(a.stateSince(), b.stateSince())
	}
	switch section {
	case triage.SectionPinned:
		return newestFirst(ra.PinnedAt, rb.PinnedAt)
	case triage.SectionNeedsYou, triage.SectionSettled:
		return newestFirst(a.stateSince(), b.stateSince())
	case triage.SectionActive:
		return cmp.Or(falseFirst(finished(ra), finished(rb)), newestFirst(ra.Began, rb.Began))
	case triage.SectionWorking:
		return newestFirst(ra.Began, rb.Began)
	case triage.SectionSnoozed:
		return cmp.Or(falseFirst(ra.UntilWoken, rb.UntilWoken), ra.WakeAt.Compare(rb.WakeAt))
	default:
		return 0
	}
}

func finished(r Row) bool {
	return r.Status == triage.StatusDone || r.Status == triage.StatusIdle
}

func newestFirst(a, b time.Time) int {
	return b.Compare(a)
}

func falseFirst(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	default:
		return -1
	}
}

type family struct {
	members []Row
}

func (f family) root() Row {
	return f.members[0]
}

// stateSince is the latest change in the family, so a child that has just
// become blocked brings its family to the top of Needs you.
func (f family) stateSince() time.Time {
	var latest time.Time
	for _, m := range f.members {
		if m.StateSince.After(latest) {
			latest = m.StateSince
		}
	}
	return latest
}

func familiesOf(rows []Row) []family {
	byKey := map[string]Row{}
	for _, r := range rows {
		byKey[r.Key] = r
	}
	var roots []string
	members := map[string][]Row{}
	for _, r := range rows {
		root := rootKey(r, byKey)
		if root == r.Key {
			roots = append(roots, root)
			continue
		}
		members[root] = append(members[root], r)
	}
	families := make([]family, 0, len(roots))
	for _, key := range roots {
		children := members[key]
		slices.SortStableFunc(children, func(a, b Row) int {
			return cmp.Or(a.Began.Compare(b.Began), cmp.Compare(a.Key, b.Key))
		})
		families = append(families, family{members: append([]Row{byKey[key]}, children...)})
	}
	return families
}

// rootKey follows a row's parents for as long as they are in rows. A parent
// listed elsewhere, or a loop, leaves the row heading its own family.
func rootKey(r Row, byKey map[string]Row) string {
	start := r.Key
	seen := map[string]bool{r.Key: true}
	for r.Parent != "" {
		parent, ok := byKey[r.Parent]
		if !ok {
			return r.Key
		}
		if seen[parent.Key] {
			return start
		}
		seen[parent.Key] = true
		r = parent
	}
	return r.Key
}

// Family returns the family of the row with key, root first, as its section
// lists it, and nil when no row has that key.
func (s Snapshot) Family(key string) []Row {
	for _, sec := range s.Sections {
		for _, f := range familiesOf(sec.Rows) {
			if slices.ContainsFunc(f.members, func(r Row) bool { return r.Key == key }) {
				return f.members
			}
		}
	}
	return nil
}

// Filter keeps the families with a member that matches query, whole, in
// their sections. An empty query keeps everything.
func (s Snapshot) Filter(query string) Snapshot {
	if query == "" {
		return s
	}
	out := Snapshot{WatcherVersion: s.WatcherVersion, Latest: s.Latest}
	for _, sec := range s.Sections {
		kept := Section{Name: sec.Name, Rows: []Row{}}
		for _, f := range familiesOf(sec.Rows) {
			if slices.ContainsFunc(f.members, func(r Row) bool { return r.Matches(query) }) {
				kept.Rows = append(kept.Rows, f.members...)
			}
		}
		out.Sections = append(out.Sections, kept)
	}
	return out
}

// Matches reports whether query appears, ignoring case, in the row's label,
// directory or detail, or in a pull request's number, URL or title, so
// `#885` finds the agent behind a review.
func (r Row) Matches(query string) bool {
	q := strings.ToLower(query)
	fields := []string{r.Label, r.Dir, r.Detail}
	for _, pr := range r.PullRequests {
		if pr.Number != 0 {
			fields = append(fields, "#"+strconv.Itoa(pr.Number))
		}
		fields = append(fields, pr.URL, pr.Title)
	}
	return slices.ContainsFunc(fields, func(f string) bool {
		return strings.Contains(strings.ToLower(f), q)
	})
}
