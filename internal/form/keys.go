package form

import (
	"regexp"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/rivo/uniseg"

	"github.com/jordanbyron/agent-inbox/internal/editor"
	"github.com/jordanbyron/agent-inbox/internal/images"
	"github.com/jordanbyron/agent-inbox/internal/slash"
)

type action int

const (
	actionNone action = iota
	actionNext
	actionPrev
	actionDown
	actionUp
	actionEnter
	actionStart
	actionStartAndShow
	actionPaste
	actionCancel
	actionQuit
	actionSlipS
	actionSlipO
)

type binding struct {
	keys   []string
	action action
}

var bindings = []binding{
	{keys: []string{"tab"}, action: actionNext},
	{keys: []string{"shift+tab"}, action: actionPrev},
	{keys: []string{"down"}, action: actionDown},
	{keys: []string{"up"}, action: actionUp},
	{keys: []string{"enter"}, action: actionEnter},
	{keys: []string{"ctrl+s"}, action: actionStart},
	{keys: []string{"ctrl+o"}, action: actionStartAndShow},
	{keys: []string{"ctrl+v"}, action: actionPaste},
	{keys: []string{"esc"}, action: actionCancel},
	{keys: []string{"ctrl+c"}, action: actionQuit},
	// Option-s arrives as alt+s, a slip for Ctrl-S worth naming.
	{keys: []string{"alt+s"}, action: actionSlipS},
	{keys: []string{"alt+o"}, action: actionSlipO},
}

func lookup(key string) action {
	for _, b := range bindings {
		if slices.Contains(b.keys, key) {
			return b.action
		}
	}
	return actionNone
}

// A text field spends these keys on its own text instead.
var cycle = map[string]int{"left": -1, "right": 1, "h": -1, "l": 1, "space": 1}

func (m *Model) press(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if m.confirmDiscard {
		return m.pressConfirm(key)
	}
	m.problem = ""
	before, hadQuery := m.menuQuery()
	if m.pressMenu(key) || (m.focus == fieldDir && m.dir.Press(key, msg.Text)) {
		return nil
	}
	cmd := m.perform(lookup(key), msg)
	if after, open := m.menuQuery(); hadQuery && open && after != before {
		m.menuPick, m.hasDismissed = 0, false
	}
	return cmd
}

func (m *Model) perform(a action, msg tea.KeyPressMsg) tea.Cmd {
	switch a {
	case actionStart:
		return m.submit(false)
	case actionStartAndShow:
		return m.submit(true)
	case actionQuit:
		return tea.Quit
	case actionCancel:
		if m.prompt.Empty() {
			return tea.Quit
		}
		m.confirmDiscard = true
	case actionSlipS:
		m.problem = "that's option-s — ctrl-s starts it"
	case actionSlipO:
		m.problem = "that's option-o — ctrl-o starts it and shows its tab"
	case actionPaste:
		m.pasteClipboard()
	case actionNext:
		m.move(1)
	case actionPrev:
		m.move(-1)
	case actionDown:
		if m.focus != fieldPrompt || !m.prompt.Down(m.promptWidth()) {
			m.move(1)
		}
	case actionUp:
		if m.focus != fieldPrompt || !m.prompt.Up(m.promptWidth()) {
			m.move(-1)
		}
	case actionEnter:
		if m.focus == fieldPrompt {
			m.prompt.Insert("\n")
		} else {
			m.move(1)
		}
	default:
		m.edit(msg)
	}
	return nil
}

func (m *Model) edit(msg tea.KeyPressMsg) {
	if buffer := m.buffer(); buffer != nil {
		buffer.Press(msg.String(), msg.Text)
		return
	}
	if step, ok := cycle[msg.String()]; ok && m.choices[m.focus] != nil {
		m.choices[m.focus].step(step)
		if m.focus == fieldHarness {
			m.chooseHarness()
		}
	}
}

