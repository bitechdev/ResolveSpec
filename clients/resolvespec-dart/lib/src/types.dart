// Types aligned with Go pkg/common/types.go. toJson() emits the wire names.

Map<String, dynamic> _compact(Map<String, dynamic> m) {
  m.removeWhere((_, v) => v == null);
  return m;
}

class FilterOption {
  final String column;

  /// eq neq gt gte lt lte like ilike in contains startswith endswith between
  /// between_inclusive is_null is_not_null
  final String operator;
  final Object? value;

  /// AND | OR
  final String? logicOperator;

  const FilterOption(this.column, this.operator,
      [this.value, this.logicOperator]);

  Map<String, dynamic> toJson() => _compact({
        'column': column,
        'operator': operator,
        'value': value,
        'logic_operator': logicOperator,
      });
}

class SortOption {
  final String column;

  /// asc | desc
  final String direction;

  const SortOption(this.column, [this.direction = 'asc']);

  Map<String, dynamic> toJson() => {'column': column, 'direction': direction};
}

class Parameter {
  final String name;
  final String value;
  final int? sequence;

  const Parameter(this.name, this.value, [this.sequence]);

  Map<String, dynamic> toJson() =>
      _compact({'name': name, 'value': value, 'sequence': sequence});
}

class CustomOperator {
  final String name;
  final String sql;

  const CustomOperator(this.name, this.sql);

  Map<String, dynamic> toJson() => {'name': name, 'sql': sql};
}

class ComputedColumn {
  final String name;
  final String expression;

  const ComputedColumn(this.name, this.expression);

  Map<String, dynamic> toJson() => {'name': name, 'expression': expression};
}

class PreloadOption {
  final String? relation;
  final String? tableName;
  final List<String>? columns;
  final List<String>? omitColumns;
  final List<SortOption>? sort;
  final List<FilterOption>? filters;
  final String? where;
  final int? limit;
  final int? offset;
  final bool? updateable;
  final Map<String, String>? computedQl;
  final bool? recursive;
  final String? primaryKey;
  final String? relatedKey;
  final String? foreignKey;
  final String? recursiveChildKey;
  final List<String>? sqlJoins;
  final List<String>? joinAliases;

  const PreloadOption({
    this.relation,
    this.tableName,
    this.columns,
    this.omitColumns,
    this.sort,
    this.filters,
    this.where,
    this.limit,
    this.offset,
    this.updateable,
    this.computedQl,
    this.recursive,
    this.primaryKey,
    this.relatedKey,
    this.foreignKey,
    this.recursiveChildKey,
    this.sqlJoins,
    this.joinAliases,
  });

  Map<String, dynamic> toJson() => _compact({
        'relation': relation,
        'table_name': tableName,
        'columns': columns,
        'omit_columns': omitColumns,
        'sort': sort?.map((e) => e.toJson()).toList(),
        'filters': filters?.map((e) => e.toJson()).toList(),
        'where': where,
        'limit': limit,
        'offset': offset,
        'updateable': updateable,
        'computed_ql': computedQl,
        'recursive': recursive,
        'primary_key': primaryKey,
        'related_key': relatedKey,
        'foreign_key': foreignKey,
        'recursive_child_key': recursiveChildKey,
        'sql_joins': sqlJoins,
        'join_aliases': joinAliases,
      });
}

class VectorSearchOption {
  final String column;
  final List<double> vector;

  /// l2 (default) | cosine | ip
  final String? metric;

  /// Distance column alias, default _distance.
  final String? as;
  final String? direction;

  const VectorSearchOption(this.column, this.vector,
      {this.metric, this.as, this.direction});

  Map<String, dynamic> toJson() => _compact({
        'column': column,
        'vector': vector,
        'metric': metric,
        'as': as,
        'direction': direction
      });
}

