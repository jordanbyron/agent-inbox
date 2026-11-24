package luasnippet_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/golden"
	"github.com/jordanbyron/agent-inbox/internal/luasnippet"
)

func TestSnippet(t *testing.T) {
	golden.Require(t, "snippet", luasnippet.For("/Users/me/go/bin/agent-inbox"))
}

func TestQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/usr/local/bin/agent-inbox", `"/usr/local/bin/agent-inbox"`},
		{`/Users/o"brien/bin/agent-inbox`, `"/Users/o\"brien/bin/agent-inbox"`},
		{`C:\bin`, `"C:\\bin"`},
		{"/tmp/new\nline", `"/tmp/new\010line"`},
		{"/Users/zoë/bin", `"/Users/zoë/bin"`},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			require.Equal(t, c.want, luasnippet.Quote(c.in))
		})
	}
}
