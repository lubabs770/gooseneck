-- Backport of mpv PR #18193 (unreleased as of mpv 0.41): YouTube rejects a
-- stream URL (403) until the time yt-dlp reports as `available_at`, so wait
-- for it before mpv opens the stream. Runs after ytdl_hook's on_load (10) and
-- before its playlist hook (20). Harmless once mpv waits itself: by then the
-- time has passed.
local utils = require 'mp.utils'

mp.add_hook("on_load", 15, function(hook)
    local raw = mp.get_property("user-data/mpv/ytdl/json-subprocess-result")
    if not raw then return end
    local res = utils.parse_json(raw)
    local json = res and res.stdout and utils.parse_json(res.stdout)
    -- the property outlives its track: only trust it for the file loading now
    if not json or not json.id or not mp.get_property("path", ""):find(json.id, 1, true) then return end
    local at = 0
    for _, f in ipairs(json.requested_formats or {json}) do
        if type(f.available_at) == "number" then at = math.max(at, f.available_at) end
    end
    local wait = at - os.time()
    if wait > 0 then
        mp.msg.info(("waiting %ds for YouTube to release the stream"):format(wait))
        hook:defer()
        mp.add_timeout(wait + 0.5, function() hook:cont() end)
    end
end)
