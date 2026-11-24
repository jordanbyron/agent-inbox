# Rex client notes

What building `internal/rex` showed Rex doing, in the style of
`docs/rex-quirks.md` on the `rex-quirks` branch, to be folded into that file
once both are merged. Each entry was observed on 2026-11-23 with the rex CLI
0.1.0-preview.21 (API revision 18) talking to a server that reported API
revision 10, unless it says otherwise.

## Versions and environment

- The CLI and the server it talks to can be different builds. The app's
  bundled `rex` was revision 18, the running server revision 10, and they
  worked together for everything below.
- Inside a pane, `REX_BIN_DIR` pointed at the server's own copy,
  `~/Library/Application Support/com.superlogical.rex/RexServer/Versions/Current/Rex Server.app/Contents/MacOS`,
  whose `rex version` said revision 10. That is why `FindBinary` tries it
  first.
- A pane also gets `REX_SESSION` and `REX_BLOCK` as ids (`session:<uuid>`,
  `block:<uuid>`) and `REX_SERVER` as a `unix://` URL of the socket.
- `server.status` gives `host_id`, which stays the same across restarts, and
  `instance_id`, which is new for every server process.

## The CLI

- A `rex api call` took about 20 ms here.
- `-s` takes a session's id or label and attaches before the call, which
  every session method needs.
