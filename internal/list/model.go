// Package list is the inbox screen: a Bubble Tea model that draws the
// watcher's snapshot and turns keys into calls on Actions.
package list

import (
	"slices"
	"strings"
	"time"

	"github.com/jordanbyron/agent-inbox/internal/theme"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/jordanbyron/agent-inbox/internal/snapshot"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

// StaleAfter is how old a snapshot can be before the list stops presenting
// its rows as current. The watcher rewrites it every 5 seconds.
const StaleAfter = 15 * time.Second

const (
	reloadEvery = time.Second
	noticeFor   = 4 * time.Second
	chromeRows  = 2
)

// Loader reads the current snapshot and returns when it was written.
type Loader func() (snapshot.Snapshot, time.Time, error)

// Config is what a Model needs from outside.
type Config struct {
	Load    Loader
	Actions Actions
	Now     func() time.Time
	// Home is shortened to ~ in the directories the list shows.
	Home string
}

// ReloadMsg tells the model that the snapshot file changed.
type ReloadMsg struct{}

type tickMsg struct{}

// Model is the list screen.
type Model struct {
	load    Loader
	actions Actions
	now     func() time.Time
	home    string

	snap      snapshot.Snapshot
	loaded    bool
	writtenAt time.Time
	loadErr   error

	width, height int
	top           int
	selected      stop
	// selectedAt is the selection's place among the stops, so that when its
	// row leaves the list the cursor lands on whatever took that place.
	selectedAt int
	expanded   map[triage.Section]bool
	keys       keymap

	filter    textinput.Model
	filtering bool

	dialog      dialog
	notice      string
	noticeUntil time.Time
	spinner     spinner.Model
}

// New returns the list, with the snapshot read once so the first frame has
// rows.
func New(c Config) Model {
	m := Model{
		load:     c.Load,
		actions:  c.Actions,
		now:      c.Now,
		home:     c.Home,
		expanded: map[triage.Section]bool{},
		filter:   newInput("", 0),
		spinner:  spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(theme.Yellow)),
	}
	m.filter.Prompt = ""
	m.reload()
	return m
}

func newInput(value string, width int) textinput.Model {
	in := textinput.New()
	styles := in.Styles()
	styles.Cursor.Blink = false
	in.SetStyles(styles)
	in.SetWidth(width)
	in.SetValue(value)
	in.Focus()
	return in
}

// Init starts the reload tick and the spinner.
func (m Model) Init() tea.Cmd {
	return tea.Batch(tick(), m.spinner.Tick)
}

func tick() tea.Cmd {
	return tea.Tick(reloadEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

// Update handles a key, a resize, a reload or a tick.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case ReloadMsg:
		m.reload()
	case tickMsg:
		m.reload()
		cmd = tick()
	case spinner.TickMsg:
		m.spinner, cmd = m.spinner.Update(msg)
	case tea.KeyPressMsg:
		cmd = m.press(msg)
	}
	m.keepSelection()
	m.scroll()
	return m, cmd
}

// Reading the snapshot is a small local file read, so it runs here rather
// than in a Cmd, and a reload and the frame it causes are one step.
func (m *Model) reload() {
	snap, writtenAt, err := m.load()
	m.loadErr = err
	if err != nil {
		return
	}
	m.snap, m.writtenAt, m.loaded = snap, writtenAt, true
}

func (m Model) stale() bool {
	return m.loaded && m.now().Sub(m.writtenAt) > StaleAfter
}

func (m *Model) press(msg tea.KeyPressMsg) tea.Cmd {
	if m.dialog != nil {
		return m.dialog.press(m, msg)
	}
	if m.filtering {
		return m.pressFilter(msg)
	}
	return m.perform(m.keys.press(msg.String(), m.now()))
}

