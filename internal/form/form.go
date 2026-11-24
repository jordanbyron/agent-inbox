// Package form is the new-agent form: a Bubble Tea model that collects what
// to start and returns it. Starting the agent is the caller's work.
package form

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/jordanbyron/agent-inbox/internal/dirpicker"
	"github.com/jordanbyron/agent-inbox/internal/editor"
	"github.com/jordanbyron/agent-inbox/internal/images"
	"github.com/jordanbyron/agent-inbox/internal/recipe"
	"github.com/jordanbyron/agent-inbox/internal/slash"
)

// Session is a Rex session the agent can start in.
type Session struct {
	ID    string
	Label string
}

// Options is what the form needs from outside.
type Options struct {
	// Recipes are the harnesses offered, the first one chosen.
	Recipes recipe.Set
	// Sessions are the Rex sessions offered besides a new one.
	Sessions []Session
	// CurrentSession is the id of the session the form starts on; with
	// none of Sessions matching, it starts on a new session.
	CurrentSession string
	// Dir is the directory the form starts with.
	Dir string
	// RecentDirs are directories agents started in before, most recent
	// first, offered while Directory is left as it started.
	RecentDirs []string
	// DirFS is the filesystem Directory browses, the real one when nil.
	DirFS dirpicker.FS
	// WorktreeDefault is config's worktree.default: whether Worktree starts
	// on yes in a git repository.
	WorktreeDefault bool
	// InRepo reports whether a directory is in a git repository.
	InRepo func(dir string) bool
	// Home is where ~ points.
	Home string
	Now  func() time.Time
	// Clipboard is read for an image or text on Ctrl-V or an empty paste.
	Clipboard images.Clipboard
	// ImageDir is where a pasted image is saved.
	ImageDir string
	// Output is where the form draws, stdout when nil. A caller that prints
	// to stdout itself draws the form somewhere else.
	Output io.Writer
}

// Result is what the user chose.
type Result struct {
	// Prompt has each attached file written as the harness mentions one.
	Prompt  string `json:"prompt"`
	Harness string `json:"harness"`
	// Name is empty when the user left it so; the caller makes one from
	// the prompt.
	Name string `json:"name"`
	// Dir is an absolute path.
	Dir string `json:"dir"`
	// Session is the Rex session's id, or "" for a new session.
	Session string `json:"session"`
	// Model, Effort and Permissions are "" when the recipe offers none.
	Model       string `json:"model"`
	Effort      string `json:"effort"`
	Permissions string `json:"permissions"`
	Worktree    bool   `json:"worktree"`
	// ShowTab says to show the agent's tab once it starts.
	ShowTab bool `json:"show_tab"`
}

// Run shows the form until the user starts the agent or cancels, and
// reports false when they canceled.
func Run(ctx context.Context, opts Options) (Result, bool, error) {
	if len(opts.Recipes) == 0 {
		return Result{}, false, errors.New("starting the form: no recipes")
	}
	// The program detects the terminal's color profile and NO_COLOR, and
	// brings the form's colors down to match.
	options := []tea.ProgramOption{tea.WithContext(ctx)}
	if opts.Output != nil {
		options = append(options, tea.WithOutput(opts.Output))
	}
	final, err := tea.NewProgram(New(opts), options...).Run()
	if err != nil {
		return Result{}, false, fmt.Errorf("running the form: %w", err)
	}
	m, ok := final.(Model)
	if !ok || !m.submitted {
		return Result{}, false, nil
	}
	return m.result, true, nil
}

type fieldKey int

const (
	fieldPrompt fieldKey = iota
	fieldHarness
	fieldName
	fieldDir
	fieldSession
	fieldModel
	fieldEffort
	fieldPermissions
	fieldWorktree
)

var fieldLabels = map[fieldKey]string{
	fieldPrompt:      "Prompt",
	fieldHarness:     "Harness",
	fieldName:        "Name",
	fieldDir:         "Directory",
	fieldSession:     "Rex session",
	fieldModel:       "Model",
	fieldEffort:      "Effort",
	fieldPermissions: "Permissions",
	fieldWorktree:    "Worktree",
}

const newSession = "new session"

// Model is the form.
type Model struct {
	opts Options

	width, height int
	focus         fieldKey

	prompt, name editor.Buffer
	dir          dirpicker.Picker
	choices      map[fieldKey]*choice

	inRepo  bool
	repoFor string

	commands    []slash.Command
	commandsFor string
	menuPick    int
	// dismissed is the query Esc closed the menu on, so it stays shut
	// until the query changes.
	dismissed    string
	hasDismissed bool

	problem        string
	confirmDiscard bool

	submitted bool
	result    Result
}

// New returns the form, focused on the prompt.
func New(opts Options) Model {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.InRepo == nil {
		opts.InRepo = func(string) bool { return false }
	}
	m := Model{
		opts:   opts,
		prompt: editor.New(""),
		name:   editor.New(""),
		dir:    dirpicker.New(opts.Dir, opts.Home, opts.RecentDirs, opts.DirFS),
		choices: map[fieldKey]*choice{
			fieldHarness:     {},
			fieldSession:     sessionChoice(opts.Sessions, opts.CurrentSession),
			fieldModel:       {},
			fieldEffort:      {},
			fieldPermissions: {},
			fieldWorktree:    {values: []string{"no", "yes"}},
		},
	}
	for _, r := range opts.Recipes {
		m.choices[fieldHarness].values = append(m.choices[fieldHarness].values, r.Name)
	}
	m.chooseHarness()
	m.refresh()
	return m
}

