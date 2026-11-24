-- agent-inbox's actions for Rex. Paste this into init.lua (`rex config check`
-- says where it is), choose keys at the bottom, then run `rex config reload`.

-- A full path, because init.lua runs with the Rex server's PATH.
local agent_inbox = AGENT_INBOX
local terminal = "com.superlogical.terminal"
local shell = terminal .. ".shell"
local beside_label = "agent-inbox-beside"
local parked_width = 0.16
local modal_label = "agent-inbox-modal"

local function running(command, label)
  return rex.layout.block{ flavor = shell, label = label, options = { command = command } }
end

local function check(result, err)
  if err ~= nil then error(err, 0) end
  return result
end

-- The server's active window can differ from the tab the app shows, so the
-- window is the one holding the block the key was pressed in.
local function window_holding(view, block_id)
  for _, window in ipairs(view.windows) do
    for _, layer in ipairs(window.layers) do
      for _, block in ipairs(layer.blocks) do
        if block.block_id == block_id then return window end
      end
    end
  end
  for _, window in ipairs(view.windows) do
    if window.window_id == view.active_window_id then return window end
  end
end

local function this_window(ctx)
  local window = window_holding(check(rex.session.view{}), ctx.block_id)
  if window == nil then error("no tab to open the inbox in", 0) end
  return window
end

local function close_if_open(ctx, window, label)
  for _, layer in ipairs(window.layers) do
    for _, block in ipairs(layer.blocks) do
      if block.label == label then
        check(rex.call("block.close", { session_id = ctx.session_id, block_id = block.block_id }))
        return true
      end
    end
  end
  return false
end

-- Rex.app returns focus to the block behind a palette once the palette closes,
-- after this action has already focused the new block; init.lua has no timer.
local function hold_focus(ctx, block_id)
  if ctx.session_id == nil or ctx.block_id == nil or block_id == nil or block_id == ctx.block_id then return end
  rex.exec.start({ agent_inbox, "hold-focus", ctx.session_id, ctx.block_id, block_id }, { detach = true })
end

local function new_agent(ctx)
  local created = check(rex.session.new_layer{
    window_id = this_window(ctx).window_id,
    bounds = { 0.1, 0.1, 0.8, 0.8 },
    layout = running({ agent_inbox, "new" }),
    focus = true,
  })
  hold_focus(ctx, created.block_ids and created.block_ids[1])
end

local function layer_holding(window, label)
  for _, layer in ipairs(window.layers) do
    for _, block in ipairs(layer.blocks or {}) do
      if block.label == label then return layer, block end
    end
  end
end

local function bring_forward(ctx, layer)
  check(rex.call("session.set_layer_state", {
    session_id = ctx.session_id,
    layer_id = layer.layer_id,
    state = "normal",
    focus = true,
  }))
end

local function toggle_modal(ctx)
  local window = this_window(ctx)
  local layer, block = layer_holding(window, modal_label)
  local pressed_in_modal = block ~= nil and block.block_id == ctx.block_id and layer.state ~= "minimized"
  if pressed_in_modal then
    check(rex.call("block.close", { session_id = ctx.session_id, block_id = block.block_id }))
    return
  end
  local target
  if layer ~= nil then
    bring_forward(ctx, layer)
    target = block.block_id
  else
    local created = check(rex.session.new_layer{
      window_id = window.window_id,
      bounds = { 0.1, 0.1, 0.8, 0.8 },
      layout = running({ agent_inbox, "list", "--modal" }, modal_label),
      focus = true,
    })
    target = created.block_ids and created.block_ids[1]
  end
  hold_focus(ctx, target)
end

local function toggle_beside(ctx)
  local window = this_window(ctx)
  if close_if_open(ctx, window, beside_label) then return end
  local tiled
  for _, layer in ipairs(window.layers) do
    if layer.kind == "tiled" then tiled = layer end
  end
  local whole = tiled.layout.split and tiled.layout.split.split_id or tiled.layout.block_id
  local created = check(rex.session.new_split{
    anchor_id = whole,
    direction = "horizontal",
    side = "before",
    ratio = parked_width,
    layout = running({ agent_inbox }, beside_label),
    focus = true,
  })
  hold_focus(ctx, created.block_ids and created.block_ids[1])
end

local function said(output)
  return (output:gsub("^Error: ", ""):gsub("%s+$", ""))
end

local function on_focused_agent(ctx, verb, ...)
  if ctx.block_id == nil then error("no focused block", 0) end
  local r = check(rex.exec({ agent_inbox, verb, "--block", ctx.block_id, ... }, { timeout = 20 }))
  if not r.ok then error(said(r.stderr), 0) end
  return said(r.stdout)
end

