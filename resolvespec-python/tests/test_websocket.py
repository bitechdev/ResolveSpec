import asyncio
import json

import pytest
from websockets.asyncio.server import serve

from resolvespec import ResolveSpecError, WebSocketClient


class Server:
    """Minimal in-process WebSocketSpec server."""

    def __init__(self):
        self.received = []
        self.conns = set()
        self.respond = True

    async def handler(self, ws):
        self.conns.add(ws)
        try:
            async for raw in ws:
                msg = json.loads(raw)
                self.received.append(msg)
                if msg["type"] == "ping":
                    await ws.send(json.dumps({"type": "pong"}))
                    continue
                if not self.respond:
                    continue
                await ws.send(json.dumps(self.reply(msg)))
        finally:
            self.conns.discard(ws)

    def reply(self, msg):
        base = {"id": msg["id"], "type": "response", "success": True, "timestamp": "t"}
        if msg["type"] == "subscription" and msg["operation"] == "subscribe":
            return {**base, "data": {"subscription_id": "sub-1"}}
        if msg.get("entity") == "fail":
            return {**base, "success": False, "error": {"code": "bad", "message": "boom"}}
        return {**base, "data": {"echo": msg.get("operation"), "record_id": msg.get("record_id")}}


@pytest.fixture
async def server():
    s = Server()
    async with serve(s.handler, "127.0.0.1", 0) as srv:
        s.url = "ws://127.0.0.1:%d" % srv.sockets[0].getsockname()[1]
        yield s


async def test_operations_and_message_shape(server):
    async with WebSocketClient(server.url, reconnect=False) as c:
        assert c.state == "connected"
        assert await c.read("users", schema="public", record_id="1", limit=5, filters=[{"column": "a", "operator": "eq", "value": 1}]) == {"echo": "read", "record_id": "1"}
        await c.create("users", {"n": 1}, schema="public")
        await c.update("users", "2", {"n": 2})
        await c.delete("users", "3")
        await c.meta("users")
    m = server.received
    assert m[0]["type"] == "request" and m[0]["operation"] == "read"
    assert m[0]["schema"] == "public" and m[0]["record_id"] == "1"
    assert m[0]["options"] == {"filters": [{"column": "a", "operator": "eq", "value": 1}], "limit": 5}
    assert m[1]["data"] == {"n": 1}
    assert m[2]["record_id"] == "2"
    assert [x["operation"] for x in m] == ["read", "create", "update", "delete", "meta"]
    assert "schema" not in m[2]
    assert len({x["id"] for x in m}) == 5


async def test_error_response_raises(server):
    async with WebSocketClient(server.url, reconnect=False) as c:
        with pytest.raises(ResolveSpecError, match="boom") as ei:
            await c.read("fail")
    assert ei.value.code == "bad"


async def test_request_timeout(server):
    server.respond = False
    async with WebSocketClient(server.url, reconnect=False, request_timeout=0.1) as c:
        with pytest.raises(ResolveSpecError, match="timeout"):
            await c.read("users")
        assert not c._pending


async def test_not_connected_raises():
    c = WebSocketClient("ws://127.0.0.1:1")
    with pytest.raises(ResolveSpecError, match="not connected"):
        await c.read("users")


async def test_subscribe_notify_unsubscribe(server):
    got = asyncio.Queue()
    async with WebSocketClient(server.url, reconnect=False) as c:
        sid = await c.subscribe("users", got.put, schema="public", filters=[{"column": "a", "operator": "eq", "value": 1}])
        assert sid == "sub-1"
        assert [s.id for s in c.get_subscriptions()] == ["sub-1"]
        assert server.received[0]["operation"] == "subscribe"
        assert server.received[0]["options"] == {"filters": [{"column": "a", "operator": "eq", "value": 1}]}
        for ws in server.conns:
            await ws.send(json.dumps({"type": "notification", "operation": "create", "subscription_id": "sub-1",
                                      "entity": "users", "data": {"id": 9}, "timestamp": "t"}))
        n = await asyncio.wait_for(got.get(), 2)
        assert n["data"] == {"id": 9}
        await c.unsubscribe("sub-1")
        assert c.get_subscriptions() == []
        assert server.received[-1] == {**server.received[-1], "operation": "unsubscribe", "subscription_id": "sub-1"}


async def test_events_and_heartbeat(server):
    events = []
    c = WebSocketClient(server.url, reconnect=False, heartbeat_interval=0.05)
    c.on("connect", lambda: events.append("connect"))
    c.on("state_change", lambda s: events.append(s))
    c.on("message", lambda m: events.append(("msg", m["type"])))
    await c.connect()
    await asyncio.sleep(0.2)
    await c.close()
    assert events[:3] == ["connecting", "connected", "connect"]
    assert ("msg", "pong") in events
    assert events[-1] == "disconnected"
    assert any(m["type"] == "ping" for m in server.received)
    with pytest.raises(ValueError):
        c.on("bogus", lambda: None)


async def test_reconnect_after_server_drop(server):
    states = []
    c = WebSocketClient(server.url, reconnect=True, reconnect_interval=0.05)
    c.on("state_change", states.append)
    await c.connect()
    for ws in list(server.conns):
        await ws.close()
    for _ in range(100):
        if states.count("connected") >= 2:
            break
        await asyncio.sleep(0.05)
    assert "reconnecting" in states
    assert c.is_connected()
    assert (await c.read("users"))["echo"] == "read"
    await c.close()


async def test_pending_requests_fail_on_disconnect(server):
    server.respond = False
    c = WebSocketClient(server.url, reconnect=False)
    await c.connect()
    task = asyncio.create_task(c.read("users"))
    await asyncio.sleep(0.05)
    for ws in list(server.conns):
        await ws.close()
    with pytest.raises(ResolveSpecError, match="disconnected"):
        await asyncio.wait_for(task, 2)
    await c.close()
