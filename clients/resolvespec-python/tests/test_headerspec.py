import json

import httpx
import pytest

from resolvespec import (
    AsyncHeaderSpecClient,
    HeaderSpecClient,
    ResolveSpecError,
    build_headers,
    decode_header_value,
    encode_header_value,
    get_headerspec_client,
)
import base64

CFG = dict(base_url="http://localhost:3000", token="tok")


# ---- build_headers (ported from headerspec.test.ts) ----

def test_preload_shared_where():
    h = build_headers({"preload": [
        {"relation": "Items", "columns": ["id"], "where": "active = true"},
        {"relation": "Tags", "where": "active = true"},
    ]})
    assert h["X-Preload"] == "Items:id|Tags"
    assert h["X-Preload-Where"] == "active = true"


def test_preload_mixed_where_numbered():
    h = build_headers({"preload": [
        {"relation": "Items", "where": "a = 1"},
        {"relation": "Category"},
        {"relation": "Tags", "where": "b = 2"},
    ]})
    assert h["X-Preload"] == "Category"
    assert "X-Preload-Where" not in h
    assert h["X-Preload-1"] == "Items" and h["X-Preload-1-Where"] == "a = 1"
    assert h["X-Preload-2"] == "Tags" and h["X-Preload-2-Where"] == "b = 2"


def test_expand_joins_or_searchcols_advsql():
    h = build_headers({
        "expand": [{"relation": "Dept", "columns": ["id", "name"]}, {"relation": "Role"}],
        "custom_sql_joins": ["LEFT JOIN a ON a.id = b.id", "INNER JOIN c ON c.id = b.cid"],
        "custom_sql_or": ["x = 1", "y = 2"],
        "search_columns": ["name", "email"],
        "advanced_sql": {"total": "a + b"},
    })
    assert h["X-Expand"] == "Dept:id,name|Role"
    assert h["X-Custom-SQL-Join"] == "LEFT JOIN a ON a.id = b.id|INNER JOIN c ON c.id = b.cid"
    assert h["X-Custom-SQL-Or"] == "x = 1 OR y = 2"
    assert h["X-SearchCols"] == "name,email"
    assert h["X-AdvSQL-total"] == "a + b"


def test_flags_pkrow_format():
    h = build_headers({
        "clean_json": True, "distinct": True, "skip_count": True, "skip_cache": False,
        "atomic_transaction": True, "single_record_as_object": False,
        "pk_row": "42", "response_format": "detail",
    })
    assert h["X-Clean-JSON"] == "true"
    assert h["X-Distinct"] == "true"
    assert h["X-SkipCount"] == "true"
    assert h["X-SkipCache"] == "false"
    assert h["X-Transaction-Atomic"] == "true"
    assert h["X-Single-Record-As-Object"] == "false"
    assert h["X-PKRow"] == "42"
    assert h["X-DetailApi"] == "true"


def test_spatial_and_vector_filters():
    h = build_headers({"filters": [
        {"column": "geom", "operator": "st_dwithin", "value": {"geom": "POINT(0 0)", "distance": 5}, "logic_operator": "OR"},
        {"column": "emb", "operator": "cosine_within", "value": {"vector": [1, 2], "distance": 0.3}},
    ]})
    assert json.loads(h["X-SpatialFilter-geom"]) == {
        "op": "st_dwithin", "value": {"geom": "POINT(0 0)", "distance": 5}, "logic": "or"}
    assert json.loads(h["X-VectorFilter-emb"])["op"] == "cosine_within"


def test_vector_search():
    h = build_headers({"vector_search": {"column": "emb", "vector": [0.1, 0.2], "metric": "cosine", "as": "dist", "direction": "desc"}})
    assert h["X-Vector-Search-emb"] == "cosine"
    assert h["X-Vector-Search-Vector"] == "[0.1,0.2]"
    assert h["X-Vector-Search-As"] == "dist"
    assert h["X-Vector-Search-Dir"] == "desc"


def test_xfiles_zip():
    xf = {"tablename": "users", "prefix": "USR", "limit": 10}
    h = build_headers({"xfiles": xf})
    assert h["X-Files"].startswith("ZIP_")
    assert json.loads(decode_header_value(h["X-Files"])) == xf


def test_columns_and_omit():
    assert build_headers({"columns": ["id", "name", "email"]})["X-Select-Fields"] == "id,name,email"
    assert build_headers({"omit_columns": ["secret", "internal"]})["X-Not-Select-Fields"] == "secret,internal"


def test_filters():
    assert build_headers({"filters": [{"column": "status", "operator": "eq", "value": "active"}]})["X-FieldFilter-status"] == "active"
    assert build_headers({"filters": [{"column": "age", "operator": "gte", "value": 18}]})["X-SearchOp-greaterthanorequal-age"] == "18"
    assert build_headers({"filters": [{"column": "name", "operator": "contains", "value": "test", "logic_operator": "OR"}]})["X-SearchOr-contains-name"] == "test"
    assert build_headers({"filters": [{"column": "price", "operator": "between", "value": [10, 100]}]})["X-SearchOp-between-price"] == "10,100"
    assert build_headers({"filters": [{"column": "deleted_at", "operator": "is_null", "value": None}]})["X-SearchOp-empty-deleted_at"] == ""
    assert build_headers({"filters": [{"column": "id", "operator": "in", "value": [1, 2, 3]}]})["X-SearchOp-in-id"] == "1,2,3"
    assert build_headers({"filters": [{"column": "a", "operator": "eq", "value": True}]})["X-FieldFilter-a"] == "true"


