package rex_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/rex"
)

func TestOnlyAgentIn(t *testing.T) {
	cases := []struct {
		name   string
		labels []string
		want   bool
	}{
		{name: "the agent alone", labels: []string{"agent-inbox:abc123"}, want: true},
		{name: "the agent and its parked inbox", labels: []string{rex.ParkedLabel, "agent-inbox:abc123"}, want: true},
		{name: "the agent and a shell", labels: []string{"agent-inbox:abc123", "shell"}, want: false},
		{name: "the agent, its parked inbox and a shell", labels: []string{rex.ParkedLabel, "agent-inbox:abc123", ""}, want: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, rex.OnlyAgentIn(c.labels))
		})
	}
}
