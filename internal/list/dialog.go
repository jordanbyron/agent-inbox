package list

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/jordanbyron/agent-inbox/internal/snoozetime"
	"github.com/jordanbyron/agent-inbox/internal/state"
)

const dialogWidth = 44

// A dialog claims every key until press clears m.dialog.
type dialog interface {
	view(screenWidth int) []string
	press(m *Model, msg tea.KeyPressMsg) tea.Cmd
}

func boxWidth(screenWidth, fit int) int {
	return min(screenWidth-4, fit)
}

func box(title string, lines []string, outer int) []string {
	inner := max(outer-2, 0)
	top := truncate(title, inner)
	out := []string{"┌" + top + strings.Repeat("─", max(inner-width(top), 0)) + "┐"}
	for _, line := range lines {
		out = append(out, "│ "+pad(line, max(inner-2, 0))+" │")
	}
	return append(out, "└"+strings.Repeat("─", inner)+"┘")
}

func hint(key, what string) string {
	return keyName(key) + "  " + dim(what)
}

func dismissed(msg tea.KeyPressMsg) bool {
	return msg.String() == "esc" || msg.String() == "q"
}

type snoozeChoice struct {
	key   string
	label string
	typed string
}

var snoozeChoices = []snoozeChoice{
	{"1", "15 minutes", "15m"},
	{"2", "1 hour", "1h"},
	{"3", "tomorrow 9am", "tomorrow"},
	{"4", "until I wake it", ""},
	{"5", "custom", ""},
}

type snoozeMenu struct {
	key string
}

func (d snoozeMenu) view(screenWidth int) []string {
	var lines []string
	for _, c := range snoozeChoices {
		lines = append(lines, "  "+keyName(c.key)+"  "+c.label)
	}
	lines = append(lines, "", "  "+hint("esc", "cancel"))
	return box(" Snooze ", lines, boxWidth(screenWidth, dialogWidth))
}

func (d snoozeMenu) press(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	if dismissed(msg) {
		m.dialog = nil
		return nil
	}
	switch msg.String() {
	case "1", "2", "3":
		for _, c := range snoozeChoices {
			if c.key == msg.String() {
				m.snoozeUntil(d.key, c.typed)
			}
		}
	case "4":
		m.dialog = nil
		m.afterSnooze(d.key, m.actions.SnoozeUntilWoken(d.key))
	case "5":
		m.dialog = newPrompt(promptSnooze, d.key, "")
	}
	return nil
}

func (m *Model) snoozeUntil(key, typed string) {
	wake, ok := snoozetime.Parse(typed, m.now())
	if !ok {
		m.say("try 45m, 2h, 3d, 17:00, 5pm, fri, tomorrow or tonight")
		return
	}
	m.dialog = nil
	m.afterSnooze(key, m.actions.Snooze(key, wake))
}

func (m *Model) afterSnooze(key string, err error) {
	if m.succeeded(err) && m.selected == rowStop(key) {
		m.selectNeighbor()
	}
}

type confirmDelete struct {
	key    string
	label  string
	family bool
}

func (d confirmDelete) view(screenWidth int) []string {
	outer := boxWidth(screenWidth, dialogWidth)
	lines := []string{"  Delete " + truncate(d.label, outer-4-width("  Delete ?")) + "?"}
	if d.family {
		lines = append(lines, "  The agents it started, their tabs,", "  worktrees and branches go with it.")
	} else {
		lines = append(lines, "  Its tab, worktree and branch go", "  with it.")
	}
	lines = append(lines, "", "  "+hint("y", "delete it"), "  "+hint("esc", "keep it"))
	return box(" Delete ", lines, outer)
}

func (d confirmDelete) press(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case msg.String() == "y":
		m.dialog = nil
		if m.succeeded(m.actions.Delete(d.key)) && m.selected == rowStop(d.key) {
			m.selectNeighbor()
		}
	case dismissed(msg) || msg.String() == "n":
		m.dialog = nil
	}
	return nil
}

type promptKind int

const (
	promptAlias promptKind = iota
	promptLink
	promptSnooze
)