def test_sort_pagination_cursor():
    h = build_headers({
        "sort": [{"column": "name", "direction": "asc"}, {"column": "created_at", "direction": "DESC"}],
        "limit": 25, "offset": 0, "cursor_forward": "abc", "cursor_backward": "xyz",
    })
    assert h["X-Sort"] == "+name,-created_at"
    assert h["X-Limit"] == "25" and h["X-Offset"] == "0"
    assert h["X-Cursor-Forward"] == "abc" and h["X-Cursor-Backward"] == "xyz"


def test_preload_basic_rownumber_computed_custom():
    h = build_headers({
        "preload": [{"relation": "Items", "columns": ["id", "name"]}, {"relation": "Category"}],
        "fetch_row_number": "42",
        "computedColumns": [{"name": "total", "expression": "price * qty"}],
        "customOperators": [{"name": "a", "sql": "status = 'active'"}, {"name": "v", "sql": "verified = true"}],
    })
    assert h["X-Preload"] == "Items:id,name|Category"
    assert h["X-Fetch-RowNumber"] == "42"
    assert h["X-CQL-SEL-total"] == "price * qty"
    assert h["X-Custom-SQL-W"] == "status = 'active' AND verified = true"


def test_empty_options():
    assert build_headers({}) == {}


# ---- encode / decode ----

def test_roundtrip():
    for s in ("some complex value with spaces & symbols!", "café ☕ 你好"):
        enc = encode_header_value(s)
        assert enc.startswith("ZIP_")
        assert decode_header_value(enc) == s


def test_decode_double_underscore_and_plain():
    assert decode_header_value("__" + base64.b64encode(b"hello").decode()) == "hello"
    assert decode_header_value("__" + base64.b64encode("café ☕".encode()).decode()) == "café ☕"
    assert decode_header_value("plain") == "plain"


def test_decode_nested():
    assert decode_header_value(encode_header_value(encode_header_value("x"))) == "x"


# ---- client ----

def make(handler, cls=HeaderSpecClient, **kw):
    return cls(**{**CFG, **kw}, transport=httpx.MockTransport(handler))


def test_read_sends_get_with_headers():
    seen = []

    def handler(req):
        seen.append(req)
        return httpx.Response(200, json=[{"id": 1}], headers={"content-range": "0-9/100", "x-limit": "10"})

    with make(handler) as c:
        res = c.read("public", "users", options={"columns": ["id", "name"], "limit": 10})
    r = seen[0]
    assert str(r.url) == "http://localhost:3000/public/users"
    assert r.method == "GET"
    assert r.headers["x-select-fields"] == "id,name"
    assert r.headers["x-limit"] == "10"
    assert r.headers["authorization"] == "Bearer tok"
    assert res["success"] is True
    assert res["data"] == [{"id": 1}]
    assert res["metadata"] == {"count": 100, "total": 100, "filtered": 100, "offset": 0, "limit": 10}


def test_metadata_defaults_without_content_range():
    with make(lambda r: httpx.Response(200, json=[])) as c:
        assert c.read("public", "users")["metadata"]["total"] == 0


def test_read_with_id_create_update_delete():
    seen = []

    def handler(req):
        seen.append(req)
        return httpx.Response(200, json={})

    with make(handler) as c:
        c.read("public", "users", "42")
        c.create("public", "users", {"name": "Test"})
        c.update("public", "users", "1", {"name": "Updated"}, {"filters": [{"column": "active", "operator": "eq", "value": True}]})
        c.delete("public", "users", "1")
    assert str(seen[0].url) == "http://localhost:3000/public/users/42"
    assert seen[1].method == "POST" and json.loads(seen[1].content) == {"name": "Test"}
    assert seen[2].method == "PUT" and str(seen[2].url).endswith("/public/users/1")
    assert seen[2].headers["x-fieldfilter-active"] == "true"
    assert seen[3].method == "DELETE"


def test_error_response():
    with make(lambda r: httpx.Response(400, json={"error": {"code": "err", "message": "fail"}})) as c:
        with pytest.raises(ResolveSpecError, match="fail") as ei:
            c.read("public", "users")
    assert ei.value.status_code == 400 and ei.value.code == "err"


def test_error_non_json():
    with make(lambda r: httpx.Response(502, text="bad gateway")) as c:
        with pytest.raises(ResolveSpecError, match="bad gateway") as ei:
            c.read("public", "users")
    assert ei.value.status_code == 502


async def test_async_client():
    async def handler(req):
        return httpx.Response(200, json=[{"id": 1}])

    async with AsyncHeaderSpecClient(**CFG, transport=httpx.MockTransport(handler)) as c:
        res = await c.read("public", "users", options={"limit": 1})
    assert res["data"] == [{"id": 1}]


def test_singleton():
    a = get_headerspec_client("http://hs-singleton:3000")
    assert a is get_headerspec_client("http://hs-singleton:3000")
    assert a is not get_headerspec_client("http://hs-singleton-b:3000")
