// Package dirpicker is the new-agent form's Directory field: a typed path
// with a list of directories under it to browse and filter.
package dirpicker

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/jordanbyron/agent-inbox/internal/editor"
)

const maxRecents = 5

// Picker is the field's text and the list under it.
type Picker struct {
	dirs  FS
	base  string
	home  string
	start string
	text  editor.Buffer

	recents          []string
	liveRecents      []entry
	recentsUnchecked bool

	listing   listed
	requested string

	inList bool
	pick   int
	repick bool
	landOn string
}

type row struct {
	entry
	up     bool
	recent bool
}

// New returns a picker showing dir. Relative paths are taken from dir, ~
// is home, and recents are offered while the text is left as it started.
func New(dir, home string, recents []string, dirs FS) Picker {
	if dirs == nil {
		dirs = OS{}
	}
	start := shortenHome(dir, home)
	return Picker{
		dirs: dirs, base: dir, home: home, start: start, text: editor.New(start),
		recents: recents, recentsUnchecked: len(recents) > 0,
	}
}

// Dir is the absolute directory the text names.
func (p Picker) Dir() string {
	return expandPath(strings.TrimSpace(p.text.String()), p.base, p.home)
}

// InList reports whether the keys are moving through the list rather than
// editing the text.
func (p Picker) InList() bool {
	return p.inList
}

// Sync reads what the list needs while the field is open, and closes the
// list when it is not.
func (p *Picker) Sync(open bool) tea.Cmd {
	if !open {
		p.inList, p.requested = false, ""
		return nil
	}
	var cmds []tea.Cmd
	if p.recentsUnchecked {
		p.recentsUnchecked = false
		cmds = append(cmds, checkRecents(p.dirs, p.recents, p.home))
	}
	if dir := p.Dir(); dir != p.requested {
		p.requested = dir
		cmds = append(cmds, read(p.dirs, dir))
	}
	return tea.Batch(cmds...)
}

// Update takes the result of a read, dropping one for a path the text has
// since left.
func (p *Picker) Update(msg tea.Msg) {
	switch msg := msg.(type) {
	case listed:
		if msg.path != p.requested {
			return
		}
		p.listing = msg
		if p.repick {
			p.repick = false
			p.pick = p.rowNamed(p.landOn)
		}
	case recentsChecked:
		p.liveRecents = msg
	}
}

func (p Picker) rowNamed(name string) int {
	rows := p.rows()
	for i, r := range rows {
		if !r.recent && !r.up && r.name == name {
			return i
		}
	}
	if len(rows) > 1 && rows[0].up {
		return 1
	}
	return 0
}

// Press handles a key while the field has focus, and reports false for
// one the form should handle instead.
func (p *Picker) Press(key, text string) bool {
	if key == "enter" && p.takeRecent() {
		return false
	}
	if p.inList {
		if p.pressList(key) {
			return true
		}
		p.inList = false
	}
	switch key {
	case "down":
		if len(p.rows()) == 0 {
			return false
		}
		p.inList, p.pick = true, max(p.tabTarget(), 0)
	case "tab":
		at := p.tabTarget()
		if at < 0 {
			return false
		}
		p.moveTo(p.rows()[at].path)
	case "ctrl+u":
		if !p.showRecents() {
			return p.text.Press(key, text)
		}
		p.inList, p.pick = true, 0
	case "backspace":
		typed := p.text.String()
		if !strings.HasSuffix(typed, "/") || typed == "/" || p.text.Head() != typed {
			return p.text.Press(key, text)
		}
		p.moveTo(filepath.Dir(p.Dir()))
	case "up", "enter", "esc", "shift+tab":
		return false
	default:
		return p.edit(key, text)
	}
	return true
}

