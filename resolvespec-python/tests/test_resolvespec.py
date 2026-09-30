import json

import httpx
import pytest

from resolvespec import (
    AsyncHeaderSpecClient,
    AsyncResolveSpecClient,
    HeaderSpecClient,
    ResolveSpecClient,
    ResolveSpecError,
    get_headerspec_client,
    get_resolvespec_client,
)

CFG = dict(base_url="http://localhost:3000", token="test-token")


def make(handler, **kw):
    return ResolveSpecClient(**{**CFG, **kw}, transport=httpx.MockTransport(handler))


def ok(_req):
    return httpx.Response(200, json={"success": True, "data": [{"id": 1}]})


def capture():
    seen = []

    def handler(req):
        seen.append(req)
        return httpx.Response(200, json={"success": True, "data": {"id": 1, "name": "Test"}})

    return seen, handler


def body(req):
    return json.loads(req.content)


def test_read_with_numeric_id():
    seen, h = capture()
    with make(h) as c:
        assert c.read("public", "users", 1)["success"] is True
    r = seen[0]
    assert str(r.url) == "http://localhost:3000/public/users/1"
    assert r.method == "POST"
    assert r.headers["authorization"] == "Bearer test-token"
    assert r.headers["content-type"] == "application/json"
    assert body(r) == {"operation": "read"}


def test_read_array_id_goes_in_body():
    seen, h = capture()
    with make(h) as c:
        c.read("public", "users", ["1", "2"])
    assert str(seen[0].url) == "http://localhost:3000/public/users"
    assert body(seen[0])["id"] == ["1", "2"]


def test_read_options_passthrough():
    seen, h = capture()
    opts = {
        "columns": ["id", "name"], "omit_columns": ["secret"],
        "filters": [{"column": "active", "operator": "eq", "value": True}],
        "sort": [{"column": "name", "direction": "asc"}],
        "limit": 10, "offset": 0, "cursor_forward": "cursor1", "fetch_row_number": "5",
        "customOperators": [{"name": "x", "sql": "a = 1"}],
    }
    with make(h) as c:
        c.read("public", "users", options=opts)
    assert body(seen[0])["options"] == opts


def test_create():
    seen, h = capture()
    with make(h) as c:
        res = c.create("public", "users", {"name": "Test"})
    assert res["data"]["name"] == "Test"
    assert body(seen[0]) == {"operation": "create", "data": {"name": "Test"}}


def test_create_batch():
    seen, h = capture()
    with make(h) as c:
        c.create("public", "users", [{"a": 1}, {"a": 2}])
    assert body(seen[0])["data"] == [{"a": 1}, {"a": 2}]


def test_update_with_id_in_url_and_array():
    seen, h = capture()
    with make(h) as c:
        c.update("public", "users", {"name": "X"}, 5)
        c.update("public", "users", {"name": "X"}, ["1", "2"])
    assert str(seen[0].url).endswith("/public/users/5")
    assert body(seen[0]) == {"operation": "update", "data": {"name": "X"}}
    assert str(seen[1].url).endswith("/public/users")
    assert body(seen[1])["id"] == ["1", "2"]


def test_update_preserves_empty_string_and_null():
    seen, h = capture()
    with make(h) as c:
        c.update("public", "users", {"a": "", "b": None}, 1)
    assert body(seen[0])["data"] == {"a": "", "b": None}


def test_delete():
    seen, h = capture()
    with make(h) as c:
        c.delete("public", "users", 1)
    assert str(seen[0].url).endswith("/public/users/1")
    assert body(seen[0]) == {"operation": "delete"}


def test_get_metadata():
    seen, h = capture()
    with make(h) as c:
        c.get_metadata("public", "users")
    assert seen[0].method == "GET"
    assert str(seen[0].url) == "http://localhost:3000/public/users"
    assert not seen[0].content


def test_error_uses_server_message():
    with make(lambda r: httpx.Response(404, json={"success": False, "error": {"code": "not_found", "message": "nope"}})) as c:
        with pytest.raises(ResolveSpecError, match="nope") as ei:
            c.read("public", "users", 1)
    assert ei.value.status_code == 404 and ei.value.code == "not_found"


def test_id_is_url_quoted():
    seen, h = capture()
    with make(h) as c:
        c.read("public", "users", "a/b")
    assert str(seen[0].url).endswith("/public/users/a%2Fb")


def test_trailing_slash_base_url():
    seen, h = capture()
    with make(h, base_url="http://localhost:3000/") as c:
        c.read("public", "users")
    assert str(seen[0].url) == "http://localhost:3000/public/users"


async def test_async_client():
    async def handler(req):
        return httpx.Response(200, json={"success": True, "data": [1]})

    async with AsyncResolveSpecClient(**CFG, transport=httpx.MockTransport(handler)) as c:
        assert (await c.read("public", "users"))["data"] == [1]
        assert (await c.create("public", "users", {}))["success"]
        assert (await c.update("public", "users", {}, 1))["success"]
        assert (await c.delete("public", "users", 1))["success"]
        assert (await c.get_metadata("public", "users"))["success"]


# ---- custom headers (ported from custom-headers.test.ts) ----

@pytest.mark.parametrize("cls", [ResolveSpecClient, HeaderSpecClient])
def test_custom_headers_on_every_op_case_insensitive(cls):
    seen = []

    def handler(req):
        seen.append(req)
        return httpx.Response(200, json={"success": True, "data": []})

    headers = {"X-Tenant": "acme", "authorization": "Basic ignored",
               "content-type": "application/custom+json", "x-limit": "99"}
    with cls("http://localhost:3000", "tok", headers, transport=httpx.MockTransport(handler)) as c:
        c.read("public", "users", options={"limit": 10})
        c.create("public", "users", {})
        if cls is ResolveSpecClient:
            c.update("public", "users", {}, "1")
            c.get_metadata("public", "users")
        else:
            c.update("public", "users", "1", {})
        c.delete("public", "users", "1")
    for r in seen:
        assert r.headers["x-tenant"] == "acme"
        assert r.headers["authorization"] == "Bearer tok"
        assert r.headers["content-type"] == "application/custom+json"
    if cls is HeaderSpecClient:
        assert seen[0].headers["x-limit"] == "10"
    assert headers["authorization"] == "Basic ignored"
    assert headers["x-limit"] == "99"


@pytest.mark.parametrize("cls", [ResolveSpecClient, HeaderSpecClient])
def test_custom_auth_without_token(cls):
    seen = []

    def handler(req):
        seen.append(req)
        return httpx.Response(200, json={"success": True, "data": []})

    with cls("http://localhost:3000", headers={"Authorization": "Basic custom"}, transport=httpx.MockTransport(handler)) as c:
        c.read("public", "users")
    assert seen[0].headers["authorization"] == "Basic custom"


@pytest.mark.parametrize("factory", [get_resolvespec_client, get_headerspec_client])
def test_cache_isolation_and_snapshot(factory):
    headers = {"X-Tenant": "acme", "X-App": "grid"}
    first = factory("http://tenant-cache", "one", headers)
    assert factory("http://tenant-cache", "one", {"x-app": "grid", "x-tenant": "acme"}) is first
    assert factory("http://tenant-cache", "two", headers) is not first
    headers["X-Tenant"] = "other"
    assert factory("http://tenant-cache", "one", headers) is not first
    assert first.headers["X-Tenant"] == "acme"
