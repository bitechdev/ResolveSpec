using System.Net;
using System.Text;
using System.Text.Json;
using ResolveSpec;
using Xunit;

public class Stub : HttpMessageHandler
{
    public HttpRequestMessage? Request;
    public string Body = "";
    readonly HttpStatusCode _status;
    readonly string _json;
    readonly Dictionary<string, string> _headers;

    public Stub(HttpStatusCode status, string json, Dictionary<string, string>? headers = null)
    {
        _status = status; _json = json; _headers = headers ?? new();
    }

    protected override async Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken ct)
    {
        Request = request;
        Body = request.Content == null ? "" : await request.Content.ReadAsStringAsync(ct);
        var r = new HttpResponseMessage(_status) { Content = new StringContent(_json, Encoding.UTF8, "application/json") };
        foreach (var (k, v) in _headers)
            if (!r.Headers.TryAddWithoutValidation(k, v)) r.Content.Headers.TryAddWithoutValidation(k, v);
        return r;
    }
}

public class ResolveSpecTests
{
    static (ResolveSpecClient, Stub) Make(HttpStatusCode s, string json)
    {
        var stub = new Stub(s, json);
        var o = new ClientOptions { Token = "tok", HttpClient = new HttpClient(stub) };
        o.Headers["X-Tenant"] = "a";
        return (new ResolveSpecClient("http://localhost:3000/", o), stub);
    }

    [Fact]
    public async Task ReadPostsBody()
    {
        var (c, s) = Make(HttpStatusCode.OK, """{"success":true,"data":[{"id":1}]}""");
        var r = await c.ReadAsync("public", "users", null, new Options { Limit = 5, Filters = new() { new FilterOption { Column = "a", Operator = "eq", Value = 1 } } });
        Assert.Equal(HttpMethod.Post, s.Request!.Method);
        Assert.Equal("/public/users", s.Request.RequestUri!.AbsolutePath);
        Assert.Equal("Bearer tok", s.Request.Headers.Authorization!.ToString());
        Assert.Equal("a", s.Request.Headers.GetValues("X-Tenant").Single());
        using var body = JsonDocument.Parse(s.Body);
        Assert.Equal("read", body.RootElement.GetProperty("operation").GetString());
        Assert.Equal(5, body.RootElement.GetProperty("options").GetProperty("limit").GetInt32());
        Assert.False(body.RootElement.TryGetProperty("id", out _));
        Assert.Single(r.Decode<List<Dictionary<string, int>>>()!);
    }

    [Fact]
    public async Task IdPlacement()
    {
        var (c, s) = Make(HttpStatusCode.OK, """{"success":true,"data":{}}""");
        await c.ReadAsync("s", "e", 7);
        Assert.Equal("/s/e/7", s.Request!.RequestUri!.AbsolutePath);
        await c.UpdateAsync("s", "e", new { a = 1 }, new[] { "1", "2" });
        Assert.Equal("/s/e", s.Request!.RequestUri!.AbsolutePath);
        using (var b = JsonDocument.Parse(s.Body))
        {
            Assert.Equal(2, b.RootElement.GetProperty("id").GetArrayLength());
            Assert.Equal("update", b.RootElement.GetProperty("operation").GetString());
        }
        await c.DeleteAsync("s", "e", "a/b");
        Assert.Equal("/s/e/a%2Fb", s.Request!.RequestUri!.AbsoluteUri[(s.Request.RequestUri.AbsoluteUri.IndexOf("/s/e", StringComparison.Ordinal))..]);
        Assert.Contains("\"delete\"", s.Body);
    }

    [Fact]
    public async Task Errors()
    {
        var (c, _) = Make(HttpStatusCode.BadRequest, """{"success":false,"error":{"code":"x","message":"bad","detail":"why"}}""");
        var e = await Assert.ThrowsAsync<ResolveSpecException>(() => c.ReadAsync("s", "e"));
        Assert.Equal((400, "x", "bad", "why"), (e.StatusCode, e.Error.Code, e.Message, e.Error.Detail));

        var (c2, _) = Make(HttpStatusCode.BadGateway, "bad gateway");
        var e2 = await Assert.ThrowsAsync<ResolveSpecException>(() => c2.ReadAsync("s", "e"));
        Assert.Equal((502, "bad gateway"), (e2.StatusCode, e2.Message));

        var (c3, _) = Make(HttpStatusCode.OK, """{"success":false,"error":{"code":"c","message":"nope"}}""");
        var e3 = await Assert.ThrowsAsync<ResolveSpecException>(() => c3.ReadAsync("s", "e"));
        Assert.Equal("nope", e3.Message);
    }
}