func (p *Picker) pressList(key string) bool {
	rows := p.rows()
	if len(rows) == 0 {
		return false
	}
	p.pick = min(p.pick, len(rows)-1)
	switch key {
	case "up":
		if p.pick == 0 {
			p.inList = false
		}
		p.pick = max(p.pick-1, 0)
	case "down":
		p.pick = min(p.pick+1, len(rows)-1)
	case "enter", "right":
		if rows[p.pick].up {
			p.toParent()
		} else {
			p.descend(rows[p.pick].path, "")
		}
	case "left", "backspace":
		p.toParent()
	case "tab":
		p.moveTo(rows[p.pick].path)
		p.inList = false
	case "esc":
		p.inList = false
	case "ctrl+u":
		if p.showRecents() {
			p.pick = 0
		}
	default:
		return false
	}
	return true
}

func (p *Picker) takeRecent() bool {
	rows := p.rows()
	if !p.inList || p.pick >= len(rows) || !rows[p.pick].recent {
		return false
	}
	p.moveTo(rows[p.pick].path)
	p.inList = false
	return true
}

func (p *Picker) descend(dir, landOn string) {
	p.moveTo(dir)
	p.repick, p.landOn = true, landOn
}

func (p *Picker) toParent() {
	p.descend(filepath.Dir(p.listing.dir), filepath.Base(p.listing.dir))
}

func (p *Picker) moveTo(dir string) {
	p.text.Replace(asTyped(dir, p.home))
}

// Typing // or /~ starts the path over, as in Emacs's find-file.
func (p *Picker) edit(key, text string) bool {
	if !p.text.Press(key, text) {
		return false
	}
	typed := p.text.String()
	if p.text.Head() == typed {
		switch {
		case strings.HasSuffix(typed, "//"):
			p.text.Replace("/")
		case strings.HasSuffix(typed, "/~"):
			p.text.Replace("~")
		}
	}
	return true
}

// Paste types text into the field, starting it over for a whole path.
func (p *Picker) Paste(text string) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if strings.HasPrefix(text, "/") || strings.HasPrefix(text, "~") {
		p.text.Replace(text)
		return
	}
	p.text.Insert(text)
}

func (p Picker) showRecents() bool {
	return len(p.liveRecents) > 0 && p.text.String() == p.start
}

func (p Picker) place() (dir, fragment string, ok bool) {
	typed, listed := p.Dir(), p.listing.dir
	if p.listing.path == "" {
		return "", "", false
	}
	if typed == listed {
		if strings.HasSuffix(strings.TrimSpace(p.text.String()), "/.") {
			return listed, ".", true
		}
		return listed, "", true
	}
	rel, err := filepath.Rel(listed, typed)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", "", false
	}
	first, _, _ := strings.Cut(rel, "/")
	return listed, first, true
}

func (p Picker) fragment() string {
	_, fragment, _ := p.place()
	return fragment
}

func (p Picker) rows() []row {
	var out []row
	if p.showRecents() {
		for _, e := range p.liveRecents {
			out = append(out, row{entry: e, recent: true})
		}
	}
	dir, fragment, ok := p.place()
	if !ok || p.listing.err != nil {
		return out
	}
	if fragment != "" {
		for _, e := range match(p.listing.entries, fragment) {
			out = append(out, row{entry: e})
		}
		return out
	}
	if parent := filepath.Dir(dir); parent != dir {
		out = append(out, row{name: "..", path: parent, up: true})
	}
	for _, e := range match(p.listing.entries, "") {
		out = append(out, row{entry: e})
	}
	return out
}

// A fragment's first match is the target without moving the keys into the
// list, so typing then Tab completes as a shell does.
func (p Picker) tabTarget() int {
	rows := p.rows()
	if p.inList {
		return min(p.pick, len(rows)-1)
	}
	if p.fragment() == "" {
		return -1
	}
	for i, r := range rows {
		if !r.recent {
			return i
		}
	}
	return -1
}

// Completes reports whether Tab would complete the text rather than move
// on to the next field.
func (p Picker) Completes() bool {
	return p.tabTarget() >= 0
}
