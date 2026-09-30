using System.Globalization;
using System.Text;
using System.Text.Json;
using System.Text.RegularExpressions;

namespace ResolveSpec;

/// <summary>
/// Options sent to funcspec endpoints as X-* headers.
/// Server behaviour (pkg/funcspec): Sort is inserted raw into ORDER BY (so it is sent as SQL terms);
/// only one search operator per column is kept; values starting with "ZIP_" or "__" are
/// base64-decoded by the server, so such plaintext values cannot be sent faithfully.
/// </summary>
public sealed class FuncSpecOptions
{
    /// <summary>eq+AND -> X-FieldFilter; others X-SearchOp / X-SearchOr.</summary>
    public List<FilterOption>? Filters { get; set; }
    /// <summary>X-SearchFilter-{col}: text ILIKE.</summary>
    public Dictionary<string, string>? SearchFilters { get; set; }
    public string? CustomSqlWhere { get; set; }
    public string? CustomSqlOr { get; set; }
    public List<SortOption>? Sort { get; set; }
    public int? Limit { get; set; }
    public int? Offset { get; set; }
    public bool? Distinct { get; set; }
    public bool? SkipCount { get; set; }
    public bool? SkipCache { get; set; }
    /// <summary>simple | detail | syncfusion</summary>
    public string? ResponseFormat { get; set; }
}

/// <summary>Client for user-defined SQL endpoints. Routes are defined by the server application.</summary>
public sealed class FuncSpecClient
{
    readonly Transport _t;

    public FuncSpecClient(string baseUrl, ClientOptions? options = null) => _t = new Transport(baseUrl, options);

    static readonly Dictionary<string, string> OperatorMap = new()
    {
        ["eq"] = "equals", ["neq"] = "notequals", ["gt"] = "greaterthan", ["gte"] = "greaterthanorequal",
        ["lt"] = "lessthan", ["lte"] = "lessthanorequal", ["like"] = "contains", ["ilike"] = "contains",
        ["contains"] = "contains", ["startswith"] = "beginswith", ["endswith"] = "endswith", ["in"] = "in",
        ["between"] = "between", ["between_inclusive"] = "betweeninclusive",
        ["is_null"] = "empty", ["is_not_null"] = "notempty",
    };

    static string Scalar(object? v) => v switch
    {
        null => "",
        string s => s,
        bool b => b ? "true" : "false",
        JsonElement { ValueKind: JsonValueKind.Null } => "",
        JsonElement e => e.ValueKind == JsonValueKind.String ? e.GetString() ?? "" : e.ToString(),
        IFormattable f => f.ToString(null, CultureInfo.InvariantCulture),
        _ => v.ToString() ?? "",
    };

    static string FilterValue(object? v) =>
        v is System.Collections.IEnumerable list and not string
            ? string.Join(",", list.Cast<object?>().Select(Scalar))
            : Scalar(v);

    /// <summary>Base64 (UTF-8) with the ZIP_ prefix.</summary>
    public static string EncodeHeaderValue(string v) => "ZIP_" + Convert.ToBase64String(Encoding.UTF8.GetBytes(v));

    /// <summary>Decode a value that may carry a ZIP_ or __ prefix (nested allowed).</summary>
    public static string DecodeHeaderValue(string v)
    {
        foreach (var p in new[] { "ZIP_", "__" })
        {
            if (!v.StartsWith(p, StringComparison.Ordinal)) continue;
            var b64 = Regex.Replace(v[p.Length..], "[\n\r ]", "");
            b64 = b64.PadRight(b64.Length + (4 - b64.Length % 4) % 4, '=');
            try { return DecodeHeaderValue(Encoding.UTF8.GetString(Convert.FromBase64String(b64))); }
            catch (FormatException) { return v; }
        }
        return v;
    }

    /// <summary>Encode values that are unsafe as raw header/query text (non-ASCII, control chars, edge spaces).</summary>
    static string Safe(string v) =>
        v != v.Trim() || v.Any(c => c > 127 || char.IsControl(c)) ? EncodeHeaderValue(v) : v;

