package form_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/form"
	"github.com/jordanbyron/agent-inbox/internal/recipe"
)

func TestMovingBetweenFields(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		want string
	}{
		{"starts on the prompt", nil, "Prompt"},
		{"Tab moves to the next field", []string{"tab"}, "Harness"},
		{"Tab walks every field in order", []string{"tab", "tab", "tab", "tab", "tab", "tab", "tab", "tab"}, "Worktree"},
		{"Tab moves from the last field to Start", repeat("tab", 9), "Start"},
		{"Tab wraps to the prompt", repeat("tab", 10), "Prompt"},
		{"Shift-Tab wraps backwards to Start", []string{"shift+tab"}, "Start"},
		{"enter moves on from the last field to Start", append(repeat("tab", 8), "enter"), "Start"},
		{"down moves on from a field", []string{"tab", "down"}, "Name"},
		{"up moves back from a field", []string{"tab", "tab", "up"}, "Harness"},
		{"down leaves a one-row prompt", []string{"down"}, "Harness"},
		{"enter moves on from a field", []string{"tab", "enter"}, "Name"},
		{"Tab on a directory that needs no completing moves on", []string{"tab", "tab", "tab", "tab"}, "Rex"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newScreen(t, 100, 30).press(tc.keys...)
			require.Equal(t, tc.want, s.focusedLabel())
		})
	}
}

func repeat(key string, n int) []string {
	keys := make([]string, n)
	for i := range keys {
		keys[i] = key
	}
	return keys
}

func TestPromptTakesTypingAndNewlines(t *testing.T) {
	s := newScreen(t, 100, 30).typing("first").press("enter").typing("second").press("ctrl+s")
	require.Equal(t, "first\nsecond", s.result().Prompt)
	require.True(t, s.quit)
}

func TestUpAndDownMoveWithinAPromptOfSeveralRows(t *testing.T) {
	s := newScreen(t, 100, 30).typing("abc").press("enter").typing("de")
	s.press("up").typing("X")
	require.Equal(t, "Prompt", s.focusedLabel())
	s.press("down", "down")
	require.Equal(t, "Harness", s.focusedLabel())
	s.press("ctrl+s")
	require.Equal(t, "abXc\nde", s.result().Prompt)
}

func TestChoicesCycle(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		want string
	}{
		{"l steps right", []string{"l"}, "fable"},
		{"right steps right", []string{"right"}, "fable"},
		{"space steps right", []string{"space"}, "fable"},
		{"h wraps around to the last", []string{"h"}, "haiku"},
		{"left steps left", []string{"l", "l", "left"}, "fable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newScreen(t, 100, 30).typing("do it").press("tab", "tab", "tab", "tab", "tab")
			require.Equal(t, "Model", s.focusedLabel())
			s.press(tc.keys...).press("ctrl+s")
			require.Equal(t, tc.want, s.result().Model)
		})
	}
}

func TestStartsWithTheDefaults(t *testing.T) {
	s := newScreen(t, 100, 30).typing("do it").press("ctrl+s")
	require.Equal(t, form.Result{
		Prompt: "do it", Harness: "claude", Dir: s.dir, Session: "s1",
		Model: "default", Effort: "default", Permissions: "default", Worktree: true,
	}, s.result())
}

func TestStartButton(t *testing.T) {
	t.Run("Enter on it starts the agent", func(t *testing.T) {
		s := newScreen(t, 100, 30).typing("do it").press("shift+tab", "enter")
		require.Equal(t, "do it", s.result().Prompt)
		require.False(t, s.result().ShowTab)
		require.True(t, s.quit)
	})
	t.Run("refuses an empty prompt as Ctrl-S does", func(t *testing.T) {
		s := newScreen(t, 100, 30).press("shift+tab", "enter")
		_, ok := s.model.Submitted()
		require.False(t, ok)
		require.Equal(t, "Prompt", s.focusedLabel())
	})
	t.Run("ignores the keys that change a choice", func(t *testing.T) {
		s := newScreen(t, 100, 30).typing("do it").press("shift+tab", "l", "space", "h")
		require.Equal(t, "Start", s.focusedLabel())
		_, ok := s.model.Submitted()
		require.False(t, ok)
	})
}

func TestCtrlOShowsTheTab(t *testing.T) {
	s := newScreen(t, 100, 30).typing("do it").press("ctrl+o")
	require.True(t, s.result().ShowTab)
	require.True(t, s.quit)
}

