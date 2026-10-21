-- test: mash through the intro into the game, then start a wild battle (ZIGZAGOON Lv3) with a RAM script
local TT = { f = 0, ow = 0, done = false }
local FB = TEST_FALLBACK or {}
callbacks:add("frame", function()
  TT.f = TT.f + 1
  if TT.done or not V then return end
  local ctx, status, lock = V.scriptCtx or FB.ctx, V.scriptStatus or FB.status, V.lockControls or FB.lock
  local cmd, cmdEnd, cb2ow = V.cmdTable or FB.cmd, V.cmdTableEnd or FB.cmdEnd, V.cb2Overworld or FB.cb2ow
  if emu:read32(V.main + 4) | 1 == cb2ow | 1 then TT.ow = TT.ow + 1 else TT.ow = 0 end
  if TT.ow < 200 then
    local k = 0
    if TT.f % 16 < 4 then k = 1 end
    if TT.f % 400 > 390 then k = 8 end
    emu:clearKeys(0x3FF); if k ~= 0 then emu:addKeys(k) end
    return
  end
  emu:clearKeys(0x3FF)
  local SCRIPT = 0x0203FC00
  for i, b in ipairs({ 0xB6, 0x20, 0x01, 3, 0, 0, 0xB7, 0x02 }) do emu:write8(SCRIPT + i - 1, b) end
  for i = 0, 0x73 do emu:write8(ctx + i, 0) end
  emu:write32(ctx + 0x5C, cmd); emu:write32(ctx + 0x60, cmdEnd)
  emu:write32(ctx + 8, SCRIPT); emu:write8(ctx + 1, 1)
  emu:write8(lock, 1); emu:write8(status, 0)
  console:log("TEST: battle script started at frame " .. TT.f)
  TT.done = true
end)
