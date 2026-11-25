package worktree

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	titleMax  = 48
	slugWords = 5
	slugMax   = 40
)

// NameFromPrompt makes an agent's name from the prompt's first sentence,
// cut at a word to fit a tab, or "Agent" when the prompt has no words.
// The name is for people; Slug makes the branch and path from it.
func NameFromPrompt(prompt string) string {
	words := strings.Fields(firstSentence(prompt))
	title := ""
	for i, word := range words {
		next := strings.TrimSpace(title + " " + word)
		if utf8.RuneCountInString(next) > titleMax {
			if title == "" {
				title = string([]rune(word)[:titleMax-1])
			}
			title = strings.TrimRight(title, ",;:-–— ") + "…"
			break
		}
		title = next
		if i == len(words)-1 {
			title = strings.TrimRight(title, ".:,;")
		}
	}
	if !strings.ContainsFunc(title, isWordRune) {
		return "Agent"
	}
	first, size := utf8.DecodeRuneInString(title)
	return string(unicode.ToUpper(first)) + title[size:]
}

// firstSentence is the prompt up to its first line break, or the first
// full stop, question or exclamation mark followed by a space.
func firstSentence(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if line, _, ok := strings.Cut(prompt, "\n"); ok {
		prompt = line
	}
	for i, r := range prompt {
		if r != '.' && r != '?' && r != '!' {
			continue
		}
		if rest := prompt[i+1:]; rest == "" || rest[0] == ' ' {
			return prompt[:i+1]
		}
	}
	return prompt
}

// Slug makes a kebab-case branch and directory name from an agent's name:
// its first few words that say something, in lowercase ASCII.
func Slug(name string) string {
	words := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	meaningful := slices.DeleteFunc(slices.Clone(words), func(w string) bool { return filler[w] })
	if len(meaningful) > 0 {
		words = meaningful
	}
	slug := ""
	for _, word := range words[:min(len(words), slugWords)] {
		next := word
		if slug != "" {
			next = slug + "-" + word
		}
		if len(next) > slugMax {
			break
		}
		slug = next
	}
	if slug == "" {
		return "agent"
	}
	return slug
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// filler is the words a branch name reads as well without.
var filler = map[string]bool{
	"a": true, "an": true, "the": true, "this": true, "that": true, "these": true, "those": true,
	"of": true, "to": true, "for": true, "in": true, "on": true, "at": true, "by": true, "with": true,
	"and": true, "or": true, "so": true, "is": true, "are": true, "be": true, "it": true, "its": true,
	"i": true, "we": true, "you": true, "me": true, "my": true, "our": true,
	"can": true, "could": true, "would": true, "should": true, "do": true, "does": true,
	"please": true, "just": true, "hey": true, "hi": true, "why": true, "how": true, "hmw": true,
}
