import httpx
import pytest

from resolvespec import AsyncFuncSpecClient, FuncSpecClient, ResolveSpecError
from resolvespec.funcspec import build_headers, build_query
from resolvespec.headerspec import decode_header_value


def make(handler, **kw):
    return FuncSpecClient("http://localhost:3000", "tok", transport=httpx.MockTransport(handler), **kw)


def capture(status=200, body=None, headers=None):
    seen = []

    def handler(req):
        seen.append(req)
        return httpx.Response(status, json=body if body is not None else [], headers=headers)

    return seen, handler


def test_filters():
    h = build_headers({"filters": [
        {"column": "status", "operator": "eq", "value": "active"},
        {"column": "age", "operator": "gte", "value": 18},
        {"column": "name", "operator": "contains", "value": "x", "logic_operator": "OR"},
        {"column": "deleted", "operator": "is_null", "value": None},
        {"column": "id", "operator": "in", "value": [1, 2]},
        {"column": "p", "operator": "between_inclusive", "value": [1, 5]},
    ]})
    assert h == {
        "X-FieldFilter-status": "active",
        "X-SearchOp-greaterthanorequal-age": "18",
        "X-SearchOr-contains-name": "x",
        "X-SearchOp-empty-deleted": "",
        "X-SearchOp-in-id": "1,2",
        "X-SearchOp-betweeninclusive-p": "1,5",
    }


def test_sort_is_sql_not_prefixed():
    # server inserts sort verbatim into ORDER BY; "-col" would negate the column
    h = build_headers({"sort": [{"column": "name", "direction": "asc"}, {"column": "created_at", "direction": "DESC"}]})
    assert h["X-Sort"] == "name ASC,created_at DESC"


def test_misc_options():
    h = build_headers({
        "search_filters": {"name": "bob"}, "custom_sql_where": "a = 1", "custom_sql_or": "b = 2",
        "limit": 5, "offset": 10, "distinct": True, "skip_count": True, "skip_cache": False,
        "response_format": "syncfusion",
    })
    assert h == {
        "X-SearchFilter-name": "bob", "X-Custom-SQL-W": "a = 1", "X-Custom-SQL-Or": "b = 2",
        "X-Limit": "5", "X-Offset": "10", "X-Distinct": "true", "X-SkipCount": "true",
        "X-SkipCache": "false", "X-Syncfusion": "true",
    }


def test_ambiguous_values_are_encoded():
    h = build_headers({"custom_sql_where": "name = 'café'", "filters": [{"column": "c", "operator": "eq", "value": " pad "}]})
    assert h["X-Custom-SQL-W"].startswith("ZIP_")
    assert decode_header_value(h["X-Custom-SQL-W"]) == "name = 'café'"
    assert decode_header_value(h["X-FieldFilter-c"]) == " pad "


def test_build_query():
    q = build_query({"p-id": 5, "flag": True, "ids": [1, 2], "skip": None, "m": "match=ab"})
    assert q == {"p-id": "5", "flag": "true", "ids": ["1", "2"], "m": "match=ab"}


def test_query_list_request_and_metadata():
    seen, h = capture(206, [{"id": 1}, {"id": 2}], {"content-range": "items 10-12/50"})
    with make(h) as c:
        res = c.query_list("/api/orders", {"p-status": "open", "id": [1, 2]}, {"limit": 2, "offset": 10})
    r = seen[0]
    assert r.method == "GET"
    assert r.url.path == "/api/orders"
    assert r.url.params.multi_items() == [("p-status", "open"), ("id", "1"), ("id", "2")]
    assert r.headers["x-limit"] == "2" and r.headers["authorization"] == "Bearer tok"
    assert res == {
        "success": True,
        "data": [{"id": 1}, {"id": 2}],
        "metadata": {"total": 50, "count": 2, "filtered": 50, "offset": 10, "limit": 2},
    }


def test_query_list_empty_result():
    seen, h = capture(200, [], {"content-range": "items 0-0/0"})
    with make(h) as c:
        assert c.query_list("orders")["metadata"]["total"] == 0
    assert seen[0].url.path == "/orders"


def test_query_single_has_no_metadata_and_method():
    seen, h = capture(200, {"id": 1})
    with make(h) as c:
        res = c.query("api/order", method="post")
    assert seen[0].method == "POST"
    assert res == {"success": True, "data": {"id": 1}}


def test_detail_format_data_passthrough():
    body = {"items": [{"a": 1}], "count": "1", "total": "1", "tablename": "/x", "tableprefix": "gsql"}
    _, h = capture(200, body, {"content-range": "items 0-1/1"})
    with make(h) as c:
        assert c.query_list("x", options={"response_format": "detail"})["data"] == body


def test_server_error_shape():
    err = {"success": False, "error": {"code": "query_failed", "message": "Failed to retrieve records", "detail": "no such column", "sql": "SELECT"}}
    _, h = capture(400, err)
    with make(h) as c:
        with pytest.raises(ResolveSpecError, match="Failed to retrieve") as ei:
            c.query_list("x")
    assert ei.value.code == "query_failed" and ei.value.detail == "no such column" and ei.value.status_code == 400


def test_plain_text_panic_error():
    with make(lambda r: httpx.Response(500, text="Internal server error: boom")) as c:
        with pytest.raises(ResolveSpecError, match="boom"):
            c.query("x")


async def test_async():
    async def handler(req):
        return httpx.Response(200, json=[{"id": 1}], headers={"content-range": "items 0-1/1"})

    async with AsyncFuncSpecClient("http://localhost:3000", transport=httpx.MockTransport(handler)) as c:
        assert (await c.query_list("x"))["metadata"]["total"] == 1
        assert (await c.query("x"))["data"] == [{"id": 1}]