public class FuncSpecTests
{
    [Fact]
    public void HeaderFilters()
    {
        var h = FuncSpecClient.BuildHeaders(new FuncSpecOptions
        {
            Filters = new()
            {
                new() { Column = "status", Operator = "eq", Value = "active" },
                new() { Column = "age", Operator = "gte", Value = 18 },
                new() { Column = "name", Operator = "contains", Value = "x", LogicOperator = "OR" },
                new() { Column = "deleted", Operator = "is_null" },
                new() { Column = "id", Operator = "in", Value = new[] { 1, 2 } },
                new() { Column = "p", Operator = "between_inclusive", Value = new[] { 1, 5 } },
            },
        });
        Assert.Equal(new Dictionary<string, string>
        {
            ["X-FieldFilter-status"] = "active",
            ["X-SearchOp-greaterthanorequal-age"] = "18",
            ["X-SearchOr-contains-name"] = "x",
            ["X-SearchOp-empty-deleted"] = "",
            ["X-SearchOp-in-id"] = "1,2",
            ["X-SearchOp-betweeninclusive-p"] = "1,5",
        }, h);
    }

    [Fact]
    public void HeaderMiscAndEncoding()
    {
        var h = FuncSpecClient.BuildHeaders(new FuncSpecOptions
        {
            SearchFilters = new() { ["name"] = "bob" }, CustomSqlWhere = "a = 1", CustomSqlOr = "b = 2",
            Sort = new() { new() { Column = "name", Direction = "asc" }, new() { Column = "created_at", Direction = "DESC" } },
            Limit = 5, Offset = 10, Distinct = true, SkipCount = true, SkipCache = false, ResponseFormat = "syncfusion",
        });
        Assert.Equal("name ASC,created_at DESC", h["X-Sort"]);
        Assert.Equal("bob", h["X-SearchFilter-name"]);
        Assert.Equal("a = 1", h["X-Custom-SQL-W"]);
        Assert.Equal("false", h["X-SkipCache"]);
        Assert.Equal("true", h["X-Syncfusion"]);

        h = FuncSpecClient.BuildHeaders(new FuncSpecOptions { Filters = new()
        {
            new() { Column = "n", Operator = "eq", Value = "héllo" },
            new() { Column = "m", Operator = "eq", Value = " pad" },
        } });
        Assert.StartsWith("ZIP_", h["X-FieldFilter-n"]);
        Assert.Equal("héllo", FuncSpecClient.DecodeHeaderValue(h["X-FieldFilter-n"]));
        Assert.Equal(" pad", FuncSpecClient.DecodeHeaderValue(h["X-FieldFilter-m"]));
    }

    [Fact]
    public void QueryBuilding()
    {
        var q = FuncSpecClient.BuildQuery(new Dictionary<string, object?> { ["a"] = true, ["b"] = new[] { "x", "y" }, ["c"] = null, ["d"] = 3 });
        Assert.Equal(new[] { "a=true", "b=x", "b=y", "d=3" }, q.Select(kv => $"{kv.Key}={kv.Value}"));
    }

    [Fact]
    public async Task QueryListMetadata()
    {
        var stub = new Stub((HttpStatusCode)206, """[{"id":1},{"id":2}]""", new() { ["Content-Range"] = "items 10-12/50" });
        var c = new FuncSpecClient("http://x", new ClientOptions { Token = "tok", HttpClient = new HttpClient(stub) });
        var r = await c.QueryListAsync("/api/users", new Dictionary<string, object?> { ["org"] = 1 }, new FuncSpecOptions { Limit = 2 });
        Assert.Equal("GET", stub.Request!.Method.Method);
        Assert.Equal("/api/users", stub.Request.RequestUri!.AbsolutePath);
        Assert.Equal("?org=1", stub.Request.RequestUri.Query);
        Assert.Equal("2", stub.Request.Headers.GetValues("X-Limit").Single());
        Assert.Equal((50L, 2L, 50L, 2L, 10L), (r.Metadata!.Total, r.Metadata.Count, r.Metadata.Filtered, r.Metadata.Limit, r.Metadata.Offset));
        Assert.Equal(2, r.Data.GetArrayLength());
    }

    [Fact]
    public async Task QuerySingleAndError()
    {
        var ok = new FuncSpecClient("http://x", new ClientOptions { HttpClient = new HttpClient(new Stub(HttpStatusCode.OK, """{"id":1}""")) });
        var r = await ok.QueryAsync("api/u");
        Assert.Null(r.Metadata);
        Assert.Equal(1, r.Data.GetProperty("id").GetInt32());

        var bad = new FuncSpecClient("http://x", new ClientOptions { HttpClient = new HttpClient(new Stub(HttpStatusCode.BadRequest,
            """{"success":false,"error":{"code":"hook_error","message":"Hook execution failed","detail":"authentication required"}}""")) });
        var e = await Assert.ThrowsAsync<ResolveSpecException>(() => bad.QueryAsync("api/u"));
        Assert.Equal(("hook_error", "authentication required"), (e.Error.Code, e.Error.Detail));
    }
}
