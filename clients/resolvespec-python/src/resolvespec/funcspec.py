"""FunctionSpec client: calls user-defined SQL endpoints (Go pkg/funcspec).

Routes are defined by the server application, so calls take a `path`.
Parameters are sent as query string values and/or `X-*` headers; the server never
reads a request body. Query-string values override headers of the same name.

Server behaviour worth knowing (pkg/funcspec):
  - `sort` is inserted raw into ORDER BY, so it must be SQL (`col DESC`), not `-col`.
  - Field selection (`X-Select-Fields`) is a no-op server-side, so it is not exposed.
  - Only one search operator per column is kept.
  - Values starting with `ZIP_` or `__` are base64-decoded by the server (even after our
    own encoding), so such plaintext values cannot be sent faithfully.
"""
from __future__ import annotations

import re
from typing import Any, Dict, List, Mapping, Optional

import httpx

from .headerspec import _OPERATOR_MAP, _bool, _filter_value, encode_header_value
from .http import client_headers, error_from, merge_headers, parse_json
from .types import APIResponse, FuncSpecOptions

Params = Mapping[str, Any]

_CONTENT_RANGE = re.compile(r"(\d+)-(\d+)/(\d+)")


def _safe(value: str) -> str:
    """Encode values that are unsafe as raw header/query text (non-ASCII, control chars, edge spaces)."""
    if not value.isascii() or not value.isprintable() or value != value.strip():
        return encode_header_value(value)
    return value


def build_headers(options: Mapping[str, Any]) -> Dict[str, str]:
    """Build the X-* headers understood by funcspec.ParseParameters."""
    h: Dict[str, str] = {}
    o = options

    for f in o.get("filters") or []:
        operator = f["operator"]
        logic = f.get("logic_operator") or "AND"
        value = _safe(_filter_value(f))
        if operator == "eq" and logic == "AND":
            h[f"X-FieldFilter-{f['column']}"] = value
        else:
            kind = "X-SearchOr" if logic == "OR" else "X-SearchOp"
            h[f"{kind}-{_OPERATOR_MAP.get(operator, operator)}-{f['column']}"] = value

    for col, text in (o.get("search_filters") or {}).items():
        h[f"X-SearchFilter-{col}"] = _safe(str(text))  # CAST(col AS TEXT) ILIKE %text%

    if o.get("custom_sql_where"):
        h["X-Custom-SQL-W"] = _safe(o["custom_sql_where"])
    if o.get("custom_sql_or"):
        h["X-Custom-SQL-Or"] = _safe(o["custom_sql_or"])

    if o.get("sort"):
        h["X-Sort"] = _safe(",".join(_sort_term(s) for s in o["sort"]))
    if o.get("limit") is not None:
        h["X-Limit"] = str(o["limit"])
    if o.get("offset") is not None:
        h["X-Offset"] = str(o["offset"])

    for name, key in (("X-Distinct", "distinct"), ("X-SkipCount", "skip_count"), ("X-SkipCache", "skip_cache")):
        if o.get(key) is not None:
            h[name] = _bool(o[key])

    fmt = o.get("response_format")
    if fmt:
        h[{"simple": "X-SimpleApi", "detail": "X-DetailApi", "syncfusion": "X-Syncfusion"}[fmt]] = "true"
    return h


def _sort_term(s: Mapping[str, str]) -> str:
    # funcspec puts this verbatim into ORDER BY
    return f"{s['column']} {'DESC' if s.get('direction', 'asc').upper() == 'DESC' else 'ASC'}"


def build_query(params: Optional[Params]) -> Dict[str, Any]:
    """Query-string values: bools -> true/false, lists -> repeated keys (server: IN filter)."""
    out: Dict[str, Any] = {}
    for k, v in (params or {}).items():
        if v is None:
            continue
        if isinstance(v, (list, tuple)):
            out[k] = [_safe(_q(x)) for x in v]
        else:
            out[k] = _safe(_q(v))
    return out


