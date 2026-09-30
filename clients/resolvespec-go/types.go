// Package resolvespec is a client for ResolveSpec (JSON body) and FunctionSpec endpoints.
package resolvespec

import "encoding/json"

// FilterOption mirrors common.FilterOption. Operator: eq neq gt gte lt lte like ilike in
// contains startswith endswith between between_inclusive is_null is_not_null.
type FilterOption struct {
	Column        string `json:"column"`
	Operator      string `json:"operator"`
	Value         any    `json:"value"`
	LogicOperator string `json:"logic_operator,omitempty"` // AND | OR
}

type SortOption struct {
	Column    string `json:"column"`
	Direction string `json:"direction"` // asc | desc
}

type Parameter struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Sequence int    `json:"sequence,omitempty"`
}

type CustomOperator struct {
	Name string `json:"name"`
	SQL  string `json:"sql"`
}

type ComputedColumn struct {
	Name       string `json:"name"`
	Expression string `json:"expression"`
}

type PreloadOption struct {
	Relation          string            `json:"relation,omitempty"`
	TableName         string            `json:"table_name,omitempty"`
	Columns           []string          `json:"columns,omitempty"`
	OmitColumns       []string          `json:"omit_columns,omitempty"`
	Sort              []SortOption      `json:"sort,omitempty"`
	Filters           []FilterOption    `json:"filters,omitempty"`
	Where             string            `json:"where,omitempty"`
	Limit             *int              `json:"limit,omitempty"`
	Offset            *int              `json:"offset,omitempty"`
	Updateable        *bool             `json:"updateable,omitempty"`
	ComputedQL        map[string]string `json:"computed_ql,omitempty"`
	Recursive         bool              `json:"recursive,omitempty"`
	PrimaryKey        string            `json:"primary_key,omitempty"`
	RelatedKey        string            `json:"related_key,omitempty"`
	ForeignKey        string            `json:"foreign_key,omitempty"`
	RecursiveChildKey string            `json:"recursive_child_key,omitempty"`
	SQLJoins          []string          `json:"sql_joins,omitempty"`
	JoinAliases       []string          `json:"join_aliases,omitempty"`
}

type VectorSearchOption struct {
	Column    string    `json:"column"`
	Vector    []float64 `json:"vector"`
	Metric    string    `json:"metric,omitempty"` // l2 (default) | cosine | ip
	As        string    `json:"as,omitempty"`     // distance alias, default _distance
	Direction string    `json:"direction,omitempty"`
}

// Options is the ResolveSpec request options object.
type Options struct {
	Preload         []PreloadOption     `json:"preload,omitempty"`
	Columns         []string            `json:"columns,omitempty"`
	OmitColumns     []string            `json:"omit_columns,omitempty"`
	Filters         []FilterOption      `json:"filters,omitempty"`
	Sort            []SortOption        `json:"sort,omitempty"`
	Limit           *int                `json:"limit,omitempty"`
	Offset          *int                `json:"offset,omitempty"`
	CustomOperators []CustomOperator    `json:"customOperators,omitempty"`
	ComputedColumns []ComputedColumn    `json:"computedColumns,omitempty"`
	Parameters      []Parameter         `json:"parameters,omitempty"`
	CursorForward   string              `json:"cursor_forward,omitempty"`
	CursorBackward  string              `json:"cursor_backward,omitempty"`
	FetchRowNumber  string              `json:"fetch_row_number,omitempty"`
	VectorSearch    *VectorSearchOption `json:"vector_search,omitempty"`
}

// Metadata of a list response.
type Metadata struct {
	Total    int64 `json:"total"`
	Count    int64 `json:"count"`
	Filtered int64 `json:"filtered"`
	Limit    int   `json:"limit"`
	Offset   int   `json:"offset"`
}

// Response is the ResolveSpec envelope. Data is left raw for the caller to decode.
type Response struct {
	Success  bool            `json:"success"`
	Data     json.RawMessage `json:"data"`
	Metadata *Metadata       `json:"metadata,omitempty"`
	Error    *APIError       `json:"error,omitempty"`
}

// Decode unmarshals Data into v.
func (r *Response) Decode(v any) error { return json.Unmarshal(r.Data, v) }

// Int returns a pointer to n, for optional Options fields.
func Int(n int) *int { return &n }

// Bool returns a pointer to b.
func Bool(b bool) *bool { return &b }