var promptTitles = map[promptKind]string{
	promptAlias:  " Alias ",
	promptLink:   " Pull request ",
	promptSnooze: " Snooze ",
}

var promptQuestions = map[promptKind]string{
	promptAlias:  "  New alias:",
	promptLink:   "  Pull request URL (empty clears):",
	promptSnooze: "  Wake at (2h, 5pm, fri, tonight):",
}

type prompt struct {
	kind  promptKind
	key   string
	input textinput.Model
}

// The input is sized for a full-width dialog; a narrower screen cuts it.
const promptInputWidth = dialogWidth - 4 - len("  > ") - 1

func newPrompt(kind promptKind, key, value string) *prompt {
	return &prompt{kind: kind, key: key, input: newInput(value, promptInputWidth)}
}

func (d *prompt) view(screenWidth int) []string {
	outer := boxWidth(screenWidth, dialogWidth)
	lines := []string{
		promptQuestions[d.kind], "",
		"  " + d.input.View(), "",
		"  " + keyName("⏎") + " " + dim("save") + " · " + keyName("esc") + " " + dim("cancel"),
	}
	return box(promptTitles[d.kind], lines, outer)
}

// Only esc dismisses a prompt, since q is text here.
func (d *prompt) press(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.dialog = nil
		return nil
	case "enter":
		d.save(m)
		return nil
	}
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	return cmd
}

// save leaves the prompt up when what was typed is refused, so it can be
// fixed rather than typed again.
func (d *prompt) save(m *Model) {
	value := strings.TrimSpace(d.input.Value())
	switch d.kind {
	case promptSnooze:
		m.snoozeUntil(d.key, value)
	case promptAlias:
		if m.succeeded(m.actions.Alias(d.key, value)) {
			m.dialog = nil
		}
	case promptLink:
		if m.succeeded(m.actions.Link(d.key, value)) {
			m.dialog = nil
		}
	}
}

type pickVerb string

const (
	pickOpen pickVerb = "open"
	pickCopy pickVerb = "copy"
)

var pickKeys = map[pickVerb]string{pickOpen: "o", pickCopy: "y"}

const pickerMargin = 6

type picker struct {
	verb pickVerb
	prs  []state.PullRequest
}

func newPicker(verb pickVerb, prs []state.PullRequest) picker {
	return picker{verb: verb, prs: prs[:min(len(prs), 9)]}
}

func (d picker) view(screenWidth int) []string {
	lines := d.lines()
	fit := dialogWidth
	for _, line := range lines {
		fit = max(fit, width(line)+pickerMargin)
	}
	title := " " + strings.ToUpper(string(d.verb[:1])) + string(d.verb[1:]) + " pull request "
	return box(title, lines, boxWidth(screenWidth, fit))
}

func (d picker) lines() []string {
	repos := map[string]bool{}
	for _, pr := range d.prs {
		repos[prRepo(pr)] = true
	}
	var lines []string
	for i, pr := range d.prs {
		name := prName(pr)
		// A bare #190 is ambiguous only once the list spans repos.
		if len(repos) > 1 && pr.Number != 0 {
			name = prRepo(pr) + name
		}
		label := prBadge(pr, name, false)
		if pr.Title != "" {
			label += dim(" · ") + pr.Title
		}
		lines = append(lines, "  "+keyName(strconv.Itoa(i+1))+"  "+label)
	}
	return append(lines, "",
		"  "+hint(pickKeys[d.verb], string(d.verb)+" "+prName(d.prs[0]))+" · "+keyName("esc")+" "+dim("cancel"))
}

func (d picker) press(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if dismissed(msg) {
		m.dialog = nil
		return nil
	}
	choice := -1
	switch {
	case key == pickKeys[d.verb] || key == "enter":
		choice = 0
	case len(key) == 1 && key >= "1" && key <= "9":
		choice = int(key[0] - '1')
	}
	if choice < 0 || choice >= len(d.prs) {
		return nil
	}
	m.dialog = nil
	m.usePullRequest(d.verb, d.prs[choice])
	return nil
}
