package list

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jordanbyron/agent-inbox/internal/snapshot"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

const compactBelow = 60

var liveSections = []triage.Section{
	triage.SectionPinned, triage.SectionNeedsYou, triage.SectionActive, triage.SectionWorking,
}

func (m Model) header(snap snapshot.Snapshot) string {
	brand := " " + cyan.Bold(true).Render("▌ agent-inbox")
	right := m.headerRight(m.width - width(brand) - 3)
	room := m.width - width(brand) - width(right) - 3
	chips := m.chips(snap, false)
	if width(chips) > room {
		chips = m.chips(snap, true)
	}
	if width(chips) > room {
		chips = ""
	}
	return pad(brand+"   "+chips, m.width-width(right)) + right
}

func (m Model) headerRight(room int) string {
	status := m.status()
	switch {
	case status == "":
		return ""
	case width(status) < room:
		return dim(status) + " "
	case room > 1:
		return dim(truncate(status, room-1)) + " "
	default:
		return ""
	}
}

func (m Model) chips(snap snapshot.Snapshot, compact bool) string {
	if m.stale() {
		old := age(m.now().Sub(m.writtenAt))
		if compact {
			return red.Bold(true).Render("⚠ " + old + " old")
		}
		return red.Bold(true).Render("⚠ snapshot " + old + " old — is the watcher running?")
	}
	if !m.loaded {
		return ""
	}
	count := map[triage.Section]int{}
	working, waiting := 0, 0
	for _, sec := range snap.Sections {
		count[sec.Name] += len(sec.Rows)
		if sec.Name != triage.SectionActive && sec.Name != triage.SectionWorking {
			continue
		}
		for _, row := range sec.Rows {
			switch {
			case row.Status == triage.StatusWorking && row.WaitingOnWork:
				waiting++
			case row.Status == triage.StatusWorking:
				working++
			}
		}
	}
	needs := "need you"
	if count[triage.SectionNeedsYou] == 1 {
		needs = "needs you"
	}
	chip := func(n int, glyph, words string, hue lipgloss.Style) string {
		if n == 0 {
			return ""
		}
		if compact {
			return hue.Render(glyph + " " + strconv.Itoa(n))
		}
		return hue.Render(glyph + " " + strconv.Itoa(n) + " " + words)
	}
	chips := slices.DeleteFunc([]string{
		chip(count[triage.SectionPinned], "★", "pinned", cyan.Bold(true)),
		chip(count[triage.SectionNeedsYou], "●", needs, red.Bold(true)),
		chip(working, "✻", "working", yellow),
		chip(waiting, "◌", "waiting", yellow),
		chip(count[triage.SectionSnoozed], "z", "snoozed", purple),
		chip(count[triage.SectionSettled], "◦", "settled", faint),
	}, func(s string) bool { return s == "" })
	if len(snap.Rows()) == 0 {
		chips = append(chips, dim("nothing running"))
	}
	if compact {
		return strings.Join(chips, "  ")
	}
	return strings.Join(chips, dim("  ·  "))
}

func (m Model) footer() string {
	var text string
	switch {
	case m.filtering:
		text = " " + keyName("/") + m.filter.View()
	case m.filter.Value() != "":
		text = " " + keyName("/") + m.filter.Value() + dim("  esc clears")
	default:
		var hints []string
		for _, b := range bindings {
			if b.footerKey != "" {
				hints = append(hints, keyName(b.footerKey)+" "+dim(b.footerHelp))
			}
		}
		text = " " + strings.Join(hints, "  ")
	}
	return pad(text, m.width)
}

type bodyLine struct {
	text   string
	stop   stop
	detail bool
}

