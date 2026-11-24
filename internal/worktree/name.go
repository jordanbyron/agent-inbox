package worktree

import (
	"strings"
)

const (
	nameWords = 4
	nameMax   = 40
)

// NameFromPrompt makes a short kebab-case agent name from a prompt's first
// words, or "agent" when the prompt has none.
func NameFromPrompt(prompt string) string {
	words := strings.FieldsFunc(strings.ToLower(prompt), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	name := ""
	for _, word := range words[:min(len(words), nameWords)] {
		next := word
		if name != "" {
			next = name + "-" + word
		}
		if len(next) > nameMax {
			break
		}
		name = next
	}
	if name == "" {
		return "agent"
	}
	return name
}
