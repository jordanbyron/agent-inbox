// Command agent-inbox is a triage inbox for coding agents running in Rex.
package main

import (
	"os"

	"github.com/jordanbyron/agent-inbox/internal/cli"
)

func main() {
	if err := cli.New().Execute(); err != nil {
		os.Exit(1)
	}
}
