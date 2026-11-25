package form

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jordanbyron/agent-inbox/internal/editor"
	"github.com/jordanbyron/agent-inbox/internal/theme"
	"github.com/jordanbyron/agent-inbox/internal/worktree"
)

const (
	labelWidth    = 12
	valueColumn   = 3 + labelWidth + 1
	minPromptRows = 3
	maxPromptRows = 10
	menuRows      = 6
	maxListRows   = 8
	minListRows   = 3
	maxNameWidth  = 36
	placeholder   = "What should this agent do?"
)

var (
	// The cursor inverts the cell it sits on: a bar between cells would
	// push everything after it a column to the right.
	cursor   = editor.Paint{Cursor: painter(lipgloss.NewStyle().Reverse(true)), Chip: painter(chipStyle)}
	noCursor = editor.Paint{Chip: painter(chipStyle)}

	chipStyle   = theme.Cyan.Bold(true)
	markerStyle = theme.Yellow.Bold(true)
	pickStyle   = theme.Cyan.Bold(true)
	focusedPick = theme.Cyan.Bold(true).Reverse(true)
)

func painter(style lipgloss.Style) func(string) string {
	return func(text string) string { return style.Render(text) }
}

// View draws the form across the whole of its layer.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m Model) render() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	listRows := m.listRows()
	rows, gaps := m.promptLayout(listRows)
	lines := []string{m.title()}
	if gaps > 1 {
		lines = append(lines, "")
	}
	lines = append(lines, m.promptBox(rows)...)
	lines = append(lines, m.menuLines(min(menuRows, m.height-1-len(lines)))...)
	if gaps > 0 {
		lines = append(lines, "")
	}
	for _, key := range m.fields()[1:] {
		lines = append(lines, m.fieldLine(key))
		if key == fieldDir {
			lines = append(lines, m.dirList(listRows)...)
		}
	}
	body := m.height - 1
	lines = lines[:min(len(lines), body)]
	for len(lines) < body {
		lines = append(lines, "")
	}
	for i, line := range lines {
		lines[i] = theme.Pad(line, m.width)
	}
	return strings.Join(append(lines, m.footer()), "\n")
}

// promptLayout shares out the rows the fields and the directory list leave:
// first a gap under the prompt, then one under the title, then the rest to
// the prompt as it grows.
func (m Model) promptLayout(listRows int) (rows, gaps int) {
	spare := m.height - 4 - (len(m.fields()) - 1) - minPromptRows - listRows
	gaps = min(max(spare, 0), 2)
	limit := max(min(minPromptRows+spare-gaps, maxPromptRows), 1)
	return min(max(m.prompt.Rows(m.promptWidth()), minPromptRows), limit), gaps
}

// listRows keeps the list's height steady while it filters, and on a short
// layer takes rows from the prompt to keep a few.
func (m Model) listRows() int {
	if m.focus != fieldDir {
		return 0
	}
	room := m.height - 1 - (1 + 2 + len(m.fields()) - 1)
	return max(min(maxListRows, room-minPromptRows), min(minListRows, room-1), 0)
}

func (m Model) dirList(rows int) []string {
	indent := strings.Repeat(" ", valueColumn-2)
	list := m.dir.Lines(max(m.width-len(indent)-1, 1), rows)
	out := make([]string, rows)
	for i, line := range list {
		out[i] = indent + line
	}
	return out
}

func (m Model) promptWidth() int {
	return max(m.width-6, 1)
}

func (m Model) title() string {
	if m.confirmDiscard {
		return " " + theme.Red.Bold(true).Render("Discard this prompt? (y/n)")
	}
	return " " + theme.Plain.Bold(true).Render("New agent")
}

func (m Model) promptBox(rows int) []string {
	focused := m.focus == fieldPrompt
	edge := theme.Plain
	if focused {
		edge = theme.Cyan
	}
	inner := m.promptWidth()
	paint := noCursor
	if focused {
		paint = cursor
	}
	text, hidden := m.prompt.View(inner, rows, paint)
	if m.prompt.Empty() {
		text = []string{theme.Dim(placeholder)}
		if focused {
			text[0] = cursor.Cursor(" ") + text[0]
		}
	}
	marker, label := edge.Render("─"), theme.Dim(" Prompt ")
	if focused {
		marker, label = markerStyle.Render("▶"), theme.Plain.Bold(true).Render(" Prompt ")
	}
	more := ""
	if hidden > 0 {
		more = theme.Dim(" ↑ " + strconv.Itoa(hidden) + " more ")
	}
	fill := max(m.width-4-1-theme.Width(label)-theme.Width(more), 0)
	out := []string{" " + edge.Render("┌") + marker + label + edge.Render(strings.Repeat("─", fill)) + more + edge.Render("┐")}
	for i := range rows {
		line := ""
		if i < len(text) {
			line = text[i]
		}
		// A wrapped row keeps its trailing space, which may run one column
		// past the text into the margin.
		out = append(out, " "+edge.Render("│")+" "+theme.Pad(line, inner+1)+edge.Render("│"))
	}
	return append(out, " "+edge.Render("└"+strings.Repeat("─", max(m.width-4, 0))+"┘"))
}