func (m *Model) perform(a action) tea.Cmd {
	switch a {
	case actionQuit:
		return tea.Quit
	case actionDown:
		m.move(1)
	case actionUp:
		m.move(-1)
	case actionTop:
		m.move(-len(m.stops()))
	case actionBottom:
		m.move(len(m.stops()))
	case actionHalfPageDown:
		m.move(m.page() / 2)
	case actionHalfPageUp:
		m.move(-m.page() / 2)
	case actionPageDown:
		m.move(m.page())
	case actionPageUp:
		m.move(-m.page())
	case actionNextSection:
		m.jumpSection(1)
	case actionPrevSection:
		m.jumpSection(-1)
	case actionActivate:
		m.activate()
	case actionFoldOpen:
		m.setExpanded(true)
	case actionFoldClose:
		m.setExpanded(false)
	case actionFoldToggle:
		if section, ok := m.currentFold(); ok {
			m.setExpanded(!m.expanded[section])
		}
	case actionNew:
		m.succeeded(m.actions.New())
	case actionFilter:
		m.filtering = true
	case actionEscape:
		m.clearFilter()
	default:
		m.performOnRow(a)
	}
	return nil
}

func (m *Model) performOnRow(a action) {
	row, ok := m.selectedRow()
	if !ok {
		return
	}
	switch a {
	case actionTogglePin:
		m.succeeded(m.actions.TogglePin(row.Key))
	case actionWake:
		m.succeeded(m.actions.Wake(row.Key))
	case actionSettle:
		if m.succeeded(m.actions.Settle(row.Key)) {
			m.selectNeighbor()
			m.say("settled — u brings it back")
		}
	case actionSnooze:
		m.dialog = snoozeMenu{key: row.Key}
	case actionAlias:
		m.dialog = newPrompt(promptAlias, row.Key, row.Alias)
	case actionLinkPullRequest:
		m.dialog = newPrompt(promptLink, row.Key, linkedURL(row))
	case actionDelete:
		m.dialog = m.deleteConfirm(row)
	case actionOpenPullRequest:
		m.pickPullRequest(pickOpen, row)
	case actionCopyPullRequest:
		m.pickPullRequest(pickCopy, row)
	}
}

func linkedURL(row snapshot.Row) string {
	for _, pr := range row.PullRequests {
		if pr.Source == state.SourceLinked {
			return pr.URL
		}
	}
	if pr, ok := row.Headline(); ok {
		return pr.URL
	}
	return ""
}

func (m *Model) pickPullRequest(verb pickVerb, row snapshot.Row) {
	switch len(row.PullRequests) {
	case 0:
		m.say("no pull request linked — P sets one")
	case 1:
		m.usePullRequest(verb, row.PullRequests[0])
	default:
		m.dialog = newPicker(verb, row.PullRequests)
	}
}

func (m *Model) usePullRequest(verb pickVerb, pr state.PullRequest) {
	if verb == pickOpen {
		if m.succeeded(m.actions.OpenPullRequest(pr.URL)) {
			m.say("opening " + prName(pr))
		}
		return
	}
	if m.succeeded(m.actions.CopyPullRequest(pr.URL)) {
		m.say("copied " + prName(pr))
	}
}

func (m *Model) deleteConfirm(row snapshot.Row) confirmDelete {
	family := m.snap.Family(row.Key)
	return confirmDelete{key: row.Key, label: family[0].Label, family: len(family) > 1}
}

func (m *Model) activate() {
	if m.selected.isFold() {
		m.setExpanded(true)
		return
	}
	if row, ok := m.selectedRow(); ok {
		m.succeeded(m.actions.Open(row.Key))
	}
}

func (m *Model) succeeded(err error) bool {
	if err != nil {
		m.say(err.Error())
		return false
	}
	return true
}

func (m *Model) say(notice string) {
	m.notice, m.noticeUntil = notice, m.now().Add(noticeFor)
}

func (m Model) status() string {
	if m.notice != "" && m.now().Before(m.noticeUntil) {
		return m.notice
	}
	if m.loadErr != nil {
		return "⚠ " + m.loadErr.Error()
	}
	return ""
}

func (m Model) filtered() snapshot.Snapshot {
	return m.snap.Filter(m.filter.Value())
}

func (m Model) stops() []stop {
	return allStops(m.filtered(), m.expanded)
}

