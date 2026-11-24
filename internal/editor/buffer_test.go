package editor_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/editor"
)

var mark = editor.Paint{Cursor: func(cell string) string { return "[" + cell + "]" }}

func press(b *editor.Buffer, keys ...string) {
	for _, k := range keys {
		b.Press(k, "")
	}
}

func typing(b *editor.Buffer, text string) {
	for _, r := range text {
		b.Press(string(r), string(r))
	}
}

func repeat(key string, n int) []string {
	keys := make([]string, n)
	for i := range keys {
		keys[i] = key
	}
	return keys
}

func TestEditing(t *testing.T) {
	cases := []struct {
		name  string
		start string
		edit  func(b *editor.Buffer)
		want  string
	}{
		{"types where the cursor is", "ad", func(b *editor.Buffer) {
			press(b, "left")
			typing(b, "bc")
			typing(b, "X")
		}, "abcXd"},
		{"stops at the start instead of wrapping around", "ab", func(b *editor.Buffer) {
			press(b, repeat("left", 5)...)
			press(b, "backspace")
		}, "ab"},
		{"stops at the end instead of wrapping around", "ab", func(b *editor.Buffer) {
			press(b, repeat("right", 5)...)
			press(b, "delete")
		}, "ab"},
		{"Ctrl-W deletes a word", "one two", func(b *editor.Buffer) { press(b, "ctrl+w") }, "one "},
		{"Ctrl-W takes the spaces before the word too", "one two  ", func(b *editor.Buffer) { press(b, "ctrl+w") }, "one "},
		{"backspace deletes backwards", "one", func(b *editor.Buffer) { press(b, "backspace") }, "on"},
		{"delete deletes forwards", "one", func(b *editor.Buffer) { press(b, "home", "delete") }, "ne"},
		{"Ctrl-A and Ctrl-E go to either end", "bc", func(b *editor.Buffer) {
			press(b, "ctrl+a")
			typing(b, "a")
			press(b, "ctrl+e")
			typing(b, "d")
		}, "abcd"},
		{"Ctrl-K kills to the end", "one two", func(b *editor.Buffer) {
			press(b, "home", "right", "right", "right", "ctrl+k")
		}, "one"},
		{"Ctrl-U kills to the start", "keep this", func(b *editor.Buffer) {
			press(b, "left", "left", "ctrl+u")
		}, "is"},
		{"keeps whole emoji together", "a👍b", func(b *editor.Buffer) { press(b, "left", "backspace") }, "ab"},
		{"keeps a flag together", "a🇨🇦b", func(b *editor.Buffer) { press(b, "left", "backspace") }, "ab"},
		{"ignores keys it has no use for", "", func(b *editor.Buffer) {
			press(b, "tab", "esc", "ctrl+x")
			b.Press("x", "x")
		}, "x"},
		{"takes a space as text", "a", func(b *editor.Buffer) { b.Press("space", " ") }, "a "},
		{"keeps a short paste inline, as typing", "a ", func(b *editor.Buffer) { b.Paste("one\ntwo") }, "a one\ntwo"},
		{"replaces the cells before the cursor", "do /uns", func(b *editor.Buffer) {
			b.ReplaceBefore(4, "/unslop ")
		}, "do /unslop "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := editor.New(tc.start)
			tc.edit(&b)
			require.Equal(t, tc.want, b.String())
		})
	}
}

func TestReportsTheKeysItTakes(t *testing.T) {
	b := editor.New("")
	require.True(t, b.Press("left", ""))
	require.True(t, b.Press("x", "x"))
	require.False(t, b.Press("tab", ""))
	require.False(t, b.Press("enter", ""))
}

func TestHead(t *testing.T) {
	b := editor.New("first /un rest")
	press(&b, repeat("left", 5)...)
	require.Equal(t, "first /un", b.Head())
}

func TestRow(t *testing.T) {
	b := editor.New("abcdefgh")
	require.Equal(t, "fgh[ ]", b.Row(4, mark))
	press(&b, repeat("left", 5)...)
	require.Equal(t, "abc[d]", b.Row(4, mark))
	require.Equal(t, "abc…", b.Row(4, editor.Paint{}))
}

func TestView(t *testing.T) {
	cases := []struct {
		name          string
		text          string
		width, height int
		keys          []string
		paint         editor.Paint
		want          []string
		hidden        int
	}{
		{
			name: "wraps and marks the cursor's row", text: "hello world\nbye", width: 6, height: 3, paint: mark,
			want: []string{"hello ", "world", "bye[ ]"},
		},
		{
			name: "shows the end of a long text", text: lines(5), width: 10, height: 2, paint: mark,
			want: []string{"line3", "line4[ ]"}, hidden: 3,
		},
		{
			name: "follows the cursor up out of the window", text: lines(5), width: 10, height: 2, paint: mark,
			keys: repeat("left", 18), want: []string{"line1[ ]", "line2"}, hidden: 1,
		},
		{
			name: "puts a cursor on a cell inside a row", text: lines(5), width: 10, height: 2, paint: mark,
			keys: repeat("left", 19), want: []string{"line[1]", "line2"}, hidden: 1,
		},
		{
			name: "cuts a long unbroken paste into full rows", text: strings.Repeat("x", 20_000), width: 100, height: 3,
			want: []string{strings.Repeat("x", 100), strings.Repeat("x", 100), ""}, hidden: 198,
		},
		{
			name: "gives a glyph wider than the box a row to itself", text: "😀😀", width: 1, height: 3,
			want: []string{"😀", "😀", ""},
		},
		{
			name: "gives the cursor a row of its own at the right margin", text: "abcd", width: 4, height: 3, paint: mark,
			want: []string{"abcd", "[ ]"},
		},
		{
			name: "keeps an empty line", text: "a\n\nb", width: 4, height: 3,
			want: []string{"a", "", "b"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := editor.New(tc.text)
			press(&b, tc.keys...)
			rows, hidden := b.View(tc.width, tc.height, tc.paint)
			require.Equal(t, tc.want, rows)
			require.Equal(t, tc.hidden, hidden)
		})
	}
}