func (m *Model) buffer() *editor.Buffer {
	switch m.focus {
	case fieldPrompt:
		return &m.prompt
	case fieldName:
		return &m.name
	default:
		return nil
	}
}

func (m *Model) move(delta int) {
	fields := m.fields()
	i := max(slices.Index(fields, m.focus), 0)
	m.focus = fields[((i+delta)%len(fields)+len(fields))%len(fields)]
}

func (m *Model) pressConfirm(key string) tea.Cmd {
	switch key {
	case "y":
		return tea.Quit
	case "n", "q", "esc":
		m.confirmDiscard = false
	}
	return nil
}

var slashWordAtCursor = regexp.MustCompile(`(?:^|\s)/(\S*)$`)

func (m Model) menuQuery() (string, bool) {
	if m.focus != fieldPrompt {
		return "", false
	}
	match := slashWordAtCursor.FindStringSubmatch(m.prompt.Head())
	if match == nil {
		return "", false
	}
	return match[1], true
}

func (m Model) menu() []slash.Command {
	query, open := m.menuQuery()
	if !open || (m.hasDismissed && m.dismissed == query) {
		return nil
	}
	return slash.Match(m.commands, query)
}

func (m Model) picked() int {
	return min(max(m.menuPick, 0), len(m.menu())-1)
}

func (m *Model) pressMenu(key string) bool {
	items := m.menu()
	if len(items) == 0 {
		return false
	}
	switch key {
	case "up", "ctrl+p":
		m.menuPick = (m.picked() - 1 + len(items)) % len(items)
	case "down", "ctrl+n":
		m.menuPick = (m.picked() + 1) % len(items)
	case "tab", "enter":
		query, _ := m.menuQuery()
		m.prompt.ReplaceBefore(uniseg.GraphemeClusterCount(query)+1, items[m.picked()].String()+" ")
		m.menuPick = 0
	case "esc":
		m.dismissed, _ = m.menuQuery()
		m.hasDismissed = true
	default:
		return false
	}
	return true
}

// paste takes a bracketed paste. An empty one is how a terminal pastes an
// image, so the clipboard is read instead, as Claude Code does on Cmd-V.
func (m *Model) paste(text string) {
	m.problem = ""
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	if text == "" {
		m.pasteClipboard()
		return
	}
	if paths, ok := images.Dropped(text); ok && m.focus == fieldPrompt {
		for i, path := range paths {
			if i > 0 {
				m.prompt.Insert(" ")
			}
			m.prompt.Attach(path, images.IsImage(path))
		}
		return
	}
	m.insert(text)
}

func (m *Model) pasteClipboard() {
	if m.opts.Clipboard == nil {
		m.problem = "no clipboard to read"
		return
	}
	clip, err := images.FromClipboard(m.opts.Clipboard, m.opts.ImageDir, m.opts.Now())
	switch {
	case err != nil:
		m.problem = err.Error()
	case clip.Image != "" && m.focus != fieldPrompt:
		m.problem = "images go in the prompt"
	case clip.Image != "":
		m.prompt.Attach(clip.Image, true)
	case clip.Text != "":
		m.insert(clip.Text)
	default:
		m.problem = "nothing on the clipboard"
	}
}

func (m *Model) insert(text string) {
	switch buffer := m.buffer(); {
	case m.focus == fieldDir:
		m.dir.Paste(text)
	case buffer == nil:
	case m.focus == fieldPrompt:
		buffer.Paste(text)
	default:
		buffer.Insert(strings.ReplaceAll(text, "\n", " "))
	}
}

type choice struct {
	values []string
	labels []string
	pick   int
}

func (c *choice) value() string {
	if c.pick < 0 || c.pick >= len(c.values) {
		return ""
	}
	return c.values[c.pick]
}

func (c *choice) label(i int) string {
	if c.labels != nil {
		return c.labels[i]
	}
	return c.values[i]
}

func (c *choice) step(delta int) {
	if n := len(c.values); n > 0 {
		c.pick = ((c.pick+delta)%n + n) % n
	}
}