func (m Model) selectedRow() (snapshot.Row, bool) {
	if m.selected.isFold() {
		return snapshot.Row{}, false
	}
	return m.snap.Find(m.selected.key)
}

func (m Model) page() int {
	return max(m.height-chromeRows, 1)
}

func (m *Model) choose(stops []stop, i int) {
	if len(stops) == 0 {
		m.selected, m.selectedAt = stop{}, 0
		return
	}
	i = min(max(i, 0), len(stops)-1)
	m.selected, m.selectedAt = stops[i], i
}

func (m *Model) move(delta int) {
	stops := m.stops()
	i := max(slices.Index(stops, m.selected), 0)
	m.choose(stops, i+delta)
}

func (m *Model) keepSelection() {
	stops := m.stops()
	if i := slices.Index(stops, m.selected); i >= 0 {
		m.selectedAt = i
		return
	}
	m.choose(stops, m.selectedAt)
}

// selectNeighbor steps off a row about to leave the list, so working down
// a section doesn't throw the cursor to the top.
func (m *Model) selectNeighbor() {
	stops := m.stops()
	i := slices.Index(stops, m.selected)
	if i < 0 || len(stops) < 2 {
		return
	}
	rest := slices.Delete(slices.Clone(stops), i, i+1)
	m.choose(rest, i)
}

func (m *Model) jumpSection(dir int) {
	snap := m.filtered()
	heads := sectionHeads(snap, m.expanded)
	if len(heads) == 0 {
		return
	}
	current, _ := sectionOf(snap, m.selected)
	i := slices.IndexFunc(heads, func(h sectionStops) bool { return h.section == current })
	if i < 0 {
		i = -1
	}
	next := heads[((i+dir)%len(heads)+len(heads))%len(heads)]
	m.choose(m.stops(), slices.Index(m.stops(), next.stops[0]))
}

func (m Model) currentFold() (triage.Section, bool) {
	section, ok := sectionOf(m.filtered(), m.selected)
	return section, ok && slices.Contains(foldable, section)
}

// setExpanded lands on the section's first row when it opens and on its
// fold line when it closes, rather than on a stop that just vanished.
func (m *Model) setExpanded(open bool) {
	section, ok := m.currentFold()
	if !ok || m.expanded[section] == open {
		return
	}
	m.expanded[section] = open
	for _, group := range sectionHeads(m.filtered(), m.expanded) {
		if group.section == section {
			m.choose(m.stops(), slices.Index(m.stops(), group.stops[0]))
		}
	}
}

func (m *Model) pressFilter(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.clearFilter()
		return nil
	case "enter":
		m.filtering = false
		if m.filter.Value() == "" {
			m.clearFilter()
		}
		return nil
	case "backspace":
		if m.filter.Value() == "" {
			m.clearFilter()
			return nil
		}
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	return cmd
}

func (m *Model) clearFilter() {
	m.filter.SetValue("")
	m.filtering = false
}

func (m *Model) scroll() {
	lines := m.body(m.filtered())
	view := m.page()
	first := slices.IndexFunc(lines, func(l bodyLine) bool { return l.stop == m.selected })
	if first >= 0 && m.selected != (stop{}) {
		last := first
		for last+1 < len(lines) && lines[last+1].detail {
			last++
		}
		if first-2 < m.top {
			m.top = first - 2
		}
		if last >= m.top+view {
			m.top = last - view + 1
		}
	}
	m.top = min(max(m.top, 0), max(len(lines)-view, 0))
}

// View draws the list across the whole screen.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m Model) render() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	snap := m.filtered()
	body := m.body(snap)
	view := m.page()
	lines := []string{m.header(snap)}
	for i := range view {
		line := ""
		if m.top+i < len(body) {
			line = body[m.top+i].text
		}
		if m.stale() {
			line = theme.Dim(stripStyles(line))
		}
		lines = append(lines, theme.Pad(line, m.width))
	}
	lines = append(lines, m.footer())
	if m.dialog != nil {
		lines = overlay(lines, m.dialog.view(m.width), m.width)
	}
	return strings.Join(lines, "\n")
}
