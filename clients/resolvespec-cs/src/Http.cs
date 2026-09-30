using System.Net.Http.Headers;
using System.Text;
using System.Text.Json;

namespace ResolveSpec;

/// <summary>Shared HTTP configuration for both clients.</summary>
public sealed class ClientOptions
{
    public string? Token { get; set; }
    public Dictionary<string, string> Headers { get; } = new(StringComparer.OrdinalIgnoreCase);
    public TimeSpan Timeout { get; set; } = TimeSpan.FromSeconds(30);
    /// <summary>Supply your own HttpClient (tests, pooling). Its BaseAddress is ignored.</summary>
    public HttpClient? HttpClient { get; set; }
}

internal sealed class Transport
{
    public readonly string BaseUrl;
    readonly ClientOptions _o;
    readonly HttpClient _http;

    public Transport(string baseUrl, ClientOptions? o)
    {
        BaseUrl = baseUrl.TrimEnd('/');
        _o = o ?? new ClientOptions();
        _http = _o.HttpClient ?? new HttpClient { Timeout = _o.Timeout };
    }

    /// <summary>Content-Type &lt; custom headers &lt; per-call headers &lt; bearer token.</summary>
    public async Task<(HttpResponseMessage resp, string body)> SendAsync(
        HttpMethod method, string url, string? json, IDictionary<string, string>? extra, CancellationToken ct)
    {
        using var req = new HttpRequestMessage(method, url);
        if (json != null) req.Content = new StringContent(json, Encoding.UTF8, "application/json");
        foreach (var (k, v) in _o.Headers) Set(req, k, v);
        if (extra != null) foreach (var (k, v) in extra) Set(req, k, v);
        if (!string.IsNullOrEmpty(_o.Token)) req.Headers.Authorization = new AuthenticationHeaderValue("Bearer", _o.Token);
        var resp = await _http.SendAsync(req, ct).ConfigureAwait(false);
        var body = await resp.Content.ReadAsStringAsync(ct).ConfigureAwait(false);
        return (resp, body);
    }

    static void Set(HttpRequestMessage req, string name, string value)
    {
        req.Headers.Remove(name);
        if (!req.Headers.TryAddWithoutValidation(name, value) && req.Content != null)
        {
            req.Content.Headers.Remove(name);
            req.Content.Headers.TryAddWithoutValidation(name, value);
        }
    }

    public static ResolveSpecException ErrorFrom(int status, string body, string? reason)
    {
        ApiError? err = null;
        var isJson = false;
        try
        {
            using var doc = JsonDocument.Parse(body);
            isJson = true;
            if (doc.RootElement.ValueKind == JsonValueKind.Object && doc.RootElement.TryGetProperty("error", out var e) && e.ValueKind == JsonValueKind.Object)
                err = e.Deserialize<ApiError>();
        }
        catch (JsonException) { }

        var message = err?.Message;
        if (string.IsNullOrEmpty(message))
        {
            var text = isJson ? "" : body.Trim();
            if (text.Length > 200) text = text[..200];
            message = text.Length > 0 ? text : $"{reason ?? "Error"} ({status})";
        }
        return new ResolveSpecException(message, status, err);
    }

    public static string Segment(string s) => Uri.EscapeDataString(s);
}