/// ResolveSpec request options object.
class Options {
  final List<PreloadOption>? preload;
  final List<String>? columns;
  final List<String>? omitColumns;
  final List<FilterOption>? filters;
  final List<SortOption>? sort;
  final int? limit;
  final int? offset;
  final List<CustomOperator>? customOperators;
  final List<ComputedColumn>? computedColumns;
  final List<Parameter>? parameters;
  final String? cursorForward;
  final String? cursorBackward;
  final String? fetchRowNumber;
  final VectorSearchOption? vectorSearch;

  const Options({
    this.preload,
    this.columns,
    this.omitColumns,
    this.filters,
    this.sort,
    this.limit,
    this.offset,
    this.customOperators,
    this.computedColumns,
    this.parameters,
    this.cursorForward,
    this.cursorBackward,
    this.fetchRowNumber,
    this.vectorSearch,
  });

  Map<String, dynamic> toJson() => _compact({
        'preload': preload?.map((e) => e.toJson()).toList(),
        'columns': columns,
        'omit_columns': omitColumns,
        'filters': filters?.map((e) => e.toJson()).toList(),
        'sort': sort?.map((e) => e.toJson()).toList(),
        'limit': limit,
        'offset': offset,
        'customOperators': customOperators?.map((e) => e.toJson()).toList(),
        'computedColumns': computedColumns?.map((e) => e.toJson()).toList(),
        'parameters': parameters?.map((e) => e.toJson()).toList(),
        'cursor_forward': cursorForward,
        'cursor_backward': cursorBackward,
        'fetch_row_number': fetchRowNumber,
        'vector_search': vectorSearch?.toJson(),
      });
}

class Metadata {
  final int total;
  final int count;
  final int filtered;
  final int limit;
  final int offset;

  const Metadata(
      {this.total = 0,
      this.count = 0,
      this.filtered = 0,
      this.limit = 0,
      this.offset = 0});

  factory Metadata.fromJson(Map<String, dynamic> j) => Metadata(
        total: (j['total'] as num?)?.toInt() ?? 0,
        count: (j['count'] as num?)?.toInt() ?? 0,
        filtered: (j['filtered'] as num?)?.toInt() ?? 0,
        limit: (j['limit'] as num?)?.toInt() ?? 0,
        offset: (j['offset'] as num?)?.toInt() ?? 0,
      );

  @override
  bool operator ==(Object other) =>
      other is Metadata &&
      other.total == total &&
      other.count == count &&
      other.filtered == filtered &&
      other.limit == limit &&
      other.offset == offset;

  @override
  int get hashCode => Object.hash(total, count, filtered, limit, offset);

  @override
  String toString() =>
      'Metadata(total: $total, count: $count, filtered: $filtered, limit: $limit, offset: $offset)';
}

class ApiError {
  final String code;
  final String message;
  final Object? details;

  /// Server-side reason (funcspec / restheadspec).
  final String? detail;
  final String? sql;

  const ApiError(
      {this.code = '', this.message = '', this.details, this.detail, this.sql});

  factory ApiError.fromJson(Map<String, dynamic> j) => ApiError(
        code: (j['code'] as String?) ?? '',
        message: (j['message'] as String?) ?? '',
        details: j['details'],
        detail: j['detail'] as String?,
        sql: j['sql'] as String?,
      );
}

/// ResolveSpec envelope. [data] is the decoded JSON value (Map, List or scalar).
class Response {
  final bool success;
  final Object? data;
  final Metadata? metadata;
  final ApiError? error;

  const Response({required this.success, this.data, this.metadata, this.error});

  factory Response.fromJson(Map<String, dynamic> j) => Response(
        success: j['success'] == true,
        data: j['data'],
        metadata: j['metadata'] is Map<String, dynamic>
            ? Metadata.fromJson(j['metadata'] as Map<String, dynamic>)
            : null,
        error: j['error'] is Map<String, dynamic>
            ? ApiError.fromJson(j['error'] as Map<String, dynamic>)
            : null,
      );
}
