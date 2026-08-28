-- Bunch is the social application. Durable social-graph state lives in the
-- generic social owner; the WebSocket connection itself remains at the
-- transport edge because its connection handle is host-owned.
local SOCIAL = "social-graph"

local function require_wire(payload)
  if type(payload) ~= "table" then error("bunch: payload must be a table") end
  local raw = payload.request_msgpack
  if type(raw) ~= "string" or raw == "" then
    error("bunch: request_msgpack must contain MessagePack bytes")
  end
  return raw
end

local function forward(provider)
  return function(payload)
    return {
      response_msgpack = pulp.call_raw(SOCIAL, provider, require_wire(payload)),
    }
  end
end

pulp.on("bunch.friend.request.v1", forward("social.v1.friend.request"))
pulp.on("bunch.friend.accept.v1", forward("social.v1.friend.accept"))
pulp.on("bunch.friend.decline.v1", forward("social.v1.friend.decline"))
pulp.on("bunch.friend.remove.v1", forward("social.v1.friend.remove"))
pulp.on("bunch.friend.list.v1", forward("social.v1.friend.list"))
pulp.on("bunch.request.list.v1", forward("social.v1.request.list"))
pulp.on("bunch.friend.ids.v1", forward("social.v1.friend.ids"))
pulp.on("bunch.block.v1", forward("social.v1.block"))
pulp.on("bunch.unblock.v1", forward("social.v1.unblock"))
pulp.on("bunch.block.list.v1", forward("social.v1.block.list"))
