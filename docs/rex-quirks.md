# Rex quirks

What Rex and the harnesses were seen to do, as opposed to what their docs
say. Each entry was observed on 2026-11-23 with Rex 0.1.0-preview.21 (API
revision 18), Claude Code 2.1.331, Codex 0.165.0 and agy 1.5.14, unless it
says otherwise. Add the version and date to anything new.

## The Rex CLI and API

- `rex` is on `PATH` only inside a Rex pane, which also gets `REX_BIN_DIR`.
  Elsewhere on macOS it is
  `/Applications/Rex Beta.app/Contents/Helpers/Rex Server.rex/Contents/MacOS/rex`.
- The API describes itself. `rex api list` names every method, `rex api
  describe METHOD` prints its schemas, and `rex api call -s SESSION METHOD
  JSON` calls it. `session.describe_block` lists the methods and events one
  block offers.
- A terminal block's own methods go through `block.method`:
  `rex api call -s SESSION block.method '{"block_id":"…","method":"program_status"}'`.
- Status is per block. `rex api list` has no call that returns every
  block's status, so a watcher asks each block once and then follows the
  block events `program_status_changed` and `program_status_removed`.
- `program_status` returns `records`, each with `state`, `app`,
  `effective_app`, `updated_at` and an `owner` (`pid`, `name`), plus `kind`,
  `msg`, `title` and `id` when the program sent them. The root record has no
  `id`.
- A block's label is set at creation with `rex run --label` or later with
  `session.set_block_label`, and comes back in `session.list_blocks`.
- `rex new LABEL -- COMMAND` makes a session that no window of the app is
  showing, which is a quiet place to run a probe. `rex kill LABEL` removes it.

## What each harness reports

All three report OSC 7501 with no hook or setting.

| | Claude Code | Codex | agy |
|---|---|---|---|
| `app` | `claude-code` | `codex` | `antigravity` |
| At work | `working` | not seen | not seen |
| Permission prompt | `blocked`, `kind: permission`, `msg: "approve Bash: curl -sI https://example.com"` | not seen | not seen |
| After a declined prompt | `idle` | not seen | not seen |
| Startup trust prompt | not seen | no record at all, on "Hooks need review" | `blocked`, `kind: permission`, `msg: "Trust this folder?"` |
| Failed turn | not seen | `error` | not seen |
| Sub-agent | a child record: `id` is the sub-agent's id, `title` its description, `state: working` | not seen | not seen |

- A Codex stopped on its hooks prompt has no status record, so it reads as
  `starting` until someone answers.
- Claude Code's status passes through a pty relay unchanged: a session
  attached with `claude-inbox attach` reports the same records, with the
  relay's process as `owner`.
- Claude Code draws on the alternate screen (`modes` returns `"screen":
  "alternate"`), so `rex capture` gives the visible screen and little else.

## Session ids

- Claude Code accepts `--session-id UUID` and `--name` at start.
- Codex has no start flag for a session id or name. `codex resume` takes a
  session id or a session name.
- agy resumes with `--conversation ID` and has no start flag for choosing it.

For Codex and agy the recipe has to learn the id after the start. How is
not worked out.

## Not checked yet

- `done`, and the `question` and `auth` kinds, for any harness.
- `working` and `idle` for Codex and agy.
- Which events fire when a tab closes.
- Where Codex and agy record a new session's id.
- What `rex capture` returns for Codex and agy.
- `claude --resume` in a new block.
