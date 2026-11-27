package list

import (
	"strconv"
	"strings"
	"time"

	"github.com/jordanbyron/agent-inbox/internal/theme"

	"charm.land/lipgloss/v2"

	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

var sectionStyles = map[triage.Section]lipgloss.Style{
	sectionTab:             theme.Blue,
	triage.SectionPinned:   theme.Cyan,
	triage.SectionNeedsYou: theme.Red,
	triage.SectionActive:   theme.Yellow,
	triage.SectionWorking:  theme.Yellow,
	triage.SectionSnoozed:  theme.Purple,
	triage.SectionSettled:  theme.Faint,
}

var sectionTitles = map[triage.Section]string{
	sectionTab:             "THIS TAB",
	triage.SectionPinned:   "PINNED",
	triage.SectionNeedsYou: "NEEDS YOU",
	triage.SectionActive:   "ACTIVE",
	triage.SectionWorking:  "WORKING",
	triage.SectionSnoozed:  "SNOOZED",
	triage.SectionSettled:  "SETTLED",
}

var prStateStyles = map[state.PRState]lipgloss.Style{
	state.PRStateOpen:   theme.Green,
	state.PRStateDraft:  theme.Faint,
	state.PRStateMerged: theme.Purple,
	state.PRStateClosed: theme.Red,
}

var prTrouble = map[triage.Health]string{
	triage.HealthFailing:          "✗ checks",
	triage.HealthChangesRequested: "changes requested",
	triage.HealthConflicting:      "conflicts",
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
		hue = theme.Faint
	}
	badge := hue.Underline(true).Hyperlink(pr.URL).Render(name)
	if !named {
		return badge
	}
	badge += hue.Render(" " + strings.ToLower(string(pr.State)))
	if trouble, ok := prTrouble[pr.Health]; ok && !quiet {
		// A conflict dims: it waits on a rebase, not on you.
		troubleHue := theme.Faint
		if pr.Health != triage.HealthConflicting {
			troubleHue = theme.Red
		}
		badge += theme.Dim(" · ") + troubleHue.Render(trouble)
	}
	return badge
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
