// Package list is the inbox screen: a Bubble Tea model that draws the
// watcher's snapshot and turns keys into calls on Actions.
package list

import (
	"fmt"
	"os/exec"
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
	"github.com/jordanbyron/agent-inbox/internal/upgrade"
)

// StaleAfter is how old a snapshot can be before the list stops presenting
// its rows as current. The watcher rewrites it every 5 seconds.
const StaleAfter = 15 * time.Second

const (
	reloadEvery  = time.Second
	upgradeEvery = 5 * time.Second
	noticeFor    = 4 * time.Second
	chromeRows   = 2
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
	// Restart starts the watcher again, for a snapshot gone stale while
	// the list is on screen. Nil leaves the watcher alone.
	Restart func() error
	// Modal makes the list close once it shows an agent, and on Esc, as
	// one in a Rex floating layer should.
	Modal bool
	// Tab is the Rex window the list sits in. Its agents lead the list.
	Tab string
	// Version is this binary's. When Installed reports another, the list
	// offers to relaunch. A nil Installed never does.
	Version   string
	Installed func() (string, error)
	// Upgrade installs the newest release over this binary. Nil leaves the
	// pill saying a release is out, with nothing to press.
	Upgrade func() error
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
	restart func() error
	modal   bool
	tab     string

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
	// busy names the action running in the background, while one is.
	busy string

	restarting  bool
	restartedAt time.Time
	restartErr  error

	version     string
	installed   func() (string, error)
	upgrade     func() error
	newer       string
	checking    bool
	checkedAt   time.Time
	relaunching bool
}

// New returns the list, with the snapshot read once so the first frame has
// rows.
func New(c Config) Model {
	m := Model{
		load:      c.Load,
		actions:   c.Actions,
		now:       c.Now,
		home:      c.Home,
		restart:   c.Restart,
		modal:     c.Modal,
		tab:       c.Tab,
		version:   c.Version,
		installed: c.Installed,
		upgrade:   c.Upgrade,
		expanded:  map[triage.Section]bool{},
		filter:    newInput("", 0),
		spinner:   spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(theme.Yellow)),
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
		cmd = tea.Batch(tick(), m.restartIfStale(), m.checkInstalled())
	case installedMsg:
		m.checking = false
		if msg.err == nil {
			m.newer = ""
			if msg.version != m.version {
				m.newer = msg.version
			}
		}
	case restartMsg:
		m.restarted(msg.err)
	case spinner.TickMsg:
		m.spinner, cmd = m.spinner.Update(msg)
	case tea.KeyPressMsg:
		cmd = m.press(msg)
	case doneMsg:
		cmd = m.finish(msg)
	case formMsg:
		cmd = m.runForm(msg)
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

// restartMsg says how starting the watcher again went.
type restartMsg struct{ err error }

// restartIfStale starts the watcher again when its snapshot has gone stale,
// at most once in StaleAfter, so one that won't start isn't hammered.
func (m *Model) restartIfStale() tea.Cmd {
	if m.restart == nil || m.restarting || !m.stale() || m.now().Sub(m.restartedAt) < StaleAfter {
		return nil
	}
	m.restarting, m.restartedAt = true, m.now()
	restart := m.restart
	return func() tea.Msg { return restartMsg{err: restart()} }
}

func (m *Model) restarted(err error) {
	m.restarting, m.restartErr = false, err
	if err != nil {
		m.say(err.Error())
		return
	}
	m.say("started the watcher again")
	m.reload()
}

// installedMsg is the version of the binary installed now.
type installedMsg struct {
	version string
	err     error
}

func (m *Model) checkInstalled() tea.Cmd {
	if m.installed == nil || m.checking || m.now().Sub(m.checkedAt) < upgradeEvery {
		return nil
	}
	m.checking, m.checkedAt = true, m.now()
	installed := m.installed
	return func() tea.Msg {
		version, err := installed()
		return installedMsg{version: version, err: err}
	}
}

// released reports whether a newer release is out than the one running
// and installed.
func (m Model) released() bool {
	return m.newer == "" && upgrade.Newer(m.snap.Latest, m.version)
}

// Relaunching reports whether the list quit to make way for the newer
// binary, which the caller then runs in its place.
func (m Model) Relaunching() bool {
	return m.relaunching
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
	case actionUpgrade:
		if !m.released() || m.upgrade == nil {
			return nil
		}
		upgrade := m.upgrade
		return m.run("upgrading agent-inbox", func(Actions) error { return upgrade() }, afterAction{recheck: true})
	case actionRelaunch:
		if m.newer == "" {
			return nil
		}
		m.relaunching = true
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
		return m.activate()
	case actionFoldOpen:
		m.setExpanded(true)
	case actionFoldClose:
		m.setExpanded(false)
	case actionFoldToggle:
		if section, ok := m.currentFold(); ok {
			m.setExpanded(!m.expanded[section])
		}
	case actionNew:
		return m.openForm()
	case actionFilter:
		m.filtering = true
	case actionEscape:
		if m.modal && m.filter.Value() == "" {
			return tea.Quit
		}
		m.clearFilter()
	default:
		return m.performOnRow(a)
	}
	return nil
}

