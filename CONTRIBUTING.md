# Contributing

## Setup

```sh
asdf install                                  # the Go in .tool-versions
gh extension install basecamp/gh-signoff
```

The linters are Go programs pinned in `tools/go.mod`. The first `bin/ci`
builds them, which takes a minute; after that they come from Go's build cache.

## Running from a checkout

```sh
go run ./cmd/agent-inbox version
```

## Local CI and signoff

There's no hosted CI. `bin/ci` runs on your machine and reports back with
[gh-signoff](https://github.com/basecamp/gh-signoff).

1. Push your branch and open the PR.
2. Run `bin/ci`. It checks formatting, lints, checks `go.mod` is tidy, audits
   dependencies with `govulncheck`, runs the tests with the race detector and
   builds the binary.
3. If every step passes and HEAD is on the remote, it runs `gh signoff`,
   which sets a green `signoff` status on the PR.
4. Merge.

With HEAD not pushed yet, `bin/ci` skips the signoff and says so, so it's
fine to run mid-work. gh-signoff also refuses a dirty working tree, since it
can't vouch for commits that don't match what you tested.

While iterating:

```sh
go test ./...
go test ./internal/cli -run TestVersion       # one test
go tool -modfile=tools/go.mod golangci-lint fmt        # fix formatting
go tool -modfile=tools/go.mod golangci-lint run --fix  # lint, fixing what it can
gh signoff status                             # is HEAD signed off?
```