// menuLines keep the pick in view, and count what is cut on the last row
// rather than spend a row of their own on it.
func (m Model) menuLines(room int) []string {
	items := m.menu()
	if len(items) == 0 || room <= 0 {
		return nil
	}
	pick := m.picked()
	first := max(pick-room+1, 0)
	shown := items[first:min(first+room, len(items))]
	more := ""
	if left := len(items) - first - len(shown); left > 0 {
		more = "  +" + strconv.Itoa(left) + " more"
	}
	nameWidth := 0
	for _, c := range shown {
		nameWidth = max(nameWidth, theme.Width(c.String()))
	}
	nameWidth = min(nameWidth, maxNameWidth)
	var out []string
	for i, c := range shown {
		on := first+i == pick
		last := i == len(shown)-1
		marker, name, desc := "  ", pickStyle.Render(theme.Pad(c.String(), nameWidth)), theme.Faint
		if on {
			marker, name, desc = markerStyle.Render("▶")+" ", focusedPick.Render(theme.Pad(c.String(), nameWidth)), theme.Plain
		}
		room := m.width - 4 - nameWidth - 3
		if last {
			room -= theme.Width(more)
		}
		line := "   " + marker + name + "  " + desc.Render(theme.Truncate(c.Description, room))
		if last && more != "" {
			line = theme.Pad(line, m.width-1-theme.Width(more)) + theme.Dim(more)
		}
		out = append(out, line)
	}
	return out
}

func (m Model) fieldLine(key fieldKey) string {
	focused := m.focus == key
	marker, label := " ", theme.Dim(theme.Pad(fieldLabels[key], labelWidth))
	if focused {
		marker, label = markerStyle.Render("▶"), theme.Plain.Bold(true).Render(theme.Pad(fieldLabels[key], labelWidth))
	}
	return " " + marker + " " + label + " " + m.fieldValue(key, max(m.width-valueColumn-1, 1))
}

func (m Model) fieldValue(key fieldKey, width int) string {
	focused := m.focus == key
	paint := noCursor
	if focused {
		paint = cursor
	}
	switch key {
	case fieldName:
		if m.name.Empty() {
			hint := theme.Dim(theme.Truncate(worktree.NameFromPrompt(m.prompt.Expand(m.mention)), width-1))
			if focused {
				return paint.Cursor(" ") + hint
			}
			return hint
		}
		return m.name.Row(width, paint)
	case fieldDir:
		return m.dir.Row(width, paint)
	default:
		return choiceRow(m.choices[key], width, focused)
	}
}

// choiceRow marks the pick with brackets as well as color, so it reads on
// a terminal without color. A row too long for width scrolls to the pick.
func choiceRow(c *choice, width int, focused bool) string {
	items := make([]string, len(c.values))
	for i := range c.values {
		items[i] = " " + c.label(i) + " "
		if i == c.pick {
			items[i] = "[" + c.label(i) + "]"
		}
	}
	first, last := 0, len(items)
	for first < c.pick && spanWidth(items[first:c.pick+1], first > 0, false) > width {
		first++
	}
	for last > c.pick+1 && spanWidth(items[first:last], first > 0, last < len(items)) > width {
		last--
	}
	var parts []string
	if first > 0 {
		parts = append(parts, theme.Dim("…"))
	}
	for i := first; i < last; i++ {
		switch {
		case i == c.pick && focused:
			parts = append(parts, focusedPick.Render(items[i]))
		case i == c.pick:
			parts = append(parts, pickStyle.Render(items[i]))
		default:
			parts = append(parts, theme.Dim(items[i]))
		}
	}
	if last < len(items) {
		parts = append(parts, theme.Dim("…"))
	}
	return theme.Truncate(strings.Join(parts, " "), width)
}

func spanWidth(items []string, before, after bool) int {
	w := theme.Width(strings.Join(items, " "))
	if before {
		w += 2
	}
	if after {
		w += 2
	}
	return w
}

func (m Model) footer() string {
	var text string
	switch {
	case m.confirmDiscard:
		text = m.hints("y", "discard", "esc", "keep editing")
	case m.problem != "":
		text = theme.Red.Render(m.problem)
	case len(m.menu()) > 0:
		text = m.hints("↑↓", "choose", "⇥ ⏎", "pick", "esc", "close")
	default:
		text = m.keyHints()
	}
	return theme.Pad(" "+text, m.width)
}

func (m Model) keyHints() string {
	if m.focus == fieldDir {
		pairs := append(m.dir.Hints(), "^S", "start", "^O", "start & show")
		if !m.dir.InList() {
			pairs = append(pairs, "esc", "cancel")
		}
		return m.hints(pairs...)
	}
	var pairs []string
	switch {
	case m.focus == fieldPrompt:
		pairs = []string{"⏎", "newline", "^V", "image"}
	case m.buffer() == nil:
		pairs = []string{"← →", "change"}
	}
	pairs = append(pairs, "^S", "start", "^O", "start & show", "esc", "cancel", "⇥", "next")
	return m.hints(pairs...)
}

// hints leaves out whole hints from the end that the footer has no room
// for, rather than cut one off partway.
func (m Model) hints(pairs ...string) string {
	out := ""
	for i := 0; i+1 < len(pairs); i += 2 {
		next := theme.Hint(pairs[i], pairs[i+1])
		if out != "" {
			next = "  " + next
		}
		if theme.Width(out+next) > m.width-2 {
			break
		}
		out += next
	}
	return out
}
