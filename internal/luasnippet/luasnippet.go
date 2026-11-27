// Package luasnippet writes the init.lua snippet that gives Rex its
// agent-inbox actions.
package luasnippet

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed snippet.lua
var snippet string

// For runs the snippet's actions through the agent-inbox binary at bin.
func For(bin string) string {
	return strings.Replace(snippet, "AGENT_INBOX", quote(bin), 1)
}

// quote writes s as a Lua 5.1 string, which has decimal escapes and no \x
// or \u, so Go's own quoting won't do.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := range len(s) {
		switch c := s[i]; {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c < 0x20 || c == 0x7f:
			fmt.Fprintf(&b, "\\%03d", c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}
