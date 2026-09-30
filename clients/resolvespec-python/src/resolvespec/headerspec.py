"""HeaderSpec client: query options sent as HTTP headers (Go restheadspec).

Methods: GET=read, POST=create, PUT=update, DELETE=delete.
"""
from __future__ import annotations

import base64
import json
import re
from typing import Any, Dict, Mapping, Optional

import httpx

from .http import build_url, client_headers, error_from, merge_headers, parse_json
from .types import APIResponse, FilterOption, HeaderSpecOptions

_PREFIXES = ("ZIP_", "__")

_OPERATOR_MAP = {
    "eq": "equals",
    "neq": "notequals",
    "gt": "greaterthan",
    "gte": "greaterthanorequal",
    "lt": "lessthan",
    "lte": "lessthanorequal",
    "like": "contains",
    "ilike": "contains",
    "contains": "contains",
    "startswith": "beginswith",
    "endswith": "endswith",
    "in": "in",
    "between": "between",
    "between_inclusive": "betweeninclusive",
    "is_null": "empty",
    "is_not_null": "notempty",
}


def encode_header_value(value: str) -> str:
    """Base64 (UTF-8) with ZIP_ prefix, for complex header values."""
    return "ZIP_" + base64.b64encode(value.encode("utf-8")).decode("ascii")


def decode_header_value(value: str) -> str:
    """Decode a value that may carry a ZIP_ or __ base64 prefix (nested allowed)."""
    code = value
    for prefix in _PREFIXES:
        if code.startswith(prefix):
            b64 = re.sub(r"[\n\r ]", "", code[len(prefix):])
            b64 += "=" * (-len(b64) % 4)
            code = base64.b64decode(b64).decode("utf-8")
            break
    if code.startswith(_PREFIXES):
        code = decode_header_value(code)
    return code


def _geo_header(operator: str) -> Optional[str]:
    op = operator.lower()
    if op.endswith("_within"):
        return "X-VectorFilter-"
    if op.startswith("st_") or op in ("bbox", "&&"):
        return "X-SpatialFilter-"
    return None


def _filter_value(f: FilterOption) -> str:
    v = f.get("value")
    if v is None:
        return ""
    if isinstance(v, (list, tuple)):
        return ",".join(_scalar(x) for x in v)
    return _scalar(v)


def _scalar(v: Any) -> str:
    if isinstance(v, bool):  # match JS String(true)
        return "true" if v else "false"
    return str(v)


def _bool(v: bool) -> str:
    return "true" if v else "false"


def _preload_spec(p: Mapping[str, Any]) -> str:
    cols = p.get("columns")
    return f"{p['relation']}:{','.join(cols)}" if cols else p["relation"]


