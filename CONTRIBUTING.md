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

Against the live Rex server, keep your real state and config out of it:

```sh
export XDG_STATE_HOME=$(mktemp -d) XDG_CONFIG_HOME=$(mktemp -d)
go build -o /tmp/agent-inbox ./cmd/agent-inbox
/tmp/agent-inbox                              # starts a watcher on the scratch state
pkill -f '/tmp/agent-inbox watch'             # stop it when you're done
```

The watcher outlives the command that started it, and a list opened in a
Rex pane runs in the login shell's environment, without these variables.

To draw the list with no Rex at all:

```sh
go run ./cmd/agent-inbox list --fixture internal/snapshot/testdata/fixture.json
```

Two test suites talk to real services and are skipped unless asked for:

```sh
AGENT_INBOX_LIVE_REX=1 go test ./internal/rex  # a throwaway Rex session
AGENT_INBOX_LIVE_GH=1 go test ./internal/pulls # this repository on GitHub
```

## Local CI and signoff

There's no hosted CI. `bin/ci` runs on your machine and reports back with
[gh-signoff](https://github.com/basecamp/gh-signoff).

1. Push your branch and open the PR, its title a conventional commit
   (`feat: …`, `fix: …`) like every commit on it.
2. Run `bin/ci`. It checks formatting, lints, checks `go.mod` is tidy, audits
   dependencies with `govulncheck`, runs the tests with the race detector and
   builds the binary.
3. If every step passes and HEAD is on the remote, it runs `gh signoff`,
   which sets a green `signoff` status on the PR.
4. Squash-merge. The squashed commit takes the PR's title, or a lone
   commit's own subject.

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

## Cutting a release

Releases are git tags; there's no version file. Go stamps the tag into the
binary, so `agent-inbox version` prints it for a build of a tagged, clean
checkout, or for `go install …@vX.Y.Z`. On `main` with a clean tree that
matches `origin/main`:

```sh
bin/release --dry-run   # show the version it would release
bin/release
```

The version comes from the conventional-commit prefixes since the last `v*`
tag: `feat` is a minor bump, `fix` a patch, and `!` or a `BREAKING CHANGE:`
footer a major bump (a minor one while we're on 0.x). With no `feat` or `fix`,
there's nothing to release. The first release is `v0.1.0`.

GitHub generates the notes and groups them by label, as `.github/release.yml`
lays out. Before tagging, `bin/release` gives every PR merged since the last
tag the label its title's prefix calls for. A title with no prefix, such as
one GitHub filled in from the branch, takes the label of its commits'
highest-ranked prefix. Then it pushes the tag, runs `gh release create --generate-notes`, and
moves the Homebrew formula in
[jordanbyron/homebrew-tap](https://github.com/jordanbyron/homebrew-tap) to
the new tag's source tarball. A tarball has no git metadata, so the formula
sets the version with `-ldflags -X …/internal/cli.stampedVersion=vX.Y.Z`.
If the tap step fails after the release is out, finish it with:

```sh
bin/release --formula vX.Y.Z
```