func TestChangingHarness(t *testing.T) {
	s := newScreen(t, 100, 30).typing("do it")
	s.press("tab", "tab", "tab", "tab", "tab", "l", "l")
	s.press("tab", "l", "l", "l")
	s.press("tab", "l")
	s.press("shift+tab", "shift+tab", "shift+tab", "shift+tab", "shift+tab", "shift+tab")
	require.Equal(t, "Harness", s.focusedLabel())
	s.press("l")
	require.Contains(t, s.lineWith("Model"), "gpt-6-luna")
	require.Contains(t, s.lineWith("Permissions"), "workspace-write")
	s.press("ctrl+s")
	r := s.result()
	require.Equal(t, "codex", r.Harness)
	require.Equal(t, "default", r.Model, "opus is not a Codex model, so Model starts over")
	require.Equal(t, "high", r.Effort, "Codex offers high too, so it is kept")
	require.Equal(t, "default", r.Permissions)
}

func TestHarnessHiddenWithOneRecipe(t *testing.T) {
	s := newScreen(t, 100, 30, func(s *screen) { s.opts.Recipes = recipe.Builtins()[:1] })
	require.Empty(t, s.lineWith("Harness"))
	s.press("tab")
	require.Equal(t, "Name", s.focusedLabel())
}

func TestPickerHiddenWhenTheRecipeOffersNothing(t *testing.T) {
	bare := recipe.Recipe{Name: "bare", App: "bare", Start: []string{"bare"}, Resume: []string{"bare"}}
	s := newScreen(t, 100, 30, func(s *screen) { s.opts.Recipes = recipe.Set{bare} })
	require.Empty(t, s.lineWith("Model"))
	s.typing("go").press("ctrl+s")
	require.Empty(t, s.result().Model)
}

func TestWorktreeFollowsTheDirectory(t *testing.T) {
	s := newScreen(t, 100, 30).typing("do it").press("tab", "tab", "tab")
	require.Equal(t, "Directory", s.focusedLabel())
	require.Contains(t, s.lineWith("Worktree"), "[yes]")

	s.press("ctrl+u").typing("~/notes")
	require.Empty(t, s.lineWith("Worktree"), "~/notes is not a git repository")

	s.press("ctrl+u").typing("~/code/site")
	require.Contains(t, s.lineWith("Worktree"), "[yes]")
	s.press("tab", "tab", "tab", "tab", "tab", "h")
	require.Contains(t, s.lineWith("Worktree"), "[no]")

	s.press("ctrl+s")
	require.Equal(t, filepath.Join(s.home, "code", "site"), s.result().Dir)
	require.False(t, s.result().Worktree)
}

func TestWorktreeLeftOffInARepositoryUntilAsked(t *testing.T) {
	s := newScreen(t, 100, 30, func(s *screen) { s.opts.WorktreeDefault = false })
	require.Contains(t, s.lineWith("Worktree"), "[no]")
	s.typing("do it").press("ctrl+s")
	require.False(t, s.result().Worktree)
}

func TestWorktreeNeverOutsideARepository(t *testing.T) {
	s := newScreen(t, 100, 30, notInRepo).typing("do it").press("ctrl+s")
	require.False(t, s.result().Worktree)
}

func TestFocusStaysOnTheFormWhenWorktreeGoes(t *testing.T) {
	s := newScreen(t, 100, 30).press("shift+tab", "shift+tab")
	require.Equal(t, "Worktree", s.focusedLabel())
	s.repos[s.dir] = false
	s.press("shift+tab", "shift+tab", "shift+tab", "shift+tab", "shift+tab")
	require.Equal(t, "Directory", s.focusedLabel())
	s.typing("/")
	s.press("shift+tab")
	require.Equal(t, "Name", s.focusedLabel())
}

func TestDirectory(t *testing.T) {
	cases := []struct {
		name  string
		typed string
		want  func(s *screen) string
	}{
		{"expands ~", "~/notes", func(s *screen) string { return filepath.Join(s.home, "notes") }},
		{"takes ~ alone as home", "~", func(s *screen) string { return s.home }},
		{"takes a relative path from the starting directory", "../site", func(s *screen) string {
			return filepath.Join(s.home, "code", "site")
		}},
		{"takes an absolute path", "/", func(*screen) string { return "/" }},
		{"takes empty as the starting directory", "", func(s *screen) string { return s.dir }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newScreen(t, 100, 30).typing("do it").press("tab", "tab", "tab", "ctrl+u").typing(tc.typed)
			s.press("ctrl+s")
			require.Equal(t, tc.want(s), s.result().Dir)
		})
	}
}