def build_headers(options: HeaderSpecOptions) -> Dict[str, str]:
    """Build restheadspec HTTP headers from options. See README for the mapping."""
    h: Dict[str, str] = {}
    o = options

    if o.get("columns"):
        h["X-Select-Fields"] = ",".join(o["columns"])
    if o.get("omit_columns"):
        h["X-Not-Select-Fields"] = ",".join(o["omit_columns"])

    for f in o.get("filters") or []:
        logic = f.get("logic_operator") or "AND"
        operator = f["operator"]
        op = _OPERATOR_MAP.get(operator, operator)
        value = _filter_value(f)
        geo = _geo_header(operator)
        if geo:
            payload: Dict[str, Any] = {"op": operator, "value": f.get("value")}
            if logic == "OR":
                payload["logic"] = "or"
            h[f"{geo}{f['column']}"] = json.dumps(payload, separators=(",", ":"))
        elif operator == "eq" and logic == "AND":
            h[f"X-FieldFilter-{f['column']}"] = value
        elif logic == "OR":
            h[f"X-SearchOr-{op}-{f['column']}"] = value
        else:
            h[f"X-SearchOp-{op}-{f['column']}"] = value

    if o.get("sort"):
        h["X-Sort"] = ",".join(
            ("-" if s["direction"].upper() == "DESC" else "+") + s["column"] for s in o["sort"]
        )

    if o.get("limit") is not None:
        h["X-Limit"] = str(o["limit"])
    if o.get("offset") is not None:
        h["X-Offset"] = str(o["offset"])
    if o.get("cursor_forward"):
        h["X-Cursor-Forward"] = o["cursor_forward"]
    if o.get("cursor_backward"):
        h["X-Cursor-Backward"] = o["cursor_backward"]

    if o.get("preload"):
        # Go applies X-Preload-Where to every preload in the matching X-Preload header,
        # so preloads are grouped by where clause.
        groups: Dict[str, list] = {}
        for p in o["preload"]:
            groups.setdefault(p.get("where") or "", []).append(_preload_spec(p))
        n = 0
        for where, specs in groups.items():
            if not where:
                h["X-Preload"] = "|".join(specs)
            elif "" not in groups and n == 0:
                # X-Preload-Where would also apply to a where-less X-Preload, so only use it alone
                h["X-Preload"] = "|".join(specs)
                h["X-Preload-Where"] = where
                n += 1
            else:
                n += 1
                h[f"X-Preload-{n}"] = "|".join(specs)
                h[f"X-Preload-{n}-Where"] = where

    if o.get("expand"):
        h["X-Expand"] = "|".join(_preload_spec(e) for e in o["expand"])
    if o.get("custom_sql_joins"):
        h["X-Custom-SQL-Join"] = "|".join(o["custom_sql_joins"])
    if o.get("custom_sql_or"):
        h["X-Custom-SQL-Or"] = " OR ".join(o["custom_sql_or"])
    if o.get("search_columns"):
        h["X-SearchCols"] = ",".join(o["search_columns"])
    for col, sql in (o.get("advanced_sql") or {}).items():
        h[f"X-AdvSQL-{col}"] = sql

    vs = o.get("vector_search")
    if vs:
        h[f"X-Vector-Search-{vs['column']}"] = vs.get("metric") or "l2"
        h["X-Vector-Search-Vector"] = json.dumps(vs["vector"], separators=(",", ":"))
        if vs.get("as"):
            h["X-Vector-Search-As"] = vs["as"]
        if vs.get("direction"):
            h["X-Vector-Search-Dir"] = vs["direction"]

    for name, key in (
        ("X-Clean-JSON", "clean_json"),
        ("X-Distinct", "distinct"),
        ("X-SkipCount", "skip_count"),
        ("X-SkipCache", "skip_cache"),
        ("X-Transaction-Atomic", "atomic_transaction"),
        ("X-Single-Record-As-Object", "single_record_as_object"),
    ):
        if o.get(key) is not None:
            h[name] = _bool(o[key])

    if o.get("pk_row"):
        h["X-PKRow"] = o["pk_row"]

    fmt = o.get("response_format")
    if fmt:
        h[{"simple": "X-SimpleApi", "detail": "X-DetailApi", "syncfusion": "X-Syncfusion"}[fmt]] = "true"

    if o.get("xfiles"):
        h["X-Files"] = encode_header_value(json.dumps(o["xfiles"], separators=(",", ":")))

    if o.get("fetch_row_number"):
        h["X-Fetch-RowNumber"] = o["fetch_row_number"]

    for cc in o.get("computedColumns") or []:
        h[f"X-CQL-SEL-{cc['name']}"] = cc["expression"]

    if o.get("customOperators"):
        h["X-Custom-SQL-W"] = " AND ".join(co["sql"] for co in o["customOperators"])

    return h


def _int(s: Optional[str]) -> int:
    try:
        return int(s)  # type: ignore[arg-type]
    except (TypeError, ValueError):
        return 0