func lines(n int) string {
	var out []string
	for i := range n {
		out = append(out, "line"+string(rune('0'+i)))
	}
	return strings.Join(out, "\n")
}

func TestUpAndDown(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		keys  []string
		moved bool
		want  string
	}{
		{"up keeps the column", "abcd\nefgh", []string{"left", "left", "up"}, true, "ab[c]d"},
		{"up stops at the end of a shorter row", "ab\nefgh", []string{"up"}, true, "ab[ ]"},
		{"up from the first row reports false", "abcd", []string{"up"}, false, "abcd[ ]"},
		{"down from the last row reports false", "abcd\nef", []string{"down"}, false, "ef[ ]"},
		{"down keeps the column", "abcd\nefgh", []string{"home", "right", "down"}, true, "e[f]gh"},
		{"up into a wrapped row stays on it", "hello world", []string{"up"}, true, "hello[ ]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := editor.New(tc.text)
			moved := true
			for _, k := range tc.keys {
				switch k {
				case "up":
					moved = b.Up(6)
				case "down":
					moved = b.Down(6)
				default:
					press(&b, k)
				}
			}
			require.Equal(t, tc.moved, moved)
			rows, _ := b.View(6, 3, mark)
			require.Contains(t, rows, tc.want)
		})
	}
}

func TestPasteFolds(t *testing.T) {
	var log []string
	for i := 1; i <= 12; i++ {
		log = append(log, "line "+string(rune('a'+i)))
	}
	long := strings.Join(log, "\n")
	b := editor.New("fix ")
	b.Paste(long)
	b.Paste(strings.Repeat("x", 900))
	require.Equal(t, "fix [Pasted text #1 +11 lines][Pasted text #2 900 chars]", b.String())
	require.Equal(t, "fix "+long+strings.Repeat("x", 900), b.Expand(func(editor.Chip) string { return "never" }))
	press(&b, "backspace")
	require.Equal(t, "fix [Pasted text #1 +11 lines]", b.String())
}

func TestAttachedImages(t *testing.T) {
	mention := func(c editor.Chip) string { return "@" + c.Path }

	t.Run("shows each image as a numbered token and hands its path back", func(t *testing.T) {
		b := editor.New("see ")
		b.Attach("/tmp/a.png", true)
		typing(&b, " and ")
		b.Attach("/tmp/b.png", true)
		require.Equal(t, "see [Image #1] and [Image #2]", b.String())
		require.Equal(t, "see @/tmp/a.png and @/tmp/b.png", b.Expand(mention))
	})

	t.Run("moves over and deletes the token as one cell", func(t *testing.T) {
		b := editor.New("a")
		b.Attach("/tmp/a.png", true)
		typing(&b, "b")
		press(&b, "left", "left", "delete")
		require.Equal(t, "ab", b.String())
		b.Attach("/tmp/c.png", true)
		require.Equal(t, "a[Image #2]b", b.String())
		press(&b, "backspace")
		require.Equal(t, "ab", b.String())
	})

	t.Run("keeps numbering past a deleted image", func(t *testing.T) {
		b := editor.New("")
		b.Attach("/tmp/a.png", true)
		press(&b, "backspace")
		b.Attach("/tmp/b.png", true)
		require.Equal(t, "[Image #2]", b.String())
	})

	t.Run("names another file by its base name", func(t *testing.T) {
		b := editor.New("read ")
		b.Attach("/tmp/notes.md", false)
		require.Equal(t, "read [notes.md]", b.String())
		require.Equal(t, "read @/tmp/notes.md", b.Expand(mention))
	})

	t.Run("wraps the token whole and paints it, the cursor over all of it", func(t *testing.T) {
		chip := func(s string) string { return "<" + s + ">" }
		b := editor.New("look ")
		b.Attach("/tmp/a.png", true)
		press(&b, "left")
		rows, _ := b.View(12, 3, editor.Paint{Cursor: mark.Cursor, Chip: chip})
		require.Equal(t, []string{"look ", "[<[Image #1]>]"}, rows)
		rows, _ = b.View(12, 3, editor.Paint{Chip: chip})
		require.Equal(t, []string{"look ", "<[Image #1]>"}, rows)
	})
}