func TestDirectoryList(t *testing.T) {
	t.Run("opens while Directory has focus", func(t *testing.T) {
		s := newScreen(t, 100, 30).press("tab", "tab", "tab")
		require.Contains(t, s.frame(), "  ..")
		s.press("tab")
		require.NotContains(t, s.frame(), "  ..")
	})

	t.Run("Tab completes a fragment, then moves on", func(t *testing.T) {
		s := newScreen(t, 100, 30)
		s.mkdir("src/apple", "src/banana")
		s.press("tab", "tab", "tab", "ctrl+u").typing("~/src/b").press("tab")
		require.Contains(t, s.lineWith("Directory"), "~/src/banana/")
		require.Contains(t, s.footer(), "⇥ next")
		s.press("tab")
		require.Equal(t, "Rex", s.focusedLabel())
	})

	t.Run("Worktree follows the directory browsed to", func(t *testing.T) {
		s := newScreen(t, 100, 30).typing("do it").press("tab", "tab", "tab")
		s.press("down", "left")
		require.Contains(t, s.lineWith("Directory"), "~/code/")
		require.Empty(t, s.lineWith("Worktree"), "~/code is not a git repository")
		s.press("down", "enter")
		require.Contains(t, s.lineWith("Directory"), "~/code/site/")
		require.Contains(t, s.lineWith("Worktree"), "[yes]")
		s.press("ctrl+s")
		require.Equal(t, filepath.Join(s.home, "code", "site"), s.result().Dir)
	})

	t.Run("Enter on a recent takes it and moves on", func(t *testing.T) {
		s := newScreen(t, 100, 30, recents).typing("do it").press("tab", "tab", "tab", "down", "down", "enter")
		require.Equal(t, "Rex", s.focusedLabel())
		s.press("ctrl+s")
		require.Equal(t, filepath.Join(s.home, "notes"), s.result().Dir)
	})

	t.Run("Esc leaves the list before it cancels", func(t *testing.T) {
		s := newScreen(t, 100, 30).typing("h").press("tab", "tab", "tab", "down", "esc")
		require.Contains(t, s.frame(), "New agent")
		require.Contains(t, s.footer(), "esc cancel")
		s.press("esc")
		require.Contains(t, s.frame(), "Discard this prompt? (y/n)")
	})
}

func TestStartRefusesAnEmptyPrompt(t *testing.T) {
	s := newScreen(t, 100, 30).press("tab", "ctrl+s")
	require.False(t, s.quit)
	require.Contains(t, s.footer(), "a prompt is required")
	require.Equal(t, "Prompt", s.focusedLabel())
	_, ok := s.model.Submitted()
	require.False(t, ok)
}

func TestStartRefusesAMissingDirectory(t *testing.T) {
	s := newScreen(t, 100, 30).typing("do it").press("tab", "tab", "tab", "ctrl+u").typing("/nope/nowhere")
	s.press("ctrl+s")
	require.Contains(t, s.footer(), "no such directory: /nope/nowhere")
	require.Equal(t, "Directory", s.focusedLabel())
	s.press("left")
	require.NotContains(t, s.footer(), "no such directory", "the next key clears it")
}

func TestEscape(t *testing.T) {
	t.Run("cancels at once when the prompt is empty", func(t *testing.T) {
		s := newScreen(t, 100, 30).press("esc")
		require.True(t, s.quit)
		_, ok := s.model.Submitted()
		require.False(t, ok)
	})

	t.Run("asks first once the prompt has text", func(t *testing.T) {
		s := newScreen(t, 100, 30).typing("h").press("esc")
		require.False(t, s.quit)
		require.Contains(t, s.frame(), "Discard this prompt? (y/n)")
		require.Contains(t, s.footer(), "y discard")
	})

	t.Run("keeps editing on n or esc", func(t *testing.T) {
		for _, key := range []string{"n", "esc", "q"} {
			s := newScreen(t, 100, 30).typing("h").press("esc", key)
			require.False(t, s.quit)
			require.Contains(t, s.frame(), "New agent")
			s.press("ctrl+s")
			require.Equal(t, "h", s.result().Prompt, "the prompt is untouched after %s", key)
		}
	})

	t.Run("discards on y", func(t *testing.T) {
		s := newScreen(t, 100, 30).typing("h").press("esc", "y")
		require.True(t, s.quit)
		_, ok := s.model.Submitted()
		require.False(t, ok)
	})

	t.Run("Ctrl-C cancels without asking", func(t *testing.T) {
		s := newScreen(t, 100, 30).typing("h").press("ctrl+c")
		require.True(t, s.quit)
	})
}