func (m Model) body(snap snapshot.Snapshot) []bodyLine {
	message := func(lines ...string) []bodyLine {
		out := []bodyLine{{}, {}}
		for _, line := range lines {
			out = append(out, bodyLine{text: "   " + line})
		}
		return out
	}
	if !m.loaded {
		return message(plain.Bold(true).Render("No snapshot yet."), dim("The watcher writes one once it starts."))
	}
	if len(snap.Rows()) == 0 {
		if q := m.filter.Value(); q != "" {
			return message(plain.Bold(true).Render("Nothing matches “"+q+"”."), dim("Esc clears the filter."))
		}
		return message(plain.Bold(true).Render("Nothing running."),
			dim("Press ")+cyan.Render("n")+dim(" to start an agent, or run one in a Rex tab."))
	}
	var lines []bodyLine
	for _, sec := range snap.Sections {
		if len(sec.Rows) == 0 {
			continue
		}
		lines = append(lines, bodyLine{}, bodyLine{text: m.sectionTitle(sec.Name, len(sec.Rows))})
		if folded(sec.Name, m.expanded) {
			lines = append(lines, bodyLine{text: m.foldLine(sec.Name, len(sec.Rows)), stop: foldStop(sec.Name)})
			continue
		}
		keys := map[string]bool{}
		for _, row := range sec.Rows {
			keys[row.Key] = true
		}
		for _, row := range sec.Rows {
			child := row.Parent != "" && keys[row.Parent]
			for i, text := range m.rowLines(row, sec.Name, child) {
				line := bodyLine{text: text, detail: i > 0}
				if i == 0 {
					line.stop = rowStop(row.Key)
				}
				lines = append(lines, line)
			}
		}
	}
	return lines
}

func (m Model) sectionTitle(section triage.Section, count int) string {
	hue := sectionStyles[section]
	title := " " + sectionTitles[section] + " "
	n := " " + strconv.Itoa(count) + " "
	fill := max(m.width-3-width(title)-width(n), 0)
	return " " + hue.Render("▎") + hue.Bold(true).Render(title) + dim(strings.Repeat("─", fill)) + dim(n)
}

func (m Model) foldLine(section triage.Section, count int) string {
	selected := m.selected == foldStop(section)
	marker := " "
	hint := ""
	if selected {
		marker = yellow.Bold(true).Render("▶")
		hint = dim("   ⏎ or zo to expand")
	}
	summary := "… " + strconv.Itoa(count) + " " + strings.ToLower(sectionTitles[section])
	return " " + marker + " " + dim(summary) + hint
}

func (m Model) rowLines(row snapshot.Row, section triage.Section, child bool) []string {
	live := slices.Contains(liveSections, section) && !row.Closed
	selected := m.selected == rowStop(row.Key)
	quiet := section == triage.SectionSettled || row.Closed || (live && row.Read)
	unread := live && row.Unread
	marker := " "
	switch {
	case selected:
		marker = yellow.Bold(true).Render("▶")
	case unread:
		marker = blue.Render("●")
	}
	indent := ""
	if child {
		indent = "  "
	}
	lead := " " + marker + " " + m.glyph(row, section) + " " + indent

	if m.width < compactBelow {
		pr := m.pullRequestBadge(row, section)
		gap := ""
		if pr != "" {
			gap = "  "
		}
		labelWidth := max(m.width-width(lead)-width(gap)-width(pr)-1, 0)
		label := styleLabel(truncate(row.Label, labelWidth), row, quiet, selected || unread)
		return []string{lead + pad(label, labelWidth) + gap + pr + " "}
	}

	meta := m.meta(row, section)
	chrome := width(lead) + 2 + width(meta) + 2 + 1
	// The project gives way only once the label has given up all its room,
	// so the row never runs past the screen.
	project := truncate(projectName(row.Dir), max(m.width-chrome, 0))
	labelWidth := max(m.width-chrome-width(project), 0)
	label := styleLabel(truncate(row.Label, labelWidth), row, quiet, selected || unread)
	projectHue := cyan
	if quiet {
		projectHue = faint
	}
	first := lead + pad(label, labelWidth) + "  " + meta + "  " + projectHue.Render(project) + " "
	if !live {
		return []string{first}
	}
	detail := row.Detail
	if detail == "" {
		detail = m.shortPath(row.Dir)
	}
	arrow := "       " + indent + "↳ "
	return []string{first, dim(arrow + truncate(detail, max(m.width-width(arrow)-1, 0)))}
}

func projectName(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Base(dir)
}

func (m Model) shortPath(dir string) string {
	if m.home != "" && (dir == m.home || strings.HasPrefix(dir, m.home+"/")) {
		return "~" + strings.TrimPrefix(dir, m.home)
	}
	return dir
}

