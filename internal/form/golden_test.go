package form_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/jordanbyron/agent-inbox/internal/golden"
)

func skills(s *screen) {
	for _, name := range []string{"unslop", "unsplit", "babysit", "code-review"} {
		s.write(".claude/skills/"+name+"/SKILL.md", "---\ndescription: "+name+" does things\n---\n")
	}
	s.write("code/app/.claude/commands/deploy.md", "---\ndescription: Ship it to production\n---\n")
}

func notInRepo(s *screen) {
	s.repos = map[string]bool{}
}

var frames = []struct {
	name      string
	configure func(s *screen)
	drive     func(s *screen)
}{
	{name: "empty"},
	{name: "filled", drive: func(s *screen) {
		s.clip.image = true
		s.typing("fix the login redirect loop that sends people back to the sign-in page after their session expires, see ")
		s.press("ctrl+v")
		s.press("tab", "tab", "tab", "tab", "l", "tab", "l", "l", "tab", "l", "l", "l", "l", "tab", "l")
	}},
	{name: "slash_menu", configure: skills, drive: func(s *screen) { s.typing("/un") }},
	{name: "long_prompt", drive: func(s *screen) {
		for i := 1; i <= 14; i++ {
			s.typing("step " + strconv.Itoa(i) + ": " + strings.Repeat("and then some more words ", 2))
			s.press("enter")
		}
		s.typing("done")
	}},
	{name: "not_a_repo", configure: notInRepo},
	{name: "recipe_claude", drive: func(s *screen) { s.press("tab") }},
	{name: "recipe_codex", drive: func(s *screen) { s.press("tab", "l") }},
	{name: "recipe_agy", drive: func(s *screen) { s.press("tab", "l", "l") }},
}

func TestGoldenFrames(t *testing.T) {
	sizes := []struct{ width, height int }{{100, 30}, {60, 20}, {50, 16}}
	for _, frame := range frames {
		for _, size := range sizes {
			name := frame.name + "_" + strconv.Itoa(size.width) + "x" + strconv.Itoa(size.height)
			t.Run(name, func(t *testing.T) {
				var configure []func(*screen)
				if frame.configure != nil {
					configure = append(configure, frame.configure)
				}
				s := newScreen(t, size.width, size.height, configure...)
				if frame.drive != nil {
					frame.drive(s)
				}
				golden.Require(t, name, s.frame())
			})
		}
	}
}