    /// <summary>Build the X-* headers understood by funcspec.ParseParameters.</summary>
    public static Dictionary<string, string> BuildHeaders(FuncSpecOptions? o)
    {
        var h = new Dictionary<string, string>();
        if (o == null) return h;

        foreach (var f in o.Filters ?? new())
        {
            var logic = string.IsNullOrEmpty(f.LogicOperator) ? "AND" : f.LogicOperator;
            var v = Safe(FilterValue(f.Value));
            if (f.Operator == "eq" && logic == "AND") { h[$"X-FieldFilter-{f.Column}"] = v; continue; }
            var op = OperatorMap.TryGetValue(f.Operator, out var m) ? m : f.Operator;
            h[$"{(logic == "OR" ? "X-SearchOr" : "X-SearchOp")}-{op}-{f.Column}"] = v;
        }
        foreach (var (col, text) in o.SearchFilters ?? new()) h[$"X-SearchFilter-{col}"] = Safe(text);
        if (!string.IsNullOrEmpty(o.CustomSqlWhere)) h["X-Custom-SQL-W"] = Safe(o.CustomSqlWhere);
        if (!string.IsNullOrEmpty(o.CustomSqlOr)) h["X-Custom-SQL-Or"] = Safe(o.CustomSqlOr);
        if (o.Sort is { Count: > 0 })
        {
            // funcspec puts this verbatim into ORDER BY
            h["X-Sort"] = Safe(string.Join(",", o.Sort.Select(s =>
                $"{s.Column} {(string.Equals(s.Direction, "desc", StringComparison.OrdinalIgnoreCase) ? "DESC" : "ASC")}")));
        }
        if (o.Limit != null) h["X-Limit"] = o.Limit.Value.ToString(CultureInfo.InvariantCulture);
        if (o.Offset != null) h["X-Offset"] = o.Offset.Value.ToString(CultureInfo.InvariantCulture);
        if (o.Distinct != null) h["X-Distinct"] = Bool(o.Distinct.Value);
        if (o.SkipCount != null) h["X-SkipCount"] = Bool(o.SkipCount.Value);
        if (o.SkipCache != null) h["X-SkipCache"] = Bool(o.SkipCache.Value);
        switch (o.ResponseFormat)
        {
            case "simple": h["X-SimpleApi"] = "true"; break;
            case "detail": h["X-DetailApi"] = "true"; break;
            case "syncfusion": h["X-Syncfusion"] = "true"; break;
        }
        return h;
    }

    static string Bool(bool b) => b ? "true" : "false";

    /// <summary>Build query-string pairs: bools -> true/false, lists -> repeated keys, null skipped.</summary>
    public static List<KeyValuePair<string, string>> BuildQuery(IDictionary<string, object?>? p)
    {
        var o = new List<KeyValuePair<string, string>>();
        foreach (var (k, v) in p ?? new Dictionary<string, object?>())
        {
            if (v == null) continue;
            if (v is System.Collections.IEnumerable list and not string)
                foreach (var e in list) o.Add(new(k, Safe(Scalar(e))));
            else o.Add(new(k, Safe(Scalar(v))));
        }
        return o;
    }

    static readonly Regex ContentRange = new(@"(\d+)-(\d+)/(\d+)");

    static Metadata MetadataFrom(string? contentRange, FuncSpecOptions? o)
    {
        var m = new Metadata { Limit = o?.Limit ?? 0 };
        var g = ContentRange.Match(contentRange ?? "");
        if (g.Success)
        {
            var start = long.Parse(g.Groups[1].Value, CultureInfo.InvariantCulture);
            var end = long.Parse(g.Groups[2].Value, CultureInfo.InvariantCulture);
            var total = long.Parse(g.Groups[3].Value, CultureInfo.InvariantCulture);
            m.Total = total; m.Filtered = total; m.Count = end - start; m.Offset = start;
        }
        return m;
    }

    async Task<Response> CallAsync(HttpMethod method, string path, IDictionary<string, object?>? p, FuncSpecOptions? o, bool list, CancellationToken ct)
    {
        var url = $"{_t.BaseUrl}/{path.TrimStart('/')}";
        var q = BuildQuery(p);
        if (q.Count > 0)
            url += "?" + string.Join("&", q.Select(kv => $"{Uri.EscapeDataString(kv.Key)}={Uri.EscapeDataString(kv.Value)}"));

        var (resp, text) = await _t.SendAsync(method, url, null, BuildHeaders(o), ct).ConfigureAwait(false);
        var status = (int)resp.StatusCode;
        if (!resp.IsSuccessStatusCode) throw Transport.ErrorFrom(status, text, resp.ReasonPhrase); // 206 is success

        var r = new Response
        {
            Success = true,
            Data = string.IsNullOrWhiteSpace(text) ? JsonDocument.Parse("null").RootElement.Clone() : JsonDocument.Parse(text).RootElement.Clone(),
        };
        if (list)
        {
            // Content-Range is a content header in HttpClient; fall back to response headers.
            IEnumerable<string>? cr = null;
            if (!resp.Content.Headers.TryGetValues("Content-Range", out cr)) resp.Headers.TryGetValues("Content-Range", out cr);
            r.Metadata = MetadataFrom(cr?.FirstOrDefault(), o);
        }
        return r;
    }

    /// <summary>Single-record endpoint (SqlQuery). Data is the row object.</summary>
    public Task<Response> QueryAsync(string path, IDictionary<string, object?>? p = null, FuncSpecOptions? o = null, HttpMethod? method = null, CancellationToken ct = default) =>
        CallAsync(method ?? HttpMethod.Get, path, p, o, false, ct);

    /// <summary>List endpoint (SqlQueryList). Metadata comes from Content-Range.</summary>
    public Task<Response> QueryListAsync(string path, IDictionary<string, object?>? p = null, FuncSpecOptions? o = null, HttpMethod? method = null, CancellationToken ct = default) =>
        CallAsync(method ?? HttpMethod.Get, path, p, o, true, ct);
}
