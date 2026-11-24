package dirpicker

import (
	"errors"
	"io/fs"
	"strconv"

	"github.com/jordanbyron/agent-inbox/internal/editor"
	"github.com/jordanbyron/agent-inbox/internal/theme"
)

const maxNameWidth = 36

var (
	markerStyle = theme.Yellow.Bold(true)
	pickStyle   = theme.Cyan.Bold(true)
	focusedPick = theme.Cyan.Bold(true).Reverse(true)
)

// Row is the text as one row of at most width columns.
func (p Picker) Row(width int, paint editor.Paint) string {
	if p.inList {
		paint.Cursor = nil
	}
	return p.text.Row(width, paint)
}

// Lines draw the list in at most height rows of width columns, scrolled to
// keep the highlight in view.
func (p Picker) Lines(width, height int) []string {
	if height <= 0 {
		return nil
	}
	if p.listing.err != nil && p.listing.path == p.requested {
		return []string{theme.Dim(theme.Truncate(p.unreadable(), width))}
	}
	rows, at := p.rows(), p.tabTarget()
	if dir, fragment, ok := p.place(); ok && fragment != "" && len(rows) == 0 {
		return []string{theme.Dim(theme.Truncate("  nothing in "+shortenHome(dir, p.home)+" matches "+fragment, width))}
	}
	var lines []string
	atLine := -1
	nameWidth := 0
	for _, r := range rows {
		nameWidth = max(nameWidth, theme.Width(label(r)))
	}
	nameWidth = min(nameWidth, maxNameWidth, max(width-6, 1))
	for i, r := range rows {
		if i == 0 && r.recent {
			lines = append(lines, "  "+theme.Dim("recent"))
		}
		if i == at {
			atLine = len(lines)
		}
		lines = append(lines, p.line(r, i == at, nameWidth))
	}
	first := max(atLine-height+1, 0)
	shown := lines[first:min(first+height, len(lines))]
	if left := len(lines) - first - len(shown); left > 0 {
		more := "+" + strconv.Itoa(left) + " more"
		last := len(shown) - 1
		shown[last] = theme.Pad(shown[last], width-theme.Width(more)) + theme.Dim(more)
	}
	for i, line := range shown {
		shown[i] = theme.Truncate(line, width)
	}
	return shown
}

func label(r row) string {
	if r.up || r.recent {
		return r.name
	}
	return r.name + "/"
}

func (p Picker) line(r row, on bool, nameWidth int) string {
	name := theme.Pad(label(r), nameWidth)
	marker := "  "
	switch {
	case on && p.inList:
		marker, name = markerStyle.Render("▶")+" ", focusedPick.Render(name)
	case on:
		marker, name = pickStyle.Render("›")+" ", pickStyle.Render(name)
	case r.up:
		name = theme.Dim(name)
	}
	if r.repo {
		return marker + name + "  " + theme.Dim("git")
	}
	return marker + name
}

func (p Picker) unreadable() string {
	reason := p.listing.err
	if pathErr, ok := errors.AsType[*fs.PathError](reason); ok {
		reason = pathErr.Err
	}
	return "  can't read " + shortenHome(p.listing.dir, p.home) + ": " + reason.Error()
}

// Hints are the keys the footer offers while the field has focus, as
// pairs of a key and what it does.
func (p Picker) Hints() []string {
	if p.inList {
		return []string{"⏎", "open", "←", "parent", "⇥", "pick", "esc", "back"}
	}
	hints := []string{"⇥", "next", "↓", "browse"}
	if p.Completes() {
		hints[1] = "complete"
	}
	if p.showRecents() {
		hints = append(hints, "^U", "recent")
	}
	return hints
}
