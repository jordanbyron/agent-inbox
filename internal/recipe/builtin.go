package recipe

var efforts = []string{"default", "low", "medium", "high", "xhigh", "max"}

// Builtins returns the recipes agent-inbox ships, each checked against its
// harness's --help.
func Builtins() Set {
	return Set{
		{
			Name: "claude",
			App:  "claude-code",
			Start: []string{
				"claude", "--session-id", "{session_id}", "--name", "{name}",
				"--model", "{model}", "--effort", "{effort}",
				"--permission-mode", "{permissions}", "--", "{prompt}",
			},
			Resume:   []string{"claude", "--resume", "{session_id}"},
			Mention:  "@{path}",
			Commands: []string{"~/.claude/skills", "~/.claude/commands"},
			Choices: Choices{
				Model:       []string{"default", "fable", "opus", "sonnet", "haiku"},
				Effort:      efforts,
				Permissions: []string{"default", "acceptEdits", "auto", "plan", "bypassPermissions"},
			},
		},
		{
			Name: "codex",
			App:  "codex",
			// Codex has no effort flag, so effort goes in as a config
			// override. Each permissions choice sets both the sandbox and
			// the approval policy, as claude-inbox did.
			Start: []string{
				"codex", "--model", "{model}", "-c", "model_reasoning_effort={effort}",
				"--sandbox", "{permissions}", "--", "{prompt}",
			},
			PermissionArgs: map[string][]string{
				"read-only":          {"--sandbox", "read-only", "--ask-for-approval", "on-request"},
				"workspace-write":    {"--sandbox", "workspace-write", "--ask-for-approval", "on-request"},
				"danger-full-access": {"--sandbox", "danger-full-access", "--ask-for-approval", "never"},
			},
			Resume:       []string{"codex", "resume", "{session_id}"},
			ResumeLatest: []string{"codex", "resume", "--last"},
			Mention:      "{path}",
			Commands:     []string{"~/.codex/skills", "~/.codex/prompts"},
			Choices: Choices{
				Model:       []string{"default", "gpt-6-luna", "gpt-5.6-terra", "gpt-5.6-luna"},
				Effort:      efforts,
				Permissions: []string{"default", "read-only", "workspace-write", "danger-full-access"},
			},
			Sessions: CodexSessions{},
		},
		{
			Name: "agy",
			App:  "antigravity",
			Start: []string{
				"agy", "--model", "{model}", "--effort", "{effort}",
				"--mode", "{permissions}", "--prompt-interactive", "{prompt}",
			},
			Resume:       []string{"agy", "--conversation", "{session_id}"},
			ResumeLatest: []string{"agy", "--continue"},
			Mention:      "{path}",
			Commands:     []string{"~/.gemini/config/skills"},
			Choices: Choices{
				Model:       []string{"default"},
				Effort:      efforts,
				Permissions: []string{"default", "accept-edits", "auto", "plan"},
			},
			Sessions: AgySessions{},
		},
	}
}
