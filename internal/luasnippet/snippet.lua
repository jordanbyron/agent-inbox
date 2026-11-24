-- agent-inbox's actions for Rex. Paste this into init.lua (`rex config check`
-- says where it is), choose keys at the bottom, then run `rex config reload`.

-- A full path, because init.lua runs with the Rex server's PATH.
local agent_inbox = AGENT_INBOX
local shell = "com.superlogical.terminal.shell"
local beside_label = "agent-inbox-beside"

local function running(command, label)
  return rex.layout.block{ flavor = shell, label = label, options = { command = command } }
end

local function check(result, err)
  if err ~= nil then error(err, 0) end
  return result
end

rex.action{
  name = "agent_inbox_new",
  title = "Agent Inbox: New Agent",
  run = function(ctx, args)
    check(rex.session.new_layer{
      bounds = { 0.1, 0.1, 0.8, 0.8 },
      layout = running({ agent_inbox, "new" }),
      focus = true,
    })
  end,
}

rex.action{
  name = "agent_inbox_tab",
  title = "Agent Inbox: Open in a Tab",
  run = function(ctx, args)
    check(rex.session.new_window{ layout = running({ agent_inbox }), focus = true })
  end,
}

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

rex.action{
  name = "agent_inbox_beside",
  title = "Agent Inbox: Toggle Beside This Tab",
  run = function(ctx, args)
    local window = window_holding(check(rex.session.view{}), ctx.block_id)
    if window == nil then error("no tab to open the inbox beside", 0) end
    local tiled
    for _, layer in ipairs(window.layers) do
      if layer.kind == "tiled" then tiled = layer end
      for _, block in ipairs(layer.blocks) do
        if block.label == beside_label then
          check(rex.call("block.close", { session_id = ctx.session_id, block_id = block.block_id }))
          return
        end
      end
    end
    local whole = tiled.layout.split and tiled.layout.split.split_id or tiled.layout.block_id
    check(rex.session.new_split{
      anchor_id = whole,
      direction = "horizontal",
      side = "after",
      ratio = 0.7,
      layout = running({ agent_inbox }, beside_label),
      focus = true,
    })
  end,
}

local function open_modal(ctx)
  local window = window_holding(check(rex.session.view{}), ctx.block_id)
  if window == nil then error("no tab to open the inbox over", 0) end
  check(rex.session.new_layer{
    window_id = window.window_id,
    bounds = { 0.1, 0.1, 0.8, 0.8 },
    layout = running({ agent_inbox, "list", "--modal" }),
    focus = true,
  })
end

rex.action{
  name = "agent_inbox_modal",
  title = "Agent Inbox: Open as a Modal",
  run = function(ctx, args) open_modal(ctx) end,
}

local function said(output)
  return (output:gsub("^Error: ", ""):gsub("%s+$", ""))
end

local function on_focused_agent(ctx, verb, ...)
  if ctx.block_id == nil then error("no focused block", 0) end
  local r = check(rex.exec({ agent_inbox, verb, "--block", ctx.block_id, ... }, { timeout = 20 }))
  if not r.ok then error(said(r.stderr), 0) end
  return said(r.stdout)
end

rex.action{
  name = "agent_inbox_snooze",
  title = "Agent Inbox: Snooze This Agent",
  args = { duration = "string?" },
  run = function(ctx, args) return on_focused_agent(ctx, "snooze", args.duration or "1h") end,
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

-- The keys are yours to choose. For example, after Cmd+I:
-- rex.bind("cmd+i>n", "agent_inbox_new")
-- rex.bind("cmd+i>i", "agent_inbox_tab")
-- rex.bind("cmd+i>m", "agent_inbox_modal")
-- rex.bind("cmd+i>b", "agent_inbox_beside")
-- rex.bind("cmd+i>s", "agent_inbox_snooze", { duration = "1h" })
-- rex.bind("cmd+i>t", "agent_inbox_pin")
-- rex.bind("cmd+i>x", "agent_inbox_settle")

-- The left arrow opens the inbox as a modal where Claude Code or Codex would
-- take it to their own agents view, and is a cursor key everywhere else.
-- Uncomment to use.
--
-- local terminal = "com.superlogical.terminal"
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
--   open_modal(ctx)
--   return true
-- end)