-- rex.ui only answers inside the action that was run, not one it invokes,
-- so the menu and the snooze action both call this directly.
local function snooze(ctx, duration)
  duration = duration or rex.ui.pick{
    title = "Snooze for",
    items = {
      { id = "15m", title = "15 minutes" },
      { id = "1h", title = "1 hour" },
      { id = "tomorrow", title = "Until tomorrow at 9am" },
      { id = "until-woken", title = "Until I wake it" },
    },
  }
  if duration == nil then return end
  return on_focused_agent(ctx, "snooze", duration)
end

local function reports_status(block_id)
  if block_id == nil then return false end
  local status = rex.block.call(terminal, "program_status", { block_id = block_id })
  return status ~= nil and status.records ~= nil and #status.records > 0
end

rex.action{
  name = "agent_inbox_modal",
  title = "Agent Inbox: Toggle Over This Tab",
  run = function(ctx, args) toggle_modal(ctx) end,
}

rex.action{
  name = "agent_inbox_beside",
  title = "Agent Inbox: Toggle on the Left of This Tab",
  run = function(ctx, args) toggle_beside(ctx) end,
}

rex.action{
  name = "agent_inbox_new",
  title = "Agent Inbox: New Agent",
  run = function(ctx, args) new_agent(ctx) end,
}

rex.action{
  name = "agent_inbox_snooze",
  title = "Agent Inbox: Snooze This Agent",
  args = { duration = "string?" },
  run = function(ctx, args) return snooze(ctx, args.duration) end,
}

rex.action{
  name = "agent_inbox_pin",
  title = "Agent Inbox: Pin or Unpin This Agent",
  run = function(ctx, args) return on_focused_agent(ctx, "pin") end,
}

rex.action{
  name = "agent_inbox_settle",
  title = "Agent Inbox: Settle This Agent",
  run = function(ctx, args) return on_focused_agent(ctx, "settle") end,
}

-- The inbox comes first, so the key and then Enter summons it.
rex.action{
  name = "agent_inbox_menu",
  title = "Agent Inbox",
  run = function(ctx, args)
    local items = {
      { id = "modal", title = "Inbox", subtitle = "Over this tab; again from inside it to dismiss", icon = "star" },
      { id = "new", title = "New agent", icon = "plus" },
      { id = "beside", title = "Park the inbox on the left of this tab", subtitle = "Again to remove it", icon = "terminal" },
    }
    if reports_status(ctx.block_id) then
      items[#items + 1] = { id = "snooze", title = "Snooze this agent", icon = "clock" }
      items[#items + 1] = { id = "pin", title = "Pin or unpin this agent", icon = "tag" }
      items[#items + 1] = { id = "settle", title = "Settle this agent", subtitle = "Closes its tab", icon = "document" }
    end
    local choice = rex.ui.pick{ title = "Agent Inbox", placeholder = "Filter", items = items }
    if choice == "modal" then return toggle_modal(ctx) end
    if choice == "new" then return new_agent(ctx) end
    if choice == "beside" then return toggle_beside(ctx) end
    if choice == "snooze" then return snooze(ctx) end
    if choice == "pin" then return on_focused_agent(ctx, "pin") end
    if choice == "settle" then return on_focused_agent(ctx, "settle") end
  end,
}

-- The keys are yours to choose. One key for the command list:
-- rex.bind("cmd+i", "agent_inbox_menu")
-- Or a key each:
-- rex.bind("cmd+shift+i", "agent_inbox_modal")
-- rex.bind("cmd+shift+n", "agent_inbox_new")

-- The left arrow opens the inbox as a modal where Claude Code or Codex would
-- take it to their own agents view, and is a cursor key everywhere else.
-- Uncomment to use.
--
-- -- What each harness shows at the end of its screen while the left arrow
-- -- would open its agents view. The hint goes once the composer has text, and
-- -- while a permission prompt, menu or dialog is up.
-- local agents_view_hints = {
--   ["claude-code"] = "← %d+ agents?$",
--   codex = "← for agents",
-- }
--
-- local function left_opens_agents_view(block_id)
--   local status = rex.block.call(terminal, "program_status", { block_id = block_id })
--   local hint
--   for _, record in ipairs(status and status.records or {}) do
--     hint = hint or agents_view_hints[record.app]
--   end
--   if hint == nil then return false end
--   local screen = rex.block.call(terminal, "format", { block_id = block_id, trim = true })
--   local last = ""
--   for line in (screen and screen.content or ""):gmatch("[^\n]+") do last = line end
--   return last:find(hint) ~= nil
-- end
--
-- -- A key function that raises swallows the key, so a failed call above
-- -- reads as "not an agent" and the key goes on to the terminal.
-- rex.bind("left", function(ctx, ev)
--   if ctx.block_id == nil or not left_opens_agents_view(ctx.block_id) then return false end
--   toggle_modal(ctx)
--   return true
-- end)
