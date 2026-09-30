"""Types aligned with Go pkg/common/types.go. Dict keys are the wire names."""
from __future__ import annotations

from typing import Any, Dict, List, NotRequired, TypedDict, Union

Operator = str  # eq neq gt gte lt lte like ilike in contains startswith endswith
#                 between between_inclusive is_null is_not_null
#                 st_dwithin bbox (spatial) | l2_within cosine_within ip_within (vector)
Operation = str  # read | create | update | delete
SortDirection = str  # asc | desc | ASC | DESC
VectorMetric = str  # l2 | cosine | ip
ResponseFormat = str  # simple | detail | syncfusion

RecordId = Union[int, str, List[str]]


class Parameter(TypedDict):
    name: str
    value: str
    sequence: NotRequired[int]


class FilterOption(TypedDict):
    column: str
    operator: str
    value: Any
    logic_operator: NotRequired[str]  # "AND" | "OR"


class SortOption(TypedDict):
    column: str
    direction: str


class CustomOperator(TypedDict):
    name: str
    sql: str


class ComputedColumn(TypedDict):
    name: str
    expression: str


class PreloadOption(TypedDict, total=False):
    relation: str
    table_name: str
    columns: List[str]
    omit_columns: List[str]
    sort: List[SortOption]
    filters: List[FilterOption]
    where: str
    limit: int
    offset: int
    updateable: bool
    computed_ql: Dict[str, str]
    recursive: bool
    primary_key: str
    related_key: str
    foreign_key: str
    recursive_child_key: str
    sql_joins: List[str]
    join_aliases: List[str]


# `as` is a keyword, so the functional syntax is required.
VectorSearchOption = TypedDict(
    "VectorSearchOption",
    {
        "column": str,
        "vector": List[float],
        "metric": str,  # l2 (default) | cosine | ip
        "as": str,  # distance column alias, default _distance
        "direction": str,  # asc (default) | desc
    },
    total=False,
)


class ExpandOption(TypedDict, total=False):
    relation: str
    columns: List[str]


class XFiles(TypedDict, total=False):
    tablename: str
    schema: str
    primarykey: str
    foreignkey: str
    relatedkey: str
    sort: List[str]
    prefix: str
    editable: bool
    recursive: bool
    expand: bool
    rownumber: bool
    skipcount: bool
    offset: int
    limit: int
    columns: List[str]
    omit_columns: List[str]
    cql_columns: List[str]
    sql_joins: List[str]
    sql_or: List[str]
    sql_and: List[str]
    parenttables: List["XFiles"]
    childtables: List["XFiles"]
    filter_fields: List[Dict[str, str]]
    cursor_forward: str
    cursor_backward: str


class Options(TypedDict, total=False):
    preload: List[PreloadOption]
    columns: List[str]
    omit_columns: List[str]
    filters: List[FilterOption]
    sort: List[SortOption]
    limit: int
    offset: int
    customOperators: List[CustomOperator]
    computedColumns: List[ComputedColumn]
    parameters: List[Parameter]
    cursor_forward: str
    cursor_backward: str
    fetch_row_number: str
    vector_search: VectorSearchOption


class HeaderSpecOptions(Options, total=False):
    """Options only available to the header-based (restheadspec) protocol."""

    expand: List[ExpandOption]  # X-Expand
    custom_sql_joins: List[str]  # X-Custom-SQL-Join
    custom_sql_or: List[str]  # X-Custom-SQL-Or
    search_columns: List[str]  # X-SearchCols
    advanced_sql: Dict[str, str]  # X-AdvSQL-{col}
    clean_json: bool  # X-Clean-JSON
    distinct: bool  # X-Distinct
    skip_count: bool  # X-SkipCount
    skip_cache: bool  # X-SkipCache
    pk_row: str  # X-PKRow
    response_format: str  # X-SimpleApi / X-DetailApi / X-Syncfusion
    single_record_as_object: bool  # X-Single-Record-As-Object
    atomic_transaction: bool  # X-Transaction-Atomic
    xfiles: XFiles  # X-Files


class FuncSpecOptions(TypedDict, total=False):
    """Options understood by funcspec endpoints (sent as X-* headers)."""

    filters: List[FilterOption]  # eq+AND -> X-FieldFilter; others X-SearchOp / X-SearchOr (one per column)
    search_filters: Dict[str, str]  # X-SearchFilter-{col}: text ILIKE
    custom_sql_where: str  # X-Custom-SQL-W
    custom_sql_or: str  # X-Custom-SQL-Or
    sort: List[SortOption]  # sent as SQL ORDER BY terms ("col DESC")
    limit: int
    offset: int
    distinct: bool
    skip_count: bool
    skip_cache: bool
    response_format: str  # simple | detail | syncfusion


# Responses are plain dicts: {"success", "data", "metadata"?, "error"?}
APIResponse = Dict[str, Any]