- A failed call prints
  `Error: stream response "control-3" failed: MESSAGE (status=404 code=not_found)`
  on stderr. It exits 1 for `bad_request` and `not_supported`, and 3 for
  `not_found`. The CLI's own lookup prints `Error: session target "x" not
  found`, with no code, and also exits 3.
- `rex capture --unwrap` joins soft-wrapped lines, so a long link comes back
  on one line.

## OSC 7501

- The sequence is `ESC ] 7501 ; key=value:key=value ESC \`, and `msg` and
  `title` are base64 (the protocol is at
  https://www.superlogical.com/rex/docs/build/program-status.md). Checked
  from `sh`:
  `printf '\033]7501;state=blocked:kind=permission:app=claude-code:msg=YXBwcm92ZSBCYXNoOiBscw==\033\\'`.
  `program_status` and the event return `msg` decoded:
  `"approve Bash: ls"`.
- `id=task1:progress=40:title=…` makes a child record with an integer
  `progress`. A child with no `app` comes back without one but with the
  root's `effective_app`.
- When a program exits, a `working` root record goes with
  `program_status_removed` and reason `child_exited`. A `done` record stays.
- `state=clear:id=a` removed child `a/b`, and the event listed only `a/b`,
  since `a` had never been reported.
- Closing a block sends no `program_status_removed`. A watcher drops the
  block's records on `block_closed`.

## Events

- Plain `rex events --json` leaves out `block_event`, so it carries no
  status. `--all` adds block events and client events for every session,
  over one connection. `-s` follows one session.
- Each line is `{"type", "payload"}`. Session-scoped events come as
  `session_event` with `payload.session_id` and `payload.event`, and a
  block's own events as a further `block_event` with `block_id`,
  `creator_name`, `name` and `payload`.
- Seen at the top: `session_created` (`session.session_id`,
  `session.label`), `session_destroyed` (`session_id`), `client_joined`
  (`client`), `client_left` (`client_id`).
- Seen in `session_event`: `block_created` and `block_closed` (`block_id`),
  `block_owner_changed` (`block_id`, `owner` or `previous`, `reason`),
  `window_created` (`window_id`, `label`), `window_closed` (`window_id`),
  `window_label_changed` (`window_id`, `label`), `active_window_changed`
  (`window_id`), `session_label_changed` (`label`), `session_view_changed`
  (`revision`), and `client_connected` / `client_disconnected`
  (`client_id`, `principal`).
- Seen in `block_event`: `program_status_changed` (`id`, `""` for the root,
  `record`, `reason: "report"`), `program_status_removed` (`ids`,
  `reason`), `child_exited` (`exit_code`, `error`), `process_changed`,
  `title_changed` and `pwd_changed`.
- `session.focus_block` sends `active_window_changed` and then
  `session_view_changed`. rexuikit saw only the second on revision 7.
- Every CLI call shows up as a `client_joined` and a `client_left`, plus a
  `client_connected` and `client_disconnected` in each session it attaches
  to, and that includes the inbox's own calls.
- `block.close` and a program exiting send `block_closed`, then
  `window_closed` if it was the window's last block. `session.close_window`
  sends `window_closed` first, then `block_closed`.
- `rex kill` sends `block_closed` for each block, then
  `session_destroyed`, with no `window_closed`.
- Relabeling a block sends only `session_view_changed`.
- Not seen: what `rex events` does when the server stops. Its help says it
  runs until the server disconnects, and the client treats any exit it
  didn't cause as the stream ending.

## Block ownership

- A block made by `rex run` or `session.new_window` is owned by the CLI's
  connection (`block_owner_changed`, reason `create`). It is let go when
  that connection closes (reason `disconnect`, no new owner).
- A block made with its session, by `session.create` with
  `initial_windows` or by `rex new`, still names the CLI's client as owner
  after the CLI exits, though `client.list` no longer lists that client.
  `Client.Start` therefore makes an empty session and then a window in it.

## Blocks and processes

- `process` returns `child` and `foreground` while the program runs, each
  with a `cwd`. After it exits, in a `--keep-open` block, only `last_exit`
  (`exit_code`, `error`) and `home` are left.
- A block running the default shell has `login` (as root, with no `cwd`) as
  its `child` and the shell as `foreground`.
- `pwd` returns `{"pwd": "file://host/path"}` once the shell has sent OSC 7,
  and `{}` from a plain `sh`.

## Apps

- Rex.app attaches to every session, even one made with `rex new` that no
  window shows, so `client.list` lists those sessions in its `session_ids`.
  The app also has a second connection with no `info`.
- Remote Control was on for this server (`client.perform` of
  `client.update.status` succeeded), so the refusal wasn't seen again.
  `ErrRemoteControlOff` matches the text claude-inbox recorded on 0.1.0,
  `Rex.app is not accepting actions. Turn on Remote Control in its Settings
  under Rex Server.`; its code is assumed to be `refused`, which
  `client.perform` documents for a declining client.

## Which tab an app shows

Checked by switching the app between two throwaway sessions with
`client.perform` of its own `session.select`, `session.next`,
`session.previous` and `client.tab.next`, while following `rex events
--all --json`.

- No method says. `client.list`, `client.inspect` (whose `actions` list the
  app's actions, none of which report state) and `session.list_clients`
  give the sessions an app is attached to, which for Rex.app is all of
  them.
- A session's `active_window_id` is the server's, not the app's. After
  `client.tab.next` in the app, the app showed the other tab and
  `session.view` still named the first, and no `active_window_changed`
  came. Only server calls such as `session.focus_block` or `new_window`
  with `focus` move it.
- What does follow the app: each time it displays a block it sizes it,
  even at the same size, and that comes out as a `size_changed` block
  event whose payload names the app as `owner` (and its pid under
  `principal`). Every tab change and session change above sent one for
  the block of the tab now showing, and none for the tab left behind.
  Only one-block tabs were tried.
- The first time the app displays a block someone else owns, it takes it
  first (`block_owner_changed`, reason `take`). It keeps ownership after
  it moves on, so ownership says which blocks it has shown, not which it
  shows.
- `client.perform` sends a top-level `client_performed` event with the
  app's `client_id` and the `action`, but not its `args`, so it can't say
  which session was selected.

- A second window of the app (`client.window.new`) sizes its blocks under
  the same client id and pid as the first, and the payload carries only
  `columns`, `rows`, the cell size, `owner` and `principal`. Nothing says
  which of the app's windows sized a block. Checked on 2026-11-23.

The watcher therefore counts a tab as seen while it is the last one an app
sized a block in, and with no `size_changed` from an app yet, nothing is
seen. An app can have several windows of its own, each showing a tab; only
the last one it sized counts, which errs toward Needs you. Auto-settle
waits until every connected app has sized a block since the watcher's
last scan, at startup or after the stream reconnects, since until then a
tab on screen looks like one that isn't. That doesn't cover a tab in an
app's other window, which Rex gives no way to tell apart.
