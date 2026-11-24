// Package editor is the text the new-agent form edits: one string with a
// cursor in it, laid out as a single row or word-wrapped over several.
package editor

import (
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/rivo/uniseg"

	"github.com/jordanbyron/agent-inbox/internal/theme"
)

// Past either, a paste folds into a chip: laid out in full it buries the
// prompt around it.
const (
	FoldChars = 800
	FoldLines = 10
)

// ChipKind says what a chip stands for.
type ChipKind int

// The kinds of chip.
const (
	ChipImage ChipKind = iota
	ChipFile
	ChipPasted
)

// Chip is an attached file or a long paste shown as one token that moves
// and deletes as a single cell, as in Claude Code's own prompt.
type Chip struct {
	Kind ChipKind
	// N numbers the chips of one kind, from 1, never reused.
	N int
	// Path is the attached file, for an image or a file.
	Path string
	// Text is what a pasted chip stands for.
	Text string
}

// Label is how the chip shows in the text.
func (c Chip) Label() string {
	switch c.Kind {
	case ChipImage:
		return "[Image #" + strconv.Itoa(c.N) + "]"
	case ChipFile:
		return "[" + baseName(c.Path) + "]"
	default:
		size := strconv.Itoa(len([]rune(c.Text))) + " chars"
		if lines := strings.Count(c.Text, "\n"); lines > 0 {
			size = "+" + strconv.Itoa(lines) + " lines"
		}
		return "[Pasted text #" + strconv.Itoa(c.N) + " " + size + "]"
	}
}

func baseName(path string) string {
	return path[strings.LastIndex(path, "/")+1:]
}

// A cell is one grapheme cluster, since prompts carry emoji that rune
// indexes cut in half, or one chip.
type cell struct {
	text  string
	width int
	chip  *Chip
}

func (c cell) String() string {
	if c.chip != nil {
		return c.chip.Label()
	}
	return c.text
}

func cells(text string) []cell {
	var out []cell
	state := -1
	for text != "" {
		var cluster string
		cluster, text, _, state = uniseg.FirstGraphemeClusterInString(text, state)
		out = append(out, cell{text: cluster, width: theme.Width(cluster)})
	}
	return out
}

// Paint styles what a view draws. A nil Cursor draws no cursor, as for a
// field without focus; a nil Chip leaves chips as plain text.
type Paint struct {
	Cursor func(cell string) string
	Chip   func(label string) string
}

// Buffer is the text and the cursor. Positions count cells.
type Buffer struct {
	cells  []cell
	cursor int
	counts map[ChipKind]int
}

// New returns a buffer holding text, the cursor at its end.
func New(text string) Buffer {
	b := Buffer{counts: map[ChipKind]int{}}
	b.Replace(text)
	return b
}

// String is the text as shown, chips as their labels.
func (b Buffer) String() string {
	var s strings.Builder
	for _, c := range b.cells {
		s.WriteString(c.String())
	}
	return s.String()
}

// Empty reports whether there is no text at all.
func (b Buffer) Empty() bool {
	return len(b.cells) == 0
}

// Expand is the text as it will be sent: a pasted chip unfolds to its text
// and mention says what each attached file becomes.
func (b Buffer) Expand(mention func(Chip) string) string {
	var s strings.Builder
	for _, c := range b.cells {
		switch {
		case c.chip == nil:
			s.WriteString(c.text)
		case c.chip.Kind == ChipPasted:
			s.WriteString(c.chip.Text)
		default:
			s.WriteString(mention(*c.chip))
		}
	}
	return s.String()
}

// Replace swaps in text with the cursor at its end, where completion wants
// it after it extends a path.
func (b *Buffer) Replace(text string) {
	b.cells = cells(text)
	b.cursor = len(b.cells)
}

// Head is the text before the cursor.
func (b Buffer) Head() string {
	var s strings.Builder
	for _, c := range b.cells[:b.cursor] {
		s.WriteString(c.String())
	}
	return s.String()
}

// ReplaceBefore swaps the count cells before the cursor for text, which is
// how a picked command goes in over the half-typed one.
func (b *Buffer) ReplaceBefore(count int, text string) {
	b.delete(b.cursor-count, count)
	b.Insert(text)
}

