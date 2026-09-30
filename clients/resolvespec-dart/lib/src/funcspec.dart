import 'dart:convert';

import 'client.dart';
import 'types.dart';

/// Options sent to funcspec endpoints as X-* headers.
///
/// Server behaviour (pkg/funcspec): [sort] is inserted raw into ORDER BY (so it is sent as SQL
/// terms); only one search operator per column is kept; values starting with `ZIP_` or `__`
/// are base64-decoded by the server, so such plaintext values cannot be sent faithfully.
class FuncSpecOptions {
  /// eq+AND -> X-FieldFilter; others X-SearchOp / X-SearchOr.
  final List<FilterOption>? filters;

  /// X-SearchFilter-{col}: text ILIKE.
  final Map<String, String>? searchFilters;
  final String? customSqlWhere;
  final String? customSqlOr;
  final List<SortOption>? sort;
  final int? limit;
  final int? offset;
  final bool? distinct;
  final bool? skipCount;
  final bool? skipCache;

  /// simple | detail | syncfusion
  final String? responseFormat;

  const FuncSpecOptions({
    this.filters,
    this.searchFilters,
    this.customSqlWhere,
    this.customSqlOr,
    this.sort,
    this.limit,
    this.offset,
    this.distinct,
    this.skipCount,
    this.skipCache,
    this.responseFormat,
  });
}

const _operatorMap = {
  'eq': 'equals',
  'neq': 'notequals',
  'gt': 'greaterthan',
  'gte': 'greaterthanorequal',
  'lt': 'lessthan',
  'lte': 'lessthanorequal',
  'like': 'contains',
  'ilike': 'contains',
  'contains': 'contains',
  'startswith': 'beginswith',
  'endswith': 'endswith',
  'in': 'in',
  'between': 'between',
  'between_inclusive': 'betweeninclusive',
  'is_null': 'empty',
  'is_not_null': 'notempty',
};

String _scalar(Object? v) => v == null ? '' : v.toString();

String _filterValue(Object? v) =>
    v is Iterable ? v.map(_scalar).join(',') : _scalar(v);

/// Base64 (UTF-8) with the `ZIP_` prefix.
String encodeHeaderValue(String v) => 'ZIP_${base64.encode(utf8.encode(v))}';

/// Decode a value that may carry a `ZIP_` or `__` prefix (nested allowed).
String decodeHeaderValue(String v) {
  for (final p in const ['ZIP_', '__']) {
    if (v.startsWith(p)) {
      var b64 = v.substring(p.length).replaceAll(RegExp(r'[\n\r ]'), '');
      b64 = b64.padRight(b64.length + (4 - b64.length % 4) % 4, '=');
      try {
        return decodeHeaderValue(utf8.decode(base64.decode(b64)));
      } on FormatException {
        return v;
      }
    }
  }
  return v;
}

/// Encode values that are unsafe as raw header/query text (non-ASCII, control chars, edge spaces).
String _safe(String v) {
  final unsafe =
      v != v.trim() || v.runes.any((c) => c > 127 || c < 32 || c == 127);
  return unsafe ? encodeHeaderValue(v) : v;
}

