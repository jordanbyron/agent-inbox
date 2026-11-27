# agent-inbox

A Go CLI and TUI that triages coding agents running in Rex.
[docs/spec.md](docs/spec.md) is the design; read it before touching
`internal/`.

## Submitting a PR

Follow [CONTRIBUTING.md](CONTRIBUTING.md). The part that matters for an agent:
there is no hosted CI. A PR is ready only once `bin/ci` has passed on the
pushed HEAD and set the green `signoff` status; run it from the branch after
the final push, and confirm with `gh signoff status`.

Work in a git worktree (`.claude/worktrees/<name>`, gitignored), never in the
shared checkout: other sessions commit there concurrently.

## Conventions

- The program is `cmd/agent-inbox/main.go` plus packages under `internal/`,
  one per concern, each named for what it holds (`triage`, `state`, `rex`).
  No `util` or `common` package. `cmd/release` is the maintainer's release
  tool behind `bin/release`, not part of the program.
- A package that needs something from outside (Rex, `gh`, git, the clock)
  declares the small interface it uses and takes it as an argument. Tests
  pass a fake; there is no mocking library.
- Errors are returned, wrapped with `fmt.Errorf("doing x: %w", err)` where
  the caller needs the context. Only `main` exits, and nothing panics on
  bad input.
- Tests sit beside the code as `*_test.go` in the `_test` package, so they
  use the package as a caller would. Assertions are testify's `require`.
  Several cases of one rule are a table of structs run with `t.Run`.
- Format and lint are whatever `.golangci.yml` says; a `//nolint` names its
  linter and its reason.
- Every commit and every PR title is a
  [conventional commit](https://www.conventionalcommits.org/):
  `type: what changed` (`feat:`, `fix:`, `refactor:`, `docs:`, `chore:`),
  with `!` or a `BREAKING CHANGE:` footer for a breaking change. The body
  says why, in prose. PRs are squash-merged, and `bin/release` picks the
  version and labels the notes from these prefixes, so a PR title without
  one makes that guess from its commits.
- A comment records a decision, a constraint or a Rex quirk the code cannot
  show, in a line or two. Exported names get the one-line doc comment Go
  expects. Code that needs a comment to be read is code to rewrite until it
  doesn't.