// Insert types text at the cursor.
func (b *Buffer) Insert(text string) {
	b.place(cells(text)...)
}

// Attach puts a chip for the file at path at the cursor: an image when
// image is true, any other file otherwise.
func (b *Buffer) Attach(path string, image bool) {
	kind := ChipFile
	if image {
		kind = ChipImage
	}
	b.placeChip(Chip{Kind: kind, Path: path})
}

// Paste inserts text, folded into a chip when it is long.
func (b *Buffer) Paste(text string) {
	if len([]rune(text)) <= FoldChars && strings.Count(text, "\n") < FoldLines {
		b.Insert(text)
		return
	}
	b.placeChip(Chip{Kind: ChipPasted, Text: text})
}

// Chips are numbered per kind and never reused, so a second image is
// [Image #2] even after the first was deleted, as Claude Code does.
func (b *Buffer) placeChip(chip Chip) {
	if b.counts == nil {
		b.counts = map[ChipKind]int{}
	}
	b.counts[chip.Kind]++
	chip.N = b.counts[chip.Kind]
	b.place(cell{chip: &chip, width: theme.Width(chip.Label())})
}

func (b *Buffer) place(c ...cell) {
	b.cells = append(b.cells[:b.cursor], append(c, b.cells[b.cursor:]...)...)
	b.cursor += len(c)
}

// Press handles one key, given as Bubble Tea names it and the text it
// types: readline's editing keys, plus printable text. It reports false
// for a key it has no use for.
func (b *Buffer) Press(key, text string) bool {
	switch key {
	case "left":
		b.cursor = max(b.cursor-1, 0)
	case "right":
		b.cursor = min(b.cursor+1, len(b.cells))
	case "home", "ctrl+a":
		b.cursor = 0
	case "end", "ctrl+e":
		b.cursor = len(b.cells)
	case "backspace", "ctrl+h":
		b.delete(b.cursor-1, 1)
	case "delete":
		b.delete(b.cursor, 1)
	case "ctrl+u":
		b.delete(0, b.cursor)
	case "ctrl+k":
		b.delete(b.cursor, len(b.cells)-b.cursor)
	case "ctrl+w":
		b.deleteWord()
	default:
		if !printable(text) {
			return false
		}
		b.Insert(text)
	}
	return true
}