def _q(v: Any) -> str:
    return _bool(v) if isinstance(v, bool) else str(v)


def _metadata(response: httpx.Response, options: Optional[Mapping[str, Any]]) -> Dict[str, int]:
    """Content-Range is `items {offset}-{offset+len}/{total}`."""
    m = _CONTENT_RANGE.search(response.headers.get("content-range", ""))
    start, end, total = (int(x) for x in m.groups()) if m else (0, 0, 0)
    return {
        "total": total,
        "count": end - start,
        "filtered": total,
        "offset": start,
        "limit": int((options or {}).get("limit") or 0),
    }


def _wrap(response: httpx.Response, options: Optional[Mapping[str, Any]], with_metadata: bool) -> APIResponse:
    data = parse_json(response)
    if not response.is_success:  # 206 Partial Content is success
        raise error_from(response, data)
    result: APIResponse = {"success": True, "data": data}
    if with_metadata:
        result["metadata"] = _metadata(response, options)
    return result


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

    def _req(self, method: str, path: str, params: Optional[Params], options: Optional[Mapping[str, Any]]):
        url = f"{self.base_url.rstrip('/')}/{path.lstrip('/')}"
        headers = merge_headers(
            client_headers(self.token, self.headers),
            build_headers(options) if options else {},
        )
        return method.upper(), url, headers, build_query(params)


class FuncSpecClient(_Base):
    """Synchronous client. Use as a context manager or call close()."""

    def __init__(self, *args: Any, transport: Optional[httpx.BaseTransport] = None, **kwargs: Any):
        super().__init__(*args, **kwargs)
        self._http = httpx.Client(timeout=self.timeout, transport=transport)

    def close(self) -> None:
        self._http.close()

    def __enter__(self) -> "FuncSpecClient":
        return self

    def __exit__(self, *exc: Any) -> None:
        self.close()

    def _send(self, req, options, with_metadata) -> APIResponse:
        method, url, headers, query = req
        return _wrap(self._http.request(method, url, headers=headers, params=query), options, with_metadata)

    def query(self, path: str, params: Optional[Params] = None, options: Optional[FuncSpecOptions] = None, *, method: str = "GET") -> APIResponse:
        """Single-record endpoint (Handler.SqlQuery). `data` is the row object."""
        return self._send(self._req(method, path, params, options), options, False)

    def query_list(self, path: str, params: Optional[Params] = None, options: Optional[FuncSpecOptions] = None, *, method: str = "GET") -> APIResponse:
        """List endpoint (Handler.SqlQueryList). Adds `metadata` from Content-Range."""
        return self._send(self._req(method, path, params, options), options, True)


class AsyncFuncSpecClient(_Base):
    """Asyncio client. Use as an async context manager or await aclose()."""

    def __init__(self, *args: Any, transport: Optional[httpx.AsyncBaseTransport] = None, **kwargs: Any):
        super().__init__(*args, **kwargs)
        self._http = httpx.AsyncClient(timeout=self.timeout, transport=transport)

    async def aclose(self) -> None:
        await self._http.aclose()

    async def __aenter__(self) -> "AsyncFuncSpecClient":
        return self

    async def __aexit__(self, *exc: Any) -> None:
        await self.aclose()

    async def _send(self, req, options, with_metadata) -> APIResponse:
        method, url, headers, query = req
        return _wrap(await self._http.request(method, url, headers=headers, params=query), options, with_metadata)

    async def query(self, path: str, params: Optional[Params] = None, options: Optional[FuncSpecOptions] = None, *, method: str = "GET") -> APIResponse:
        return await self._send(self._req(method, path, params, options), options, False)

    async def query_list(self, path: str, params: Optional[Params] = None, options: Optional[FuncSpecOptions] = None, *, method: str = "GET") -> APIResponse:
        return await self._send(self._req(method, path, params, options), options, True)