func sessionChoice(sessions []Session, current string) *choice {
	c := &choice{}
	for i, s := range sessions {
		c.values = append(c.values, s.ID)
		c.labels = append(c.labels, s.Label)
		if s.ID == current {
			c.pick = i
		}
	}
	c.values = append(c.values, "")
	c.labels = append(c.labels, newSession)
	if !hasSession(sessions, current) {
		c.pick = len(sessions)
	}
	return c
}

func hasSession(sessions []Session, id string) bool {
	for _, s := range sessions {
		if s.ID == id {
			return true
		}
	}
	return false
}

// Init does nothing; the form waits on keys.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles a key, a paste, a resize or a directory read.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		cmd = m.press(msg)
	case tea.PasteMsg:
		m.paste(msg.Content)
	default:
		m.dir.Update(msg)
	}
	m.refresh()
	return m, tea.Batch(cmd, m.dir.Sync(m.focus == fieldDir))
}

// refresh catches up on what follows from the Directory and Harness fields.
// It runs here rather than when drawing, since it reads the filesystem.
func (m *Model) refresh() {
	if dir := m.directory(); dir != m.repoFor {
		was := m.inRepo
		m.repoFor = dir
		m.inRepo = m.opts.InRepo(dir)
		if m.inRepo && !was {
			m.choices[fieldWorktree].pick = boolIndex(m.opts.WorktreeDefault)
		}
		if !m.fieldShown(m.focus) {
			m.focus = fieldDir
		}
	}
	if _, open := m.menuQuery(); open {
		r := m.recipe()
		key := r.Name + "\x00" + m.directory()
		if key != m.commandsFor {
			m.commandsFor = key
			m.commands = slash.List(slash.Where{
				Dirs: r.Commands, Project: m.directory(), Home: m.opts.Home, Plugins: r.App == "claude-code",
			})
		}
	}
}

func boolIndex(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (m Model) recipe() recipe.Recipe {
	if pick := m.choices[fieldHarness].pick; pick < len(m.opts.Recipes) {
		return m.opts.Recipes[pick]
	}
	return recipe.Recipe{}
}

func (m Model) fields() []fieldKey {
	var out []fieldKey
	for key := fieldPrompt; key <= fieldWorktree; key++ {
		if m.fieldShown(key) {
			out = append(out, key)
		}
	}
	return out
}

func (m Model) fieldShown(key fieldKey) bool {
	switch key {
	case fieldHarness:
		return len(m.opts.Recipes) > 1
	case fieldWorktree:
		return m.inRepo
	case fieldModel, fieldEffort, fieldPermissions:
		return len(m.choices[key].values) > 0
	default:
		return true
	}
}

// chooseHarness offers the recipe's values, keeping each pick the new
// recipe also offers and starting the rest on the first.
func (m *Model) chooseHarness() {
	offered := m.recipe().Choices
	for key, values := range map[fieldKey][]string{
		fieldModel: offered.Model, fieldEffort: offered.Effort, fieldPermissions: offered.Permissions,
	} {
		c := m.choices[key]
		was := c.value()
		c.values, c.pick = values, 0
		for i, v := range values {
			if v == was {
				c.pick = i
			}
		}
	}
}

func (m Model) directory() string {
	return m.dir.Dir()
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// mention writes a file the way the recipe's harness mentions one. Claude
// Code reads an @ mention up to a space unless the space is escaped.
func (m Model) mention(chip editor.Chip) string {
	format := m.recipe().Mention
	if format == "" {
		format = "{path}"
	}
	path := chip.Path
	if strings.HasPrefix(format, "@") {
		path = strings.ReplaceAll(path, " ", `\ `)
	}
	return strings.ReplaceAll(format, "{path}", path)
}

func (m *Model) submit(showTab bool) tea.Cmd {
	r := Result{
		Prompt:      strings.TrimSpace(m.prompt.Expand(m.mention)),
		Harness:     m.recipe().Name,
		Name:        strings.TrimSpace(m.name.String()),
		Dir:         m.directory(),
		Session:     m.choices[fieldSession].value(),
		Model:       m.choices[fieldModel].value(),
		Effort:      m.choices[fieldEffort].value(),
		Permissions: m.choices[fieldPermissions].value(),
		Worktree:    m.inRepo && m.choices[fieldWorktree].value() == "yes",
		ShowTab:     showTab,
	}
	switch {
	case r.Prompt == "":
		m.problem, m.focus = "a prompt is required", fieldPrompt
		return nil
	case !isDir(r.Dir):
		m.problem, m.focus = "no such directory: "+r.Dir, fieldDir
		return nil
	}
	m.submitted, m.result = true, r
	return tea.Quit
}

// Submitted returns what the user chose, and false until they started it.
func (m Model) Submitted() (Result, bool) {
	return m.result, m.submitted
}