def _wrap(response: httpx.Response) -> APIResponse:
    """Wrap a raw restheadspec body, deriving metadata from Content-Range / X-Limit."""
    data = parse_json(response)
    if not response.is_success:
        raise error_from(response, data)
    cr = response.headers.get("content-range")
    total = _int(cr.split("/")[-1]) if cr else 0
    offset = _int(cr.split("/")[0].split("-")[0].split(" ")[-1]) if cr else 0
    return {
        "data": data,
        "success": True,
        "error": data.get("error") if isinstance(data, dict) else None,
        "metadata": {
            "count": total,
            "total": total,
            "filtered": total,
            "offset": offset,
            "limit": _int(response.headers.get("x-limit")),
        },
    }


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

    def _base_headers(self) -> Dict[str, str]:
        return client_headers(self.token, self.headers)

    def _req(self, method, schema, entity, id, options=None, body=None):
        opt = build_headers(options) if options else {}
        return (
            method,
            build_url(self.base_url, schema, entity, id),
            merge_headers(self._base_headers(), opt),
            body,
        )

    def _read_req(self, schema, entity, id, options):
        return self._req("GET", schema, entity, id, options)

    def _create_req(self, schema, entity, data, options):
        return self._req("POST", schema, entity, None, options, data)

    def _update_req(self, schema, entity, id, data, options):
        return self._req("PUT", schema, entity, id, options, data)

    def _delete_req(self, schema, entity, id):
        return self._req("DELETE", schema, entity, id)


class HeaderSpecClient(_Base):
    """Synchronous client. Use as a context manager or call close()."""

    def __init__(self, *args: Any, transport: Optional[httpx.BaseTransport] = None, **kwargs: Any):
        super().__init__(*args, **kwargs)
        self._http = httpx.Client(timeout=self.timeout, transport=transport)

    def close(self) -> None:
        self._http.close()

    def __enter__(self) -> "HeaderSpecClient":
        return self

    def __exit__(self, *exc: Any) -> None:
        self.close()

    def _send(self, req) -> APIResponse:
        method, url, headers, body = req
        return _wrap(self._http.request(method, url, headers=headers, json=body))

    def read(self, schema: str, entity: str, id: Optional[str] = None, options: Optional[HeaderSpecOptions] = None) -> APIResponse:
        return self._send(self._read_req(schema, entity, id, options))

    def create(self, schema: str, entity: str, data: Any, options: Optional[HeaderSpecOptions] = None) -> APIResponse:
        return self._send(self._create_req(schema, entity, data, options))

    def update(self, schema: str, entity: str, id: str, data: Any, options: Optional[HeaderSpecOptions] = None) -> APIResponse:
        return self._send(self._update_req(schema, entity, id, data, options))

    def delete(self, schema: str, entity: str, id: str) -> APIResponse:
        return self._send(self._delete_req(schema, entity, id))


class AsyncHeaderSpecClient(_Base):
    """Asyncio client. Use as an async context manager or await aclose()."""

    def __init__(self, *args: Any, transport: Optional[httpx.AsyncBaseTransport] = None, **kwargs: Any):
        super().__init__(*args, **kwargs)
        self._http = httpx.AsyncClient(timeout=self.timeout, transport=transport)

    async def aclose(self) -> None:
        await self._http.aclose()

    async def __aenter__(self) -> "AsyncHeaderSpecClient":
        return self

    async def __aexit__(self, *exc: Any) -> None:
        await self.aclose()

    async def _send(self, req) -> APIResponse:
        method, url, headers, body = req
        return _wrap(await self._http.request(method, url, headers=headers, json=body))

    async def read(self, schema: str, entity: str, id: Optional[str] = None, options: Optional[HeaderSpecOptions] = None) -> APIResponse:
        return await self._send(self._read_req(schema, entity, id, options))

    async def create(self, schema: str, entity: str, data: Any, options: Optional[HeaderSpecOptions] = None) -> APIResponse:
        return await self._send(self._create_req(schema, entity, data, options))

    async def update(self, schema: str, entity: str, id: str, data: Any, options: Optional[HeaderSpecOptions] = None) -> APIResponse:
        return await self._send(self._update_req(schema, entity, id, data, options))

    async def delete(self, schema: str, entity: str, id: str) -> APIResponse:
        return await self._send(self._delete_req(schema, entity, id))