func printable(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func (b *Buffer) delete(at, length int) {
	if at < 0 || length <= 0 {
		return
	}
	b.cells = append(b.cells[:at], b.cells[min(at+length, len(b.cells)):]...)
	b.cursor = at
}

func (b *Buffer) deleteWord() {
	at := b.cursor
	for at > 0 && b.cells[at-1].text == " " {
		at--
	}
	for at > 0 && b.cells[at-1].text != " " {
		at--
	}
	b.delete(at, b.cursor-at)
}

// Row is the text as one row of at most width columns, scrolled so that
// the cursor stays in view. Without a cursor it is the text truncated.
func (b Buffer) Row(width int, p Paint) string {
	if p.Cursor == nil {
		return theme.Truncate(b.String(), width)
	}
	first, used := b.cursor, 0
	for first > 0 && used+b.cells[first-1].width <= width-1 {
		first--
		used += b.cells[first].width
	}
	var shown []cell
	used = 0
	for _, c := range b.cells[first:] {
		if used+c.width > width {
			break
		}
		shown = append(shown, c)
		used += c.width
	}
	return paint(shown, b.cursor-first, p)
}

// View is the visible slice of the text word-wrapped to width: at most
// height rows, and how many rows are hidden above them. It shows the end,
// where typing happens, unless the cursor has gone up out of that window.
func (b Buffer) View(width, height int, p Paint) ([]string, int) {
	rows := b.wrap(width)
	at := b.cursorRow(rows)
	first := max(len(rows)-height, 0)
	first = min(first, at)
	shown := rows[first:min(first+height, len(rows))]
	lines := make([]string, len(shown))
	for i, r := range shown {
		offset := -1
		if p.Cursor != nil && first+i == at {
			offset = b.cursor - r.start
		}
		lines[i] = paint(r.cells, offset, p)
	}
	return lines, first
}

// Rows is how many rows the text takes wrapped to width.
func (b Buffer) Rows(width int) int {
	return len(b.wrap(width))
}

// Up moves the cursor to the row above, keeping its column, and reports
// false when it is already on the first row.
func (b *Buffer) Up(width int) bool {
	return b.moveRow(width, -1)
}

// Down moves the cursor to the row below, and reports false on the last.
func (b *Buffer) Down(width int) bool {
	return b.moveRow(width, 1)
}

func (b *Buffer) moveRow(width, delta int) bool {
	rows := b.wrap(width)
	at := b.cursorRow(rows)
	to := at + delta
	if to < 0 || to >= len(rows) {
		return false
	}
	column := widthOf(rows[at].cells[:min(b.cursor-rows[at].start, len(rows[at].cells))])
	target := rows[to]
	last := len(target.cells)
	// The end of a row the wrap broke is the start of the next one, so the
	// cursor stops a cell short to stay on the row it moved to.
	if to+1 < len(rows) && rows[to+1].start == target.start+last && last > 0 {
		last--
	}
	k, used := 0, 0
	for k < last && used+target.cells[k].width <= column {
		used += target.cells[k].width
		k++
	}
	b.cursor = target.start + k
	return true
}

func paint(cells []cell, offset int, p Paint) string {
	var s strings.Builder
	for i, c := range cells {
		text := c.String()
		if c.chip != nil && p.Chip != nil {
			text = p.Chip(text)
		}
		if i == offset {
			text = p.Cursor(text)
		}
		s.WriteString(text)
	}
	if offset >= len(cells) {
		s.WriteString(p.Cursor(" "))
	}
	return s.String()
}

type row struct {
	cells []cell
	start int
}

func widthOf(cells []cell) int {
	w := 0
	for _, c := range cells {
		w += c.width
	}
	return w
}

// wrap gives a cursor at the end of a full row a row of its own, as a
// terminal does when text reaches the right margin.
func (b Buffer) wrap(width int) []row {
	var rows []row
	for _, line := range b.lines() {
		if len(line.cells) == 0 {
			rows = append(rows, line)
			continue
		}
		at := line.start
		for _, seg := range segments(line.cells, width) {
			rows = append(rows, row{cells: seg, start: at})
			at += len(seg)
		}
	}
	if b.cursor == len(b.cells) && widthOf(rows[len(rows)-1].cells) >= width {
		rows = append(rows, row{start: len(b.cells)})
	}
	return rows
}

// A word keeps its trailing space, since the cursor may sit on it.
func segments(cells []cell, width int) [][]cell {
	var lines [][]cell
	var line []cell
	used := 0
	for _, word := range words(cells) {
		wordWidth := widthOf(word)
		bare := wordWidth
		if word[len(word)-1].text == " " {
			bare--
		}
		if len(line) > 0 && used+bare > width {
			lines = append(lines, line)
			line, used = nil, 0
		}
		for wordWidth > width {
			cut, cutWidth := fit(word, width)
			lines = append(lines, word[:cut])
			word = word[cut:]
			wordWidth -= cutWidth
		}
		line = append(line, word...)
		used += wordWidth
	}
	if len(line) > 0 {
		lines = append(lines, line)
	}
	return lines
}

func words(cells []cell) [][]cell {
	var out [][]cell
	start := 0
	for i, c := range cells {
		if c.text == " " {
			out = append(out, cells[start:i+1])
			start = i + 1
		}
	}
	if start < len(cells) {
		out = append(out, cells[start:])
	}
	return out
}

// fit is how many of word's cells fit in width, and their width: always at
// least one, so a glyph wider than the row still moves on.
func fit(word []cell, width int) (int, int) {
	count, used := 0, 0
	for _, c := range word {
		if count > 0 && used+c.width > width {
			break
		}
		count++
		used += c.width
	}
	return count, used
}

// lines splits the text at newlines. The newline belongs to the line it
// ends, so a cursor on it lands past the end of that line.
func (b Buffer) lines() []row {
	var out []row
	start := 0
	for i, c := range b.cells {
		if c.text == "\n" {
			out = append(out, row{cells: b.cells[start:i], start: start})
			start = i + 1
		}
	}
	return append(out, row{cells: b.cells[start:], start: start})
}

func (b Buffer) cursorRow(rows []row) int {
	for i, r := range slices.Backward(rows) {
		if r.start <= b.cursor {
			return i
		}
	}
	return 0
}
