package list

import (
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

// The hues are claude-inbox's, tuned to Atom One Dark as 256-color indices.
// Bubble Tea brings them down to whatever the terminal and NO_COLOR allow.
var (
	red    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	green  = lipgloss.NewStyle().Foreground(lipgloss.Color("108"))
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("179"))
	blue   = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	purple = lipgloss.NewStyle().Foreground(lipgloss.Color("176"))
	cyan   = lipgloss.NewStyle().Foreground(lipgloss.Color("73"))
	faint  = lipgloss.NewStyle().Faint(true)
	plain  = lipgloss.NewStyle()
)

var sectionStyles = map[triage.Section]lipgloss.Style{
	triage.SectionPinned:   cyan,
	triage.SectionNeedsYou: red,
	triage.SectionActive:   yellow,
	triage.SectionWorking:  yellow,
	triage.SectionSnoozed:  purple,
	triage.SectionSettled:  faint,
}

var sectionTitles = map[triage.Section]string{
	triage.SectionPinned:   "PINNED",
	triage.SectionNeedsYou: "NEEDS YOU",
	triage.SectionActive:   "ACTIVE",
	triage.SectionWorking:  "WORKING",
	triage.SectionSnoozed:  "SNOOZED",
	triage.SectionSettled:  "SETTLED",
}

var prStateStyles = map[state.PRState]lipgloss.Style{
	state.PRStateOpen:   green,
	state.PRStateDraft:  faint,
	state.PRStateMerged: purple,
	state.PRStateClosed: red,
}

var prTrouble = map[triage.Health]string{
	triage.HealthFailing:          "✗ checks",
	triage.HealthChangesRequested: "changes requested",
	triage.HealthConflicting:      "conflicts",
}

func dim(s string) string {
	return faint.Render(s)
}

func keyName(s string) string {
	return cyan.Bold(true).Render(s)
}

func prName(pr state.PullRequest) string {
	if pr.Number == 0 {
		return strings.TrimPrefix(strings.TrimPrefix(pr.URL, "https://"), "www.")
	}
	return "#" + strconv.Itoa(pr.Number)
}

func prRepo(pr state.PullRequest) string {
	rest, ok := strings.CutPrefix(pr.URL, "https://github.com/")
	if !ok {
		return ""
	}
	owner, rest, _ := strings.Cut(rest, "/")
	repo, _, _ := strings.Cut(rest, "/")
	return owner + "/" + repo
}

func prBadge(pr state.PullRequest, name string, quiet bool) string {
	hue, named := prStateStyles[pr.State]
	if quiet || !named {
		hue = faint
	}
	badge := hue.Underline(true).Hyperlink(pr.URL).Render(name)
	if !named {
		return badge
	}
	badge += hue.Render(" " + strings.ToLower(string(pr.State)))
	if trouble, ok := prTrouble[pr.Health]; ok && !quiet {
		// A conflict dims: it waits on a rebase, not on you.
		troubleHue := faint
		if pr.Health != triage.HealthConflicting {
			troubleHue = red
		}
		badge += dim(" · ") + troubleHue.Render(trouble)
	}
	return badge
}

func width(s string) int {
	return ansi.StringWidth(s)
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

func pad(s string, w int) string {
	s = truncate(s, w)
	return s + strings.Repeat(" ", max(w-width(s), 0))
}

func age(d time.Duration) string {
	s := int(d / time.Second)
	switch {
	case s <= 0:
		return "0s"
	case s < 60:
		return strconv.Itoa(s) + "s"
	case s < 3600:
		return strconv.Itoa(s/60) + "m"
	case s < 86400:
		return strconv.Itoa(s/3600) + "h"
	default:
		return strconv.Itoa(s/86400) + "d"
	}
}
