using System.Text.Json;
using System.Text.Json.Serialization;

namespace ResolveSpec;

/// <summary>Client for the ResolveSpec JSON body protocol: POST {operation, data, options}.</summary>
public sealed class ResolveSpecClient
{
    static readonly JsonSerializerOptions Json = new() { DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull };
    readonly Transport _t;

    public ResolveSpecClient(string baseUrl, ClientOptions? options = null) => _t = new Transport(baseUrl, options);

    sealed class Request
    {
        [JsonPropertyName("operation")] public string Operation { get; set; } = "";
        [JsonPropertyName("id")] public string[]? Id { get; set; }
        [JsonPropertyName("data")] public object? Data { get; set; }
        [JsonPropertyName("options")] public Options? Options { get; set; }
    }

    // A single id (int/long/string) goes in the URL; string[] / IEnumerable<string> goes in the body.
    static string? UrlId(object? id) => id switch
    {
        null => null,
        string s => s,
        IEnumerable<string> => null,
        _ => Convert.ToString(id, System.Globalization.CultureInfo.InvariantCulture),
    };

    static string[]? BodyId(object? id) => id is IEnumerable<string> e and not string ? e.ToArray() : null;

    string Url(string schema, string entity, string? id)
    {
        var u = $"{_t.BaseUrl}/{Transport.Segment(schema)}/{Transport.Segment(entity)}";
        return string.IsNullOrEmpty(id) ? u : $"{u}/{Transport.Segment(id)}";
    }

    async Task<Response> SendAsync(HttpMethod method, string url, Request? body, CancellationToken ct)
    {
        var json = body == null ? null : JsonSerializer.Serialize(body, Json);
        var (resp, text) = await _t.SendAsync(method, url, json, null, ct).ConfigureAwait(false);
        var status = (int)resp.StatusCode;
        if (!resp.IsSuccessStatusCode) throw Transport.ErrorFrom(status, text, resp.ReasonPhrase);
        var r = JsonSerializer.Deserialize<Response>(text, Json) ?? new Response();
        if (!r.Success && r.Error != null) throw new ResolveSpecException(r.Error.Message, status, r.Error);
        return r;
    }

    /// <summary>GET /{schema}/{entity}</summary>
    public Task<Response> GetMetadataAsync(string schema, string entity, CancellationToken ct = default) =>
        SendAsync(HttpMethod.Get, Url(schema, entity, null), null, ct);

    public Task<Response> ReadAsync(string schema, string entity, object? id = null, Options? options = null, CancellationToken ct = default) =>
        SendAsync(HttpMethod.Post, Url(schema, entity, UrlId(id)), new Request { Operation = "read", Id = BodyId(id), Options = options }, ct);

    public Task<Response> CreateAsync(string schema, string entity, object data, Options? options = null, CancellationToken ct = default) =>
        SendAsync(HttpMethod.Post, Url(schema, entity, null), new Request { Operation = "create", Data = data, Options = options }, ct);

    public Task<Response> UpdateAsync(string schema, string entity, object data, object? id = null, Options? options = null, CancellationToken ct = default) =>
        SendAsync(HttpMethod.Post, Url(schema, entity, UrlId(id)), new Request { Operation = "update", Id = BodyId(id), Data = data, Options = options }, ct);

    public Task<Response> DeleteAsync(string schema, string entity, object id, CancellationToken ct = default) =>
        SendAsync(HttpMethod.Post, Url(schema, entity, UrlId(id)), new Request { Operation = "delete" }, ct);
}
