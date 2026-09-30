"""ResolveSpec client: JSON body protocol (POST {operation, data, options})."""
from __future__ import annotations

from typing import Any, Dict, List, Mapping, Optional, Tuple

import httpx

from .http import build_url, client_headers, drop_none, error_from, parse_json
from .types import APIResponse, Options, RecordId


def _url_id(id: Optional[RecordId]) -> Optional[str]:
    return str(id) if isinstance(id, (int, str)) else None


def _body_id(id: Optional[RecordId]) -> Optional[List[str]]:
    return id if isinstance(id, list) else None


class _Base:
    def __init__(
        self,
        base_url: str,
        token: Optional[str] = None,
        headers: Optional[Mapping[str, str]] = None,
        timeout: Optional[float] = 30.0,
    ):
        self.base_url = base_url
        self.token = token
        self.headers = dict(headers or {})  # snapshot
        self.timeout = timeout

    def _headers(self) -> Dict[str, str]:
        return client_headers(self.token, self.headers)

    def _request(
        self, method: str, schema: str, entity: str, id: Optional[str], body: Optional[Dict[str, Any]]
    ) -> Tuple[str, str, Dict[str, str], Optional[Dict[str, Any]]]:
        return method, build_url(self.base_url, schema, entity, id), self._headers(), body

    @staticmethod
    def _result(response: httpx.Response) -> APIResponse:
        data = parse_json(response)
        if not response.is_success:
            raise error_from(response, data)
        return data

    # request builders (shared by sync and async)
    def _metadata_req(self, schema, entity):
        return self._request("GET", schema, entity, None, None)

    def _read_req(self, schema, entity, id, options):
        body = drop_none({"operation": "read", "id": _body_id(id), "options": options})
        return self._request("POST", schema, entity, _url_id(id), body)

    def _create_req(self, schema, entity, data, options):
        body = drop_none({"operation": "create", "data": data, "options": options})
        return self._request("POST", schema, entity, None, body)

    def _update_req(self, schema, entity, data, id, options):
        body = drop_none({"operation": "update", "id": _body_id(id), "data": data, "options": options})
        return self._request("POST", schema, entity, _url_id(id), body)

    def _delete_req(self, schema, entity, id):
        return self._request("POST", schema, entity, str(id), {"operation": "delete"})


class ResolveSpecClient(_Base):
    """Synchronous client. Use as a context manager or call close()."""

    def __init__(self, *args: Any, transport: Optional[httpx.BaseTransport] = None, **kwargs: Any):
        super().__init__(*args, **kwargs)
        self._http = httpx.Client(timeout=self.timeout, transport=transport)

    def close(self) -> None:
        self._http.close()

    def __enter__(self) -> "ResolveSpecClient":
        return self

    def __exit__(self, *exc: Any) -> None:
        self.close()

    def _send(self, req) -> APIResponse:
        method, url, headers, body = req
        return self._result(self._http.request(method, url, headers=headers, json=body))

    def get_metadata(self, schema: str, entity: str) -> APIResponse:
        return self._send(self._metadata_req(schema, entity))

    def read(self, schema: str, entity: str, id: Optional[RecordId] = None, options: Optional[Options] = None) -> APIResponse:
        return self._send(self._read_req(schema, entity, id, options))

    def create(self, schema: str, entity: str, data: Any, options: Optional[Options] = None) -> APIResponse:
        return self._send(self._create_req(schema, entity, data, options))

    def update(self, schema: str, entity: str, data: Any, id: Optional[RecordId] = None, options: Optional[Options] = None) -> APIResponse:
        return self._send(self._update_req(schema, entity, data, id, options))

    def delete(self, schema: str, entity: str, id: Any) -> APIResponse:
        return self._send(self._delete_req(schema, entity, id))


class AsyncResolveSpecClient(_Base):
    """Asyncio client. Use as an async context manager or await aclose()."""

    def __init__(self, *args: Any, transport: Optional[httpx.AsyncBaseTransport] = None, **kwargs: Any):
        super().__init__(*args, **kwargs)
        self._http = httpx.AsyncClient(timeout=self.timeout, transport=transport)

    async def aclose(self) -> None:
        await self._http.aclose()

    async def __aenter__(self) -> "AsyncResolveSpecClient":
        return self

    async def __aexit__(self, *exc: Any) -> None:
        await self.aclose()

    async def _send(self, req) -> APIResponse:
        method, url, headers, body = req
        return self._result(await self._http.request(method, url, headers=headers, json=body))

    async def get_metadata(self, schema: str, entity: str) -> APIResponse:
        return await self._send(self._metadata_req(schema, entity))

    async def read(self, schema: str, entity: str, id: Optional[RecordId] = None, options: Optional[Options] = None) -> APIResponse:
        return await self._send(self._read_req(schema, entity, id, options))

    async def create(self, schema: str, entity: str, data: Any, options: Optional[Options] = None) -> APIResponse:
        return await self._send(self._create_req(schema, entity, data, options))

    async def update(self, schema: str, entity: str, data: Any, id: Optional[RecordId] = None, options: Optional[Options] = None) -> APIResponse:
        return await self._send(self._update_req(schema, entity, data, id, options))

    async def delete(self, schema: str, entity: str, id: Any) -> APIResponse:
        return await self._send(self._delete_req(schema, entity, id))