func TestOptionSlips(t *testing.T) {
	s := newScreen(t, 100, 30).typing("h").press("alt+s")
	require.Contains(t, s.footer(), "ctrl-s starts it")
	s.press("alt+o")
	require.Contains(t, s.footer(), "ctrl-o starts it")
	require.NotContains(t, s.frame(), "Discard")
	s.press("ctrl+s")
	require.Equal(t, "h", s.result().Prompt)
}

func TestRexSession(t *testing.T) {
	t.Run("starts on the current session", func(t *testing.T) {
		s := newScreen(t, 100, 30, func(s *screen) { s.opts.CurrentSession = "s2" })
		require.Contains(t, s.lineWith("Rex session"), "[side projects]")
	})

	t.Run("offers a new session after the others", func(t *testing.T) {
		s := newScreen(t, 100, 30).typing("do it").press("tab", "tab", "tab", "tab", "l", "l")
		require.Contains(t, s.lineWith("Rex session"), "[new session]")
		s.press("ctrl+s")
		require.Empty(t, s.result().Session)
	})

	t.Run("starts on a new session with no current one", func(t *testing.T) {
		s := newScreen(t, 100, 30, func(s *screen) { s.opts.Sessions, s.opts.CurrentSession = nil, "" })
		require.Contains(t, s.lineWith("Rex session"), "[new session]")
	})
}

func TestName(t *testing.T) {
	t.Run("shows the name it would get from the prompt", func(t *testing.T) {
		s := newScreen(t, 100, 30).typing("Fix the flaky spec in billing")
		require.Contains(t, s.lineWith("Name"), "Fix the flaky spec in billing")
		s.press("ctrl+s")
		require.Empty(t, s.result().Name, "the caller fills an empty name")
	})

	t.Run("takes a typed name", func(t *testing.T) {
		s := newScreen(t, 100, 30).typing("do it").press("tab", "tab").typing("billing ").press("ctrl+s")
		require.Equal(t, "billing", s.result().Name)
	})
}

func TestPastingImages(t *testing.T) {
	t.Run("attaches the clipboard's image on an empty paste", func(t *testing.T) {
		s := newScreen(t, 100, 30)
		s.clip.image = true
		s.typing("match ").paste("")
		require.Contains(t, s.lineWith("match"), "match [Image #1]")
		s.press("ctrl+s")
		image := filepath.Join(s.home, "images", "20261123-093000.000.png")
		require.Equal(t, "match @"+image, s.result().Prompt)
		require.FileExists(t, image)
	})

	t.Run("attaches it on Ctrl-V too", func(t *testing.T) {
		s := newScreen(t, 100, 30)
		s.clip.image = true
		s.press("ctrl+v")
		require.Contains(t, s.lineWith("│"), "[Image #1]")
	})

	t.Run("pastes the clipboard's text when that is what it holds", func(t *testing.T) {
		s := newScreen(t, 100, 30)
		s.clip.text = "fix it"
		s.press("ctrl+v", "ctrl+s")
		require.Equal(t, "fix it", s.result().Prompt)
	})

	t.Run("says when the clipboard is empty", func(t *testing.T) {
		s := newScreen(t, 100, 30).press("ctrl+v")
		require.Contains(t, s.footer(), "nothing on the clipboard")
	})

	t.Run("keeps an image out of the one-line fields", func(t *testing.T) {
		s := newScreen(t, 100, 30)
		s.clip.image = true
		s.typing("do it").press("tab", "tab").paste("")
		require.Contains(t, s.footer(), "images go in the prompt")
		s.press("ctrl+s")
		require.Empty(t, s.result().Name)
	})
}