/// Build the X-* headers understood by funcspec.ParseParameters.
Map<String, String> buildHeaders(FuncSpecOptions? o) {
  final h = <String, String>{};
  if (o == null) return h;

  for (final f in o.filters ?? const <FilterOption>[]) {
    final logic = f.logicOperator ?? 'AND';
    final v = _safe(_filterValue(f.value));
    if (f.operator == 'eq' && logic == 'AND') {
      h['X-FieldFilter-${f.column}'] = v;
    } else {
      final kind = logic == 'OR' ? 'X-SearchOr' : 'X-SearchOp';
      h['$kind-${_operatorMap[f.operator] ?? f.operator}-${f.column}'] = v;
    }
  }
  o.searchFilters
      ?.forEach((col, text) => h['X-SearchFilter-$col'] = _safe(text));
  if (o.customSqlWhere != null && o.customSqlWhere!.isNotEmpty) {
    h['X-Custom-SQL-W'] = _safe(o.customSqlWhere!);
  }
  if (o.customSqlOr != null && o.customSqlOr!.isNotEmpty) {
    h['X-Custom-SQL-Or'] = _safe(o.customSqlOr!);
  }
  if (o.sort != null && o.sort!.isNotEmpty) {
    // funcspec puts this verbatim into ORDER BY
    h['X-Sort'] = _safe(o.sort!
        .map((s) =>
            '${s.column} ${s.direction.toLowerCase() == 'desc' ? 'DESC' : 'ASC'}')
        .join(','));
  }
  if (o.limit != null) h['X-Limit'] = '${o.limit}';
  if (o.offset != null) h['X-Offset'] = '${o.offset}';
  if (o.distinct != null) h['X-Distinct'] = '${o.distinct}';
  if (o.skipCount != null) h['X-SkipCount'] = '${o.skipCount}';
  if (o.skipCache != null) h['X-SkipCache'] = '${o.skipCache}';
  switch (o.responseFormat) {
    case 'simple':
      h['X-SimpleApi'] = 'true';
    case 'detail':
      h['X-DetailApi'] = 'true';
    case 'syncfusion':
      h['X-Syncfusion'] = 'true';
  }
  return h;
}

/// Build query-string pairs: lists -> repeated keys, null skipped, bools -> true/false.
Map<String, List<String>> buildQuery(Map<String, Object?>? params) {
  final out = <String, List<String>>{};
  params?.forEach((k, v) {
    if (v == null) return;
    out[k] = v is Iterable
        ? v.map((e) => _safe(_scalar(e))).toList()
        : [_safe(_scalar(v))];
  });
  return out;
}

String? _header(Map<String, String> headers, String name) {
  for (final e in headers.entries) {
    if (e.key.toLowerCase() == name) return e.value;
  }
  return null;
}

final _contentRange = RegExp(r'(\d+)-(\d+)/(\d+)');

Metadata _metadata(String? contentRange, FuncSpecOptions? o) {
  final m = _contentRange.firstMatch(contentRange ?? '');
  if (m == null) return Metadata(limit: o?.limit ?? 0);
  final start = int.parse(m.group(1)!);
  final end = int.parse(m.group(2)!);
  final total = int.parse(m.group(3)!);
  return Metadata(
      total: total,
      count: end - start,
      filtered: total,
      limit: o?.limit ?? 0,
      offset: start);
}

/// Client for user-defined SQL endpoints. Routes are defined by the server application.
class FuncSpecClient {
  final Transport _t;

  FuncSpecClient(String baseUrl, [ClientOptions? options])
      : _t = Transport(baseUrl, options);

  void close() => _t.close();

  Future<Response> _call(String method, String path,
      Map<String, Object?>? params, FuncSpecOptions? o, bool list) async {
    final base =
        Uri.parse('${_t.baseUrl}/${path.replaceAll(RegExp(r'^/+'), '')}');
    final pairs = <String>[];
    buildQuery(params).forEach((k, vs) {
      for (final v in vs) {
        pairs.add(
            '${Uri.encodeQueryComponent(k)}=${Uri.encodeQueryComponent(v)}');
      }
    });
    final uri = pairs.isEmpty ? base : base.replace(query: pairs.join('&'));

    final resp = await _t.send(method, uri, extra: buildHeaders(o));
    if (resp.statusCode < 200 || resp.statusCode > 299) {
      throw Transport.errorFrom(resp); // 206 is success
    }
    final text = utf8.decode(resp.bodyBytes);
    return Response(
      success: true,
      data: text.trim().isEmpty ? null : jsonDecode(text),
      metadata:
          list ? _metadata(_header(resp.headers, 'content-range'), o) : null,
    );
  }

  /// Single-record endpoint (SqlQuery). `data` is the row object.
  Future<Response> query(String path,
          {Map<String, Object?>? params,
          FuncSpecOptions? options,
          String method = 'GET'}) =>
      _call(method.toUpperCase(), path, params, options, false);

  /// List endpoint (SqlQueryList). Metadata comes from Content-Range.
  Future<Response> queryList(String path,
          {Map<String, Object?>? params,
          FuncSpecOptions? options,
          String method = 'GET'}) =>
      _call(method.toUpperCase(), path, params, options, true);
}