func (m *Model) performOnRow(a action) tea.Cmd {
	row, ok := m.selectedRow()
	if !ok {
		return nil
	}
	key := row.Key
	switch a {
	case actionTogglePin:
		return m.run("pinning", func(a Actions) error { return a.TogglePin(key) }, afterAction{})
	case actionWake:
		return m.run("waking", func(a Actions) error { return a.Wake(key) }, afterAction{})
	case actionSettle:
		return m.run("settling", func(a Actions) error { return a.Settle(key) },
			afterAction{leaving: key, notice: "settled — u brings it back"})
	case actionSnooze:
		m.dialog = snoozeMenu{key: row.Key}
	case actionAlias:
		m.dialog = newPrompt(promptAlias, row.Key, row.Alias)
	case actionLinkPullRequest:
		m.dialog = newPrompt(promptLink, row.Key, linkedURL(row))
	case actionDelete:
		m.dialog = m.deleteConfirm(row)
	case actionOpenPullRequest:
		return m.pickPullRequest(pickOpen, row)
	case actionCopyPullRequest:
		return m.pickPullRequest(pickCopy, row)
	}
	return nil
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

func (m *Model) pickPullRequest(verb pickVerb, row snapshot.Row) tea.Cmd {
	switch len(row.PullRequests) {
	case 0:
		m.say("no pull request linked — P sets one")
	case 1:
		return m.usePullRequest(verb, row.PullRequests[0])
	default:
		m.dialog = newPicker(verb, row.PullRequests)
	}
	return nil
}

func (m *Model) usePullRequest(verb pickVerb, pr state.PullRequest) tea.Cmd {
	if verb == pickOpen {
		return m.run("opening", func(a Actions) error { return a.OpenPullRequest(pr.URL) },
			afterAction{notice: "opening " + prName(pr)})
	}
	return m.run("copying", func(a Actions) error { return a.CopyPullRequest(pr.URL) },
		afterAction{notice: "copied " + prName(pr)})
}

func (m *Model) deleteConfirm(row snapshot.Row) confirmDelete {
	family := m.snap.Family(row.Key)
	return confirmDelete{key: row.Key, label: family[0].Label, family: len(family) > 1}
}

func (m *Model) activate() tea.Cmd {
	if m.selected.isFold() {
		m.setExpanded(true)
		return nil
	}
	row, ok := m.selectedRow()
	if !ok {
		return nil
	}
	return m.run("showing", func(a Actions) error { return a.Open(row.Key) }, afterAction{quit: m.modal})
}

// doneMsg says how an action run by run went.
type doneMsg struct {
	err  error
	then afterAction
}

// afterAction is what the list does once an action succeeds.
type afterAction struct {
	notice string
	// leaving is the row the action takes out of its section, so the
	// cursor steps off it rather than follow it.
	leaving string
	// dialog closes when the action succeeds, and stays up to be fixed
	// when it is refused.
	dialog dialog
	// quit closes the list when the action succeeds.
	quit bool
	// recheck asks at once what binary is installed.
	recheck bool
}

// run calls an action in a Cmd, since one that goes to Rex or git can take
// a second, and says what it is doing meanwhile. One runs at a time.
func (m *Model) run(doing string, call func(Actions) error, then afterAction) tea.Cmd {
	if m.busy != "" {
		return nil
	}
	m.busy, m.notice = doing, ""
	actions := m.actions
	return func() tea.Msg { return doneMsg{err: call(actions), then: then} }
}

// formMsg is what Actions.New returned.
type formMsg struct {
	cmd *exec.Cmd
	err error
}

func (m *Model) openForm() tea.Cmd {
	if m.busy != "" {
		return nil
	}
	m.busy, m.notice = "opening the form", ""
	actions := m.actions
	return func() tea.Msg {
		cmd, err := actions.New()
		return formMsg{cmd: cmd, err: err}
	}
}

// runForm hands the terminal to the form when it has to run here, and
// takes it back when the form exits.
func (m *Model) runForm(msg formMsg) tea.Cmd {
	if msg.err != nil || msg.cmd == nil {
		return m.finish(doneMsg{err: msg.err})
	}
	return tea.ExecProcess(msg.cmd, func(err error) tea.Msg {
		if err != nil {
			err = fmt.Errorf("the new-agent form: %w", err)
		}
		return doneMsg{err: err}
	})
}

func (m *Model) finish(msg doneMsg) tea.Cmd {
	m.busy = ""
	if msg.err != nil {
		m.say(msg.err.Error())
		return nil
	}
	if msg.then.quit {
		return tea.Quit
	}
	if msg.then.recheck {
		m.checkedAt = time.Time{}
		return m.checkInstalled()
	}
	if msg.then.leaving != "" && m.selected == rowStop(msg.then.leaving) {
		m.selectNeighbor()
	}
	if msg.then.dialog != nil && m.dialog == msg.then.dialog {
		m.dialog = nil
	}
	if msg.then.notice != "" {
		m.say(msg.then.notice)
	}
	m.reload()
	return nil
}

func (m *Model) say(notice string) {
	m.notice, m.noticeUntil = notice, m.now().Add(noticeFor)
}

func (m Model) status() string {
	if m.notice != "" && m.now().Before(m.noticeUntil) {
		return m.notice
	}
	if m.busy != "" {
		return m.busy + "…"
	}
	if m.loadErr != nil {
		return "⚠ " + m.loadErr.Error()
	}
	return ""
}

func (m Model) filtered() snapshot.Snapshot {
	return m.snap.Filter(m.filter.Value())
}

// shown is the filtered snapshot as the list lays it out, with the tab's
// agents in a section of their own.
func (m Model) shown() snapshot.Snapshot {
	return arrange(m.filtered(), m.tab)
}

func (m Model) stops() []stop {
	return allStops(m.shown(), m.expanded)
}

func (m Model) selectedRow() (snapshot.Row, bool) {
	if m.selected.isFold() {
		return snapshot.Row{}, false
	}
	return m.snap.Find(m.selected.key)
}

func (m Model) page() int {
	return max(m.height-chromeRows-len(m.pill()), 1)
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
	snap := m.shown()
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
	section, ok := sectionOf(m.shown(), m.selected)
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
	for _, group := range sectionHeads(m.shown(), m.expanded) {
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
	lines := m.body(m.shown())
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
	body := m.body(m.shown())
	view := m.page()
	lines := []string{m.header(m.filtered())}
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
	lines = append(lines, m.pill()...)
	lines = append(lines, m.footer())
	if m.dialog != nil {
		lines = overlay(lines, m.dialog.view(m.width), m.width)
	}
	return strings.Join(lines, "\n")
}