func TestMentionsFollowTheHarness(t *testing.T) {
	cases := []struct {
		harnessSteps int
		want         func(path string) string
	}{
		{0, func(path string) string { return "see @" + strings.ReplaceAll(path, " ", `\ `) }},
		{1, func(path string) string { return "see " + path }},
		{2, func(path string) string { return "see " + path }},
	}
	for _, tc := range cases {
		s := newScreen(t, 100, 30)
		s.write("shots/a b.png", "png")
		shot := filepath.Join(s.home, "shots", "a b.png")
		s.typing("see ").paste(strings.ReplaceAll(shot, " ", `\ `) + " ")
		s.press("tab").press(repeat("l", tc.harnessSteps)...).press("ctrl+s")
		require.Equal(t, tc.want(shot), s.result().Prompt, s.result().Harness)
	}
}

func TestDroppingFiles(t *testing.T) {
	t.Run("attaches a dropped image and keeps any other text as text", func(t *testing.T) {
		s := newScreen(t, 100, 30)
		s.write("a.png", "png")
		s.typing("see ").paste(filepath.Join(s.home, "a.png") + " ").paste(" not " + s.home + "/nope.txt")
		require.Contains(t, s.lineWith("see"), "see [Image #1] not ")
		s.press("ctrl+s")
		require.Equal(t, "see @"+s.home+"/a.png not "+s.home+"/nope.txt", s.result().Prompt)
	})

	t.Run("attaches any dropped file, several at once", func(t *testing.T) {
		s := newScreen(t, 100, 30)
		s.write("notes.md", "")
		s.write("b.png", "png")
		s.paste(s.home + "/notes.md " + s.home + "/b.png")
		require.Contains(t, s.lineWith("│"), "[notes.md] [Image #1]")
		s.press("ctrl+s")
		require.Equal(t, "@"+s.home+"/notes.md @"+s.home+"/b.png", s.result().Prompt)
	})

	t.Run("types a dropped path into a one-line field", func(t *testing.T) {
		s := newScreen(t, 100, 30).typing("do it").press("tab", "tab", "tab", "ctrl+u")
		s.paste(s.home + "/notes")
		s.press("ctrl+s")
		require.Equal(t, filepath.Join(s.home, "notes"), s.result().Dir)
	})
}

func TestPastingText(t *testing.T) {
	t.Run("keeps a multi-line paste in the prompt and flattens it in a one-line field", func(t *testing.T) {
		s := newScreen(t, 100, 30).paste("one\r\ntwo\rthree").press("tab", "tab").paste("my\nname").press("ctrl+s")
		require.Equal(t, "one\ntwo\nthree", s.result().Prompt)
		require.Equal(t, "my name", s.result().Name)
	})

	t.Run("folds a long paste into a token and starts with all of it", func(t *testing.T) {
		var log []string
		for i := range 40 {
			log = append(log, "error "+string(rune('a'+i%26)))
		}
		s := newScreen(t, 100, 30).typing("why ").paste(strings.Join(log, "\n"))
		require.Contains(t, s.lineWith("why"), "[Pasted text #1 +39 lines]")
		s.press("ctrl+s")
		require.Equal(t, "why "+strings.Join(log, "\n"), s.result().Prompt)
	})

	t.Run("ignores a paste on a picker", func(t *testing.T) {
		s := newScreen(t, 100, 30).typing("do it").press("tab").paste("codex").press("ctrl+s")
		require.Equal(t, "claude", s.result().Harness)
	})
}

