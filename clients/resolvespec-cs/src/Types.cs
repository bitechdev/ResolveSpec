using System.Text.Json;
using System.Text.Json.Serialization;

namespace ResolveSpec;

// Types aligned with Go pkg/common/types.go. JsonPropertyName values are the wire names.

public sealed class FilterOption
{
    [JsonPropertyName("column")] public string Column { get; set; } = "";
    /// <summary>eq neq gt gte lt lte like ilike in contains startswith endswith between between_inclusive is_null is_not_null</summary>
    [JsonPropertyName("operator")] public string Operator { get; set; } = "eq";
    [JsonPropertyName("value")] public object? Value { get; set; }
    /// <summary>AND | OR</summary>
    [JsonPropertyName("logic_operator")] public string? LogicOperator { get; set; }
}

public sealed class SortOption
{
    [JsonPropertyName("column")] public string Column { get; set; } = "";
    /// <summary>asc | desc</summary>
    [JsonPropertyName("direction")] public string Direction { get; set; } = "asc";
}

public sealed class Parameter
{
    [JsonPropertyName("name")] public string Name { get; set; } = "";
    [JsonPropertyName("value")] public string Value { get; set; } = "";
    [JsonPropertyName("sequence")] public int? Sequence { get; set; }
}

public sealed class CustomOperator
{
    [JsonPropertyName("name")] public string Name { get; set; } = "";
    [JsonPropertyName("sql")] public string Sql { get; set; } = "";
}

public sealed class ComputedColumn
{
    [JsonPropertyName("name")] public string Name { get; set; } = "";
    [JsonPropertyName("expression")] public string Expression { get; set; } = "";
}

public sealed class PreloadOption
{
    [JsonPropertyName("relation")] public string? Relation { get; set; }
    [JsonPropertyName("table_name")] public string? TableName { get; set; }
    [JsonPropertyName("columns")] public List<string>? Columns { get; set; }
    [JsonPropertyName("omit_columns")] public List<string>? OmitColumns { get; set; }
    [JsonPropertyName("sort")] public List<SortOption>? Sort { get; set; }
    [JsonPropertyName("filters")] public List<FilterOption>? Filters { get; set; }
    [JsonPropertyName("where")] public string? Where { get; set; }
    [JsonPropertyName("limit")] public int? Limit { get; set; }
    [JsonPropertyName("offset")] public int? Offset { get; set; }
    [JsonPropertyName("updateable")] public bool? Updateable { get; set; }
    [JsonPropertyName("computed_ql")] public Dictionary<string, string>? ComputedQl { get; set; }
    [JsonPropertyName("recursive")] public bool? Recursive { get; set; }
    [JsonPropertyName("primary_key")] public string? PrimaryKey { get; set; }
    [JsonPropertyName("related_key")] public string? RelatedKey { get; set; }
    [JsonPropertyName("foreign_key")] public string? ForeignKey { get; set; }
    [JsonPropertyName("recursive_child_key")] public string? RecursiveChildKey { get; set; }
    [JsonPropertyName("sql_joins")] public List<string>? SqlJoins { get; set; }
    [JsonPropertyName("join_aliases")] public List<string>? JoinAliases { get; set; }
}

public sealed class VectorSearchOption
{
    [JsonPropertyName("column")] public string Column { get; set; } = "";
    [JsonPropertyName("vector")] public List<double> Vector { get; set; } = new();
    /// <summary>l2 (default) | cosine | ip</summary>
    [JsonPropertyName("metric")] public string? Metric { get; set; }
    /// <summary>Distance column alias, default _distance.</summary>
    [JsonPropertyName("as")] public string? As { get; set; }
    [JsonPropertyName("direction")] public string? Direction { get; set; }
}

/// <summary>ResolveSpec request options object.</summary>
public sealed class Options
{
    [JsonPropertyName("preload")] public List<PreloadOption>? Preload { get; set; }
    [JsonPropertyName("columns")] public List<string>? Columns { get; set; }
    [JsonPropertyName("omit_columns")] public List<string>? OmitColumns { get; set; }
    [JsonPropertyName("filters")] public List<FilterOption>? Filters { get; set; }
    [JsonPropertyName("sort")] public List<SortOption>? Sort { get; set; }
    [JsonPropertyName("limit")] public int? Limit { get; set; }
    [JsonPropertyName("offset")] public int? Offset { get; set; }
    [JsonPropertyName("customOperators")] public List<CustomOperator>? CustomOperators { get; set; }
    [JsonPropertyName("computedColumns")] public List<ComputedColumn>? ComputedColumns { get; set; }
    [JsonPropertyName("parameters")] public List<Parameter>? Parameters { get; set; }
    [JsonPropertyName("cursor_forward")] public string? CursorForward { get; set; }
    [JsonPropertyName("cursor_backward")] public string? CursorBackward { get; set; }
    [JsonPropertyName("fetch_row_number")] public string? FetchRowNumber { get; set; }
    [JsonPropertyName("vector_search")] public VectorSearchOption? VectorSearch { get; set; }
}

public sealed class Metadata
{
    [JsonPropertyName("total")] public long Total { get; set; }
    [JsonPropertyName("count")] public long Count { get; set; }
    [JsonPropertyName("filtered")] public long Filtered { get; set; }
    [JsonPropertyName("limit")] public long Limit { get; set; }
    [JsonPropertyName("offset")] public long Offset { get; set; }
}

public sealed class ApiError
{
    [JsonPropertyName("code")] public string Code { get; set; } = "";
    [JsonPropertyName("message")] public string Message { get; set; } = "";
    [JsonPropertyName("details")] public JsonElement? Details { get; set; }
    /// <summary>Server-side reason (funcspec / restheadspec).</summary>
    [JsonPropertyName("detail")] public string? Detail { get; set; }
    [JsonPropertyName("sql")] public string? Sql { get; set; }
}

/// <summary>ResolveSpec envelope. <see cref="Data"/> is raw JSON; use <see cref="Decode{T}"/>.</summary>
public sealed class Response
{
    [JsonPropertyName("success")] public bool Success { get; set; }
    [JsonPropertyName("data")] public JsonElement Data { get; set; }
    [JsonPropertyName("metadata")] public Metadata? Metadata { get; set; }
    [JsonPropertyName("error")] public ApiError? Error { get; set; }

    public T? Decode<T>() => Data.ValueKind == JsonValueKind.Undefined ? default : Data.Deserialize<T>();
}

/// <summary>Thrown on a non-2xx response or an unsuccessful API result.</summary>
public sealed class ResolveSpecException : Exception
{
    public int StatusCode { get; }
    public ApiError Error { get; }

    public ResolveSpecException(string message, int statusCode, ApiError? error = null) : base(message)
    {
        StatusCode = statusCode;
        Error = error ?? new ApiError { Message = message };
    }
}