// Bold marks an unread row, as a mail client does, and the selected one.
// An alias is italic, so a name you gave reads as yours.
func styleLabel(label string, row snapshot.Row, quiet, bold bool) string {
	style := plain
	switch {
	case quiet:
		return dim(label)
	case row.Alias != "":
		style = style.Italic(true)
	}
	return style.Bold(bold).Render(label)
}

func (m Model) glyph(row snapshot.Row, section triage.Section) string {
	switch {
	case section == triage.SectionSnoozed:
		return purple.Render("z")
	case section == triage.SectionSettled || row.Closed:
		return dim("◦")
	}
	switch row.Status {
	case triage.StatusBlocked:
		return red.Bold(true).Render("?")
	case triage.StatusError:
		return red.Bold(true).Render("✗")
	case triage.StatusWorking:
		if row.WaitingOnWork {
			return yellow.Render("◌")
		}
		return m.spinner.View()
	case triage.StatusDone:
		return green.Render("✓")
	case triage.StatusIdle:
		return dim("■")
	default:
		return dim("·")
	}
}

func (m Model) meta(row snapshot.Row, section triage.Section) string {
	now := m.now()
	ago := age(now.Sub(spokeAt(row)))
	var base string
	switch {
	case section == triage.SectionSnoozed:
		wake := "until woken"
		if !row.UntilWoken {
			wake = "wakes in " + age(row.WakeAt.Sub(now))
		}
		base = purple.Render(wake)
		if row.Pinned() {
			base += cyan.Render(" ★")
		}
	case section == triage.SectionSettled:
		base = dim(statusWord(row.Status) + harnessTag(row) + " · " + ago)
	case row.Closed:
		base = dim("closed" + harnessTag(row) + " · " + ago)
	default:
		base = stateBadge(row) + dim(harnessTag(row)+" · "+ago)
	}
	if pr := m.pullRequestBadge(row, section); pr != "" {
		base += dim(" · ") + pr
	}
	return base
}

func (m Model) pullRequestBadge(row snapshot.Row, section triage.Section) string {
	pr, ok := row.Headline()
	if !ok {
		return ""
	}
	badge := prBadge(pr, prName(pr), section == triage.SectionSettled || row.Closed)
	if more := len(row.PullRequests) - 1; more > 0 {
		badge += dim(" · +" + strconv.Itoa(more))
	}
	return badge
}

func spokeAt(row snapshot.Row) time.Time {
	if row.StateSince.IsZero() {
		return row.Began
	}
	return row.StateSince
}

// Claude is the default; any other harness names itself.
func harnessTag(row snapshot.Row) string {
	if row.Harness == "" || row.Harness == "claude" {
		return ""
	}
	return " · " + row.Harness
}

func statusWord(status triage.Status) string {
	if status == triage.StatusError {
		return "failed"
	}
	return string(status)
}

func stateBadge(row snapshot.Row) string {
	switch row.Status {
	case triage.StatusBlocked:
		badge := "needs you"
		if row.Kind != snapshot.KindNone {
			badge += ": " + string(row.Kind)
		}
		return red.Bold(true).Render(badge)
	case triage.StatusError:
		return red.Bold(true).Render(statusWord(row.Status))
	case triage.StatusWorking:
		if row.WaitingOnWork {
			return yellow.Render("waiting")
		}
		return yellow.Render("working")
	case triage.StatusDone:
		return green.Render("done")
	default:
		return dim(string(row.Status))
	}
}

func overlay(lines, box []string, screenWidth int) []string {
	boxWidth := 0
	for _, line := range box {
		boxWidth = max(boxWidth, width(line))
	}
	left := max((screenWidth-boxWidth)/2, 0)
	top := max((len(lines)-len(box))/2, 0)
	out := slices.Clone(lines)
	for i, line := range box {
		y := top + i
		if y >= len(out) {
			break
		}
		under := stripStyles(out[y])
		before := pad(ansi.Truncate(under, left, ""), left)
		after := ansi.TruncateLeft(under, left+boxWidth, "")
		out[y] = pad(dim(before)+pad(line, boxWidth)+dim(after), screenWidth)
	}
	return out
}

func stripStyles(s string) string {
	return ansi.Strip(s)
}
