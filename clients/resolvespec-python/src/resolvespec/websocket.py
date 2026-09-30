"""WebSocketSpec client (asyncio). Mirrors the Go websocketspec message protocol."""
from __future__ import annotations

import asyncio
import json
import logging
import uuid
from dataclasses import dataclass, field
from typing import Any, Awaitable, Callable, Dict, List, Optional, Union

from websockets.asyncio.client import ClientConnection, connect

from .http import ResolveSpecError
from .types import FilterOption, PreloadOption, SortOption

log = logging.getLogger("resolvespec.websocket")

# Connection states
DISCONNECTED = "disconnected"
CONNECTING = "connecting"
CONNECTED = "connected"
DISCONNECTING = "disconnecting"
RECONNECTING = "reconnecting"

Notification = Dict[str, Any]
Callback = Callable[[Any], Union[None, Awaitable[None]]]
EVENTS = ("connect", "disconnect", "error", "message", "state_change")


@dataclass
class Subscription:
    id: str
    entity: str
    schema: Optional[str] = None
    options: Optional[Dict[str, Any]] = None
    callback: Optional[Callback] = field(default=None, repr=False)


def _drop_none(d: Dict[str, Any]) -> Dict[str, Any]:
    return {k: v for k, v in d.items() if v is not None}


class WebSocketClient:
    """
    Usage:
        async with WebSocketClient("ws://localhost:8080/ws") as ws:
            rows = await ws.read("users", schema="public", limit=10)

    Events (`on(event, callback)`): connect, disconnect, error, message, state_change.
    Callbacks may be sync or async.
    """

    def __init__(
        self,
        url: str,
        *,
        reconnect: bool = True,
        reconnect_interval: float = 3.0,
        max_reconnect_attempts: int = 10,
        heartbeat_interval: float = 30.0,
        request_timeout: float = 30.0,
        subscribe_timeout: float = 10.0,
        headers: Optional[Dict[str, str]] = None,
    ):
        self.url = url
        self.reconnect = reconnect
        self.reconnect_interval = reconnect_interval
        self.max_reconnect_attempts = max_reconnect_attempts
        self.heartbeat_interval = heartbeat_interval
        self.request_timeout = request_timeout
        self.subscribe_timeout = subscribe_timeout
        self.headers = dict(headers or {})

        self._ws: Optional[ClientConnection] = None
        self._state = DISCONNECTED
        self._pending: Dict[str, "asyncio.Future[Dict[str, Any]]"] = {}
        self._subscriptions: Dict[str, Subscription] = {}
        self._listeners: Dict[str, Callback] = {}
        self._tasks: List["asyncio.Task[Any]"] = []
        self._reader: Optional["asyncio.Task[Any]"] = None
        self._manual_close = False

    # ---- lifecycle -------------------------------------------------------

    async def __aenter__(self) -> "WebSocketClient":
        await self.connect()
        return self

    async def __aexit__(self, *exc: Any) -> None:
        await self.close()

    async def connect(self) -> None:
        if self.is_connected():
            return
        self._manual_close = False
        self._set_state(CONNECTING)
        try:
            self._ws = await connect(self.url, additional_headers=self.headers or None)
        except Exception as e:
            self._set_state(DISCONNECTED)
            await self._emit("error", e)
            raise
        self._set_state(CONNECTED)
        self._reader = asyncio.create_task(self._read_loop(self._ws))
        self._heartbeat = asyncio.create_task(self._heartbeat_loop())
        await self._emit("connect")

    async def close(self) -> None:
        self._manual_close = True
        self._set_state(DISCONNECTING)
        for t in (self._reader, getattr(self, "_heartbeat", None), getattr(self, "_reconnect_task", None)):
            if t and t is not asyncio.current_task():
                t.cancel()
        if self._ws:
            await self._ws.close()
            self._ws = None
        self._fail_pending(ResolveSpecError("WebSocket closed"))
        self._set_state(DISCONNECTED)

    def is_connected(self) -> bool:
        return self._ws is not None and self._state == CONNECTED

    @property
    def state(self) -> str:
        return self._state

    def on(self, event: str, callback: Callback) -> None:
        if event not in EVENTS:
            raise ValueError(f"unknown event {event!r}; expected one of {EVENTS}")
        self._listeners[event] = callback

    def off(self, event: str) -> None:
        self._listeners.pop(event, None)

    def get_subscriptions(self) -> List[Subscription]:
        return list(self._subscriptions.values())

    # ---- operations ------------------------------------------------------

    async def request(
        self,
        operation: str,
        entity: str,
        *,
        schema: Optional[str] = None,
        record_id: Optional[str] = None,
        data: Any = None,
        options: Optional[Dict[str, Any]] = None,
    ) -> Any:
        message = _drop_none({
            "type": "request",
            "operation": operation,
            "entity": entity,
            "schema": schema,
            "record_id": record_id,
            "data": data,
            "options": options,
        })
        response = await self._call(message, self.request_timeout, "Request")
        return response.get("data")

    async def read(
        self,
        entity: str,
        *,
        schema: Optional[str] = None,
        record_id: Optional[str] = None,
        filters: Optional[List[FilterOption]] = None,
        columns: Optional[List[str]] = None,
        sort: Optional[List[SortOption]] = None,
        preload: Optional[List[PreloadOption]] = None,
        limit: Optional[int] = None,
        offset: Optional[int] = None,
    ) -> Any:
        options = _drop_none({
            "filters": filters, "columns": columns, "sort": sort,
            "preload": preload, "limit": limit, "offset": offset,
        })
        return await self.request("read", entity, schema=schema, record_id=record_id, options=options)

    async def create(self, entity: str, data: Any, *, schema: Optional[str] = None) -> Any:
        return await self.request("create", entity, schema=schema, data=data)

    async def update(self, entity: str, id: str, data: Any, *, schema: Optional[str] = None) -> Any:
        return await self.request("update", entity, schema=schema, record_id=id, data=data)

    async def delete(self, entity: str, id: str, *, schema: Optional[str] = None) -> None:
        await self.request("delete", entity, schema=schema, record_id=id)

    async def meta(self, entity: str, *, schema: Optional[str] = None) -> Any:
        return await self.request("meta", entity, schema=schema)

    async def subscribe(
        self,
        entity: str,
        callback: Callback,
        *,
        schema: Optional[str] = None,
        filters: Optional[List[FilterOption]] = None,
    ) -> str:
        message = _drop_none({
            "type": "subscription",
            "operation": "subscribe",
            "entity": entity,
            "schema": schema,
            "options": _drop_none({"filters": filters}),
        })
        response = await self._call(message, self.subscribe_timeout, "Subscription")
        sub_id = (response.get("data") or {}).get("subscription_id")
        if not sub_id:
            raise ResolveSpecError("Subscription failed")
        self._subscriptions[sub_id] = Subscription(
            sub_id, entity, schema, _drop_none({"filters": filters}) or None, callback
        )
        return sub_id

    async def unsubscribe(self, subscription_id: str) -> None:
        message = {"type": "subscription", "operation": "unsubscribe", "subscription_id": subscription_id}
        await self._call(message, self.subscribe_timeout, "Unsubscribe")
        self._subscriptions.pop(subscription_id, None)

    # ---- internals -------------------------------------------------------

    async def _call(self, message: Dict[str, Any], timeout: float, what: str) -> Dict[str, Any]:
        self._ensure_connected()
        mid = str(uuid.uuid4())
        message["id"] = mid
        fut: "asyncio.Future[Dict[str, Any]]" = asyncio.get_running_loop().create_future()
        self._pending[mid] = fut
        try:
            await self._ws.send(json.dumps(message))  # type: ignore[union-attr]
            response = await asyncio.wait_for(fut, timeout)
        except asyncio.TimeoutError:
            raise ResolveSpecError(f"{what} timeout") from None
        finally:
            self._pending.pop(mid, None)
        if not response.get("success"):
            err = response.get("error") or {}
            raise ResolveSpecError(
                err.get("message") or f"{what} failed", code=err.get("code"), details=err.get("details")
            )
        return response

    def _ensure_connected(self) -> None:
        if not self.is_connected():
            raise ResolveSpecError("WebSocket is not connected. Call connect() first.")

    def _fail_pending(self, exc: Exception) -> None:
        for fut in self._pending.values():
            if not fut.done():
                fut.set_exception(exc)
        self._pending.clear()

    async def _read_loop(self, ws: ClientConnection) -> None:
        try:
            async for raw in ws:
                await self._handle_message(raw)
        except asyncio.CancelledError:
            raise
        except Exception as e:  # connection error
            await self._emit("error", e)
        # connection ended
        if ws is not self._ws:
            return
        self._ws = None
        if hb := getattr(self, "_heartbeat", None):
            hb.cancel()
        self._fail_pending(ResolveSpecError("WebSocket disconnected"))
        self._set_state(DISCONNECTED)
        await self._emit("disconnect", ws.close_code, ws.close_reason)
        if self.reconnect and not self._manual_close:
            self._reconnect_task = asyncio.create_task(self._reconnect())

    async def _reconnect(self) -> None:
        for attempt in range(1, self.max_reconnect_attempts + 1):
            if self._manual_close:
                return
            log.debug("Reconnection attempt %d/%d", attempt, self.max_reconnect_attempts)
            self._set_state(RECONNECTING)
            await asyncio.sleep(self.reconnect_interval)
            try:
                await self.connect()
                return
            except Exception as e:
                log.debug("Reconnection failed: %s", e)
        self._set_state(DISCONNECTED)

    async def _handle_message(self, raw: Union[str, bytes]) -> None:
        try:
            message = json.loads(raw)
        except ValueError as e:
            log.debug("Error parsing message: %s", e)
            return
        await self._emit("message", message)
        kind = message.get("type")
        if kind == "response":
            fut = self._pending.get(message.get("id"))
            if fut and not fut.done():
                fut.set_result(message)
        elif kind == "notification":
            sub = self._subscriptions.get(message.get("subscription_id"))
            if sub and sub.callback:
                await _maybe_await(sub.callback(message))
        elif kind != "pong":
            log.debug("Unknown message type: %s", kind)

    async def _heartbeat_loop(self) -> None:
        try:
            while True:
                await asyncio.sleep(self.heartbeat_interval)
                if self.is_connected():
                    await self._ws.send(json.dumps({"id": str(uuid.uuid4()), "type": "ping"}))  # type: ignore[union-attr]
        except asyncio.CancelledError:
            raise
        except Exception as e:
            log.debug("Heartbeat failed: %s", e)

    def _set_state(self, state: str) -> None:
        if self._state != state:
            self._state = state
            cb = self._listeners.get("state_change")
            if cb:
                res = cb(state)
                if asyncio.iscoroutine(res):
                    asyncio.ensure_future(res)

    async def _emit(self, event: str, *args: Any) -> None:
        cb = self._listeners.get(event)
        if cb:
            await _maybe_await(cb(*args))


async def _maybe_await(result: Any) -> None:
    if asyncio.iscoroutine(result) or isinstance(result, asyncio.Future):
        await result
