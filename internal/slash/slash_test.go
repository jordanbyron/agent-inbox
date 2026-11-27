package slash_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/slash"
)

var claudeDirs = []string{"~/.claude/skills", "~/.claude/commands"}

func write(t *testing.T, files map[string]string) {
	t.Helper()
	for path, body := range files {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	}
}

func names(commands []slash.Command) []string {
	out := []string{}
	for _, c := range commands {
		out = append(out, c.Name)
	}
	return out
}

func TestListsProjectAndHomeCommands(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	write(t, map[string]string{
		home + "/.claude/skills/unslop/SKILL.md":            "---\ndescription: Cut AI tells.\n---\n",
		home + "/.claude/skills/shared/SKILL.md":            "---\ndescription: from home\n---\n",
		project + "/.claude/skills/shared/SKILL.md":         "---\ndescription: from project\n---\n",
		project + "/.claude/commands/deploy.md":             "---\ndescription: Ship it\n---\nDeploy $ARGUMENTS",
		home + "/.claude/commands/frontend/component.md":    "Make a component",
		home + "/.claude/skills/not-a-skill/README.md":      "no SKILL.md here",
		home + "/.claude/commands/frontend/notes/draft.txt": "not markdown",
	})
	list := slash.List(slash.Where{Dirs: claudeDirs, Project: project, Home: home})
	require.Equal(t, []slash.Command{
		{Name: "component", Description: ""},
		{Name: "deploy", Description: "Ship it"},
		{Name: "shared", Description: "from project"},
		{Name: "unslop", Description: "Cut AI tells."},
	}, list)
	require.Equal(t, "/component", list[0].String())
}

func TestReadsDescriptions(t *testing.T) {
	home := t.TempDir()
	cases := map[string]string{
		"folded": "description: >-\n  Send a push notification\n  when something happens.\n",
		"quoted": "description: 'It''s quoted: with a colon'\n",
		"dq":     "description: \"Double quoted\"\n",
		"hidden": "description: Not for the menu\nuser-invocable: false\n",
		"none":   "name: none\n",
	}
	for name, meta := range cases {
		write(t, map[string]string{home + "/.claude/skills/" + name + "/SKILL.md": "---\n" + meta + "---\n"})
	}
	by := map[string]string{}
	for _, c := range slash.List(slash.Where{Dirs: claudeDirs, Home: home}) {
		by[c.Name] = c.Description
	}
	require.Equal(t, map[string]string{
		"folded": "Send a push notification",
		"quoted": "It's quoted: with a colon",
		"dq":     "Double quoted",
		"none":   "",
	}, by)
}

func TestNamespacesPluginAndSyncedSkills(t *testing.T) {
	home := t.TempDir()
	plugin := home + "/.claude/plugins/cache/official/skill-creator/abc"
	write(t, map[string]string{
		plugin + "/skills/skill-creator/SKILL.md":                          "---\ndescription: Make skills\n---\n",
		plugin + "/commands/eval.md":                                       "---\ndescription: Run evals\n---",
		home + "/.claude/skills/synced/bucket-1/docs/SKILL.md":             "---\ndescription: Living docs\n---\n",
		home + "/.claude/plugins/installed_plugins.json":                   `{"version":2,"plugins":{"skill-creator@official":[{"scope":"user","installPath":"` + plugin + `"}]}}`,
		home + "/.claude/settings.json":                                    `{"enabledPlugins":{"skill-creator@official":true}}`,
		home + "/.claude/plugins/cache/official/other/1/skills/x/SKILL.md": "---\ndescription: not installed\n---\n",
	})
	list := slash.List(slash.Where{Dirs: claudeDirs, Home: home, Plugins: true})
	require.Equal(t, []string{"anthropic-skills:docs", "skill-creator:eval", "skill-creator:skill-creator"}, names(list))
}

func TestPluginsOnlyWhereSettingsEnableThem(t *testing.T) {
	home, project, other := t.TempDir(), t.TempDir(), t.TempDir()
	review := home + "/.claude/plugins/cache/m/dev-workflow/1"
	write(t, map[string]string{
		review + "/skills/pr-review/SKILL.md":            "---\ndescription: Review a PR\n---\n",
		home + "/.claude/plugins/installed_plugins.json": `{"version":2,"plugins":{"dev-workflow@m":{"scope":"project","installPath":"` + review + `"}}}`,
		project + "/.claude/settings.json":               `{"enabledPlugins":{"dev-workflow@m":true}}`,
		other + "/.claude/settings.local.json":           `{"enabledPlugins":{"dev-workflow@m":true}}`,
		project + "/app/.keep":                           "",
	})
	cases := []struct {
		name    string
		project string
		want    []string
	}{
		{"enabled in the project's settings", project, []string{"dev-workflow:pr-review"}},
		{"enabled in another project's local settings", other, []string{"dev-workflow:pr-review"}},
		{"not in a subdirectory of the project", project + "/app", []string{}},
		{"not where nothing enables it", home, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			list := slash.List(slash.Where{Dirs: claudeDirs, Project: tc.project, Home: home, Plugins: true})
			require.Equal(t, tc.want, names(list))
		})
	}
}

func TestLaterSettingsTurnAPluginOff(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	root := home + "/p"
	write(t, map[string]string{
		root + "/commands/go.md":                         "",
		home + "/.claude/plugins/installed_plugins.json": `{"plugins":{"p@m":[{"installPath":"` + root + `"}]}}`,
		home + "/.claude/settings.json":                  `{"enabledPlugins":{"p@m":true}}`,
		project + "/.claude/settings.local.json":         `{"enabledPlugins":{"p@m":false}}`,
	})
	require.Empty(t, slash.List(slash.Where{Dirs: claudeDirs, Project: project, Home: home, Plugins: true}))
}

func TestReadsOtherHarnessesDirectories(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	write(t, map[string]string{
		home + "/.codex/skills/review/SKILL.md":      "---\ndescription: Review\n---\n",
		home + "/.codex/prompts/explain.md":          "---\ndescription: Explain it\n---\n",
		project + "/.codex/prompts/release.md":       "",
		home + "/.claude/skills/unslop/SKILL.md":     "---\ndescription: Claude's\n---\n",
		home + "/.claude/settings.json":              `{"enabledPlugins":{}}`,
		home + "/.claude/skills/synced/b/x/SKILL.md": "",
	})
	list := slash.List(slash.Where{Dirs: []string{"~/.codex/skills", "~/.codex/prompts"}, Project: project, Home: home})
	require.Equal(t, []string{"explain", "release", "review"}, names(list))
}

func TestCopesWithNothingInstalled(t *testing.T) {
	home := t.TempDir()
	require.Empty(t, slash.List(slash.Where{Dirs: claudeDirs, Project: home, Home: home, Plugins: true}))
}

func TestMatch(t *testing.T) {
	commands := []slash.Command{{Name: "review"}, {Name: "code-review"}, {Name: "unslop"}, {Name: "Babysit"}}
	cases := []struct {
		query string
		want  []string
	}{
		{"re", []string{"review", "code-review"}},
		{"b", []string{"Babysit"}},
		{"", []string{"review", "code-review", "unslop", "Babysit"}},
		{"zz", []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			require.Equal(t, tc.want, names(slash.Match(commands, tc.query)))
		})
	}
}