func TestSlashMenu(t *testing.T) {
	open := func(t *testing.T) *screen {
		t.Helper()
		return newScreen(t, 100, 30, skills)
	}

	t.Run("opens on a slash and narrows as you type", func(t *testing.T) {
		s := open(t).typing("/")
		for _, name := range []string{"/babysit", "/code-review", "/deploy", "/unslop", "/unsplit"} {
			require.NotEmpty(t, s.lineWith(name), name)
		}
		require.Contains(t, s.footer(), "pick")
		s.typing("uns")
		require.Contains(t, s.lineWith("/unsplit"), "unsplit does things")
		require.Empty(t, s.lineWith("/deploy"))
		require.NotEmpty(t, s.lineWith("Worktree"))
		s.typing("zz")
		require.Empty(t, s.lineWith("/unslop"))
	})

	t.Run("picks with Tab or Enter, leaving a space after the command", func(t *testing.T) {
		s := open(t).typing("/unsl").press("tab")
		require.Equal(t, "Prompt", s.focusedLabel())
		require.Empty(t, s.lineWith("unslop does things"))
		s.typing("the readme").press("enter").typing("/dep").press("enter", "ctrl+s")
		require.Equal(t, "/unslop the readme\n/deploy", s.result().Prompt)
	})

	t.Run("moves the pick with the arrows, wrapping around", func(t *testing.T) {
		s := open(t).typing("/").press("down")
		require.Contains(t, s.lineWith("▶ /"), "/code-review")
		s.press("up", "up")
		require.Contains(t, s.lineWith("▶ /"), "/unsplit")
		s.press("enter", "ctrl+s")
		require.Equal(t, "/unsplit", s.result().Prompt)
	})

	t.Run("closes on Esc until the query changes", func(t *testing.T) {
		s := open(t).typing("/un").press("esc")
		require.Empty(t, s.lineWith("/unslop"))
		require.NotContains(t, s.frame(), "Discard")
		s.press("tab")
		require.Equal(t, "Harness", s.focusedLabel())
		s.press("shift+tab")
		require.Empty(t, s.lineWith("/unslop"))
		s.typing("s")
		require.NotEmpty(t, s.lineWith("/unslop"))
		s.press("esc", "esc")
		require.Contains(t, s.footer(), "discard")
	})

	t.Run("offers commands for a slash word anywhere, but not mid-word", func(t *testing.T) {
		s := open(t).typing("first do").press("enter").typing("then /uns")
		require.NotEmpty(t, s.lineWith("/unsplit"))
		s.press("tab").typing("a/b")
		require.Empty(t, s.lineWith("/unsplit"))
		s.press("ctrl+u").typing("/unslop x")
		require.Empty(t, s.lineWith("unslop does things"))
		s.press("left", "left")
		require.NotEmpty(t, s.lineWith("unslop does things"))
		s.press("tab", "ctrl+s")
		require.Equal(t, "/unslop  x", s.result().Prompt, "Ctrl-U kills back to the start")
	})

	t.Run("keeps the pick in view and counts the rest on the last row", func(t *testing.T) {
		s := newScreen(t, 80, 24)
		for _, n := range "abcdefghij" {
			s.write(".claude/skills/cmd-"+string(n)+"/SKILL.md", "---\ndescription: "+string(n)+"\n---\n")
		}
		s.typing("/")
		require.Contains(t, s.lineWith("/cmd-f"), "+4 more")
		require.Empty(t, s.lineWith("/cmd-g"))
		s.press(repeat("down", 7)...)
		require.Empty(t, s.lineWith("/cmd-b"))
		require.NotEmpty(t, s.lineWith("/cmd-c"))
		require.Contains(t, s.lineWith("/cmd-h"), "+2 more")
	})

	t.Run("reads the chosen harness's own commands", func(t *testing.T) {
		s := open(t)
		s.write(".codex/prompts/explain.md", "---\ndescription: Explain it\n---\n")
		s.press("tab", "l", "shift+tab").typing("/")
		require.Contains(t, s.lineWith("/explain"), "Explain it")
		require.Empty(t, s.lineWith("/unslop"))
	})

	t.Run("stays shut with no commands", func(t *testing.T) {
		s := newScreen(t, 100, 30).typing("/")
		require.Contains(t, s.footer(), "newline")
	})
}

func TestTheFrameFitsItsLayer(t *testing.T) {
	for _, size := range []struct{ width, height int }{{50, 16}, {60, 20}, {120, 40}} {
		s := newScreen(t, size.width, size.height, skills)
		s.typing(strings.Repeat("a long prompt that wraps ", 30))
		lines := s.lines()
		require.Len(t, lines, size.height)
		for _, line := range lines {
			require.LessOrEqual(t, len([]rune(line)), size.width)
		}
		require.Contains(t, s.footer(), "^S")
	}
}

func TestNoFrameBeforeTheSizeIsKnown(t *testing.T) {
	m := form.New(form.Options{Recipes: recipe.Builtins(), Dir: os.TempDir()})
	require.Empty(t, m.View().Content)
}

func TestAWrappedRowsTrailingSpaceIsNotCut(t *testing.T) {
	s := newScreen(t, 50, 16).typing("ten Xeleven twelve thirteen fourteen fifteen sixteen")
	require.Contains(t, s.frame(), "│ ten Xeleven twelve thirteen fourteen fifteen │")
}

func TestNoRecipesIsNoPanic(t *testing.T) {
	m := form.New(form.Options{Dir: os.TempDir()})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	require.Contains(t, ansi.Strip(next.View().Content), "New agent")
}
