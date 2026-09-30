package resolvespec

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// FuncSpecOptions are sent to funcspec endpoints as X-* headers.
//
// Server behaviour (pkg/funcspec): Sort is inserted raw into ORDER BY (so it is sent as SQL
// terms); only one search operator per column is kept; values starting with "ZIP_" or "__"
// are base64-decoded by the server, so such plaintext values cannot be sent faithfully.
type FuncSpecOptions struct {
	Filters        []FilterOption    // eq+AND -> X-FieldFilter; others X-SearchOp / X-SearchOr
	SearchFilters  map[string]string // X-SearchFilter-{col}: text ILIKE
	CustomSQLWhere string            // X-Custom-SQL-W
	CustomSQLOr    string            // X-Custom-SQL-Or
	Sort           []SortOption
	Limit          *int
	Offset         *int
	Distinct       *bool
	SkipCount      *bool
	SkipCache      *bool
	ResponseFormat string // simple | detail | syncfusion
}

// Params are query-string values. Slice values are sent as repeated keys (server: IN filter).
type Params map[string]any

// FuncSpecClient calls user-defined SQL endpoints. Routes are defined by the server app.
type FuncSpecClient struct{ cfg config }

func NewFuncSpecClient(baseURL string, opts ...Option) *FuncSpecClient {
	return &FuncSpecClient{cfg: newConfig(baseURL, opts)}
}

var operatorMap = map[string]string{
	"eq": "equals", "neq": "notequals", "gt": "greaterthan", "gte": "greaterthanorequal",
	"lt": "lessthan", "lte": "lessthanorequal", "like": "contains", "ilike": "contains",
	"contains": "contains", "startswith": "beginswith", "endswith": "endswith", "in": "in",
	"between": "between", "between_inclusive": "betweeninclusive",
	"is_null": "empty", "is_not_null": "notempty",
}

func scalar(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		return strconv.FormatBool(x)
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	}
	return fmt.Sprint(v)
}

func filterValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []string:
		return strings.Join(x, ",")
	case []int:
		parts := make([]string, len(x))
		for i, n := range x {
			parts[i] = strconv.Itoa(n)
		}
		return strings.Join(parts, ",")
	case []any:
		parts := make([]string, len(x))
		for i, n := range x {
			parts[i] = scalar(n)
		}
		return strings.Join(parts, ",")
	}
	return scalar(v)
}

// EncodeHeaderValue base64-encodes (UTF-8) with the ZIP_ prefix.
func EncodeHeaderValue(v string) string { return "ZIP_" + base64.StdEncoding.EncodeToString([]byte(v)) }

// DecodeHeaderValue decodes a value that may carry a ZIP_ or __ prefix (nested allowed).
func DecodeHeaderValue(v string) string {
	for _, p := range []string{"ZIP_", "__"} {
		if strings.HasPrefix(v, p) {
			b64 := strings.NewReplacer("\n", "", "\r", "", " ", "").Replace(v[len(p):])
			for len(b64)%4 != 0 {
				b64 += "="
			}
			raw, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				return v
			}
			return DecodeHeaderValue(string(raw))
		}
	}
	return v
}

// safe encodes values that are unsafe as raw header/query text (non-ASCII, control chars, edge spaces).
func safe(v string) string {
	if v != strings.TrimSpace(v) {
		return EncodeHeaderValue(v)
	}
	for _, r := range v {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) {
			return EncodeHeaderValue(v)
		}
	}
	return v
}

// BuildHeaders builds the X-* headers understood by funcspec.ParseParameters.
func BuildHeaders(o *FuncSpecOptions) map[string]string {
	h := map[string]string{}
	if o == nil {
		return h
	}
	for _, f := range o.Filters {
		logic := f.LogicOperator
		if logic == "" {
			logic = "AND"
		}
		v := safe(filterValue(f.Value))
		if f.Operator == "eq" && logic == "AND" {
			h["X-FieldFilter-"+f.Column] = v
			continue
		}
		op := operatorMap[f.Operator]
		if op == "" {
			op = f.Operator
		}
		kind := "X-SearchOp"
		if logic == "OR" {
			kind = "X-SearchOr"
		}
		h[kind+"-"+op+"-"+f.Column] = v
	}
	for col, text := range o.SearchFilters {
		h["X-SearchFilter-"+col] = safe(text)
	}
	if o.CustomSQLWhere != "" {
		h["X-Custom-SQL-W"] = safe(o.CustomSQLWhere)
	}
	if o.CustomSQLOr != "" {
		h["X-Custom-SQL-Or"] = safe(o.CustomSQLOr)
	}
	if len(o.Sort) > 0 {
		terms := make([]string, len(o.Sort))
		for i, s := range o.Sort {
			dir := "ASC"
			if strings.EqualFold(s.Direction, "desc") {
				dir = "DESC"
			}
			terms[i] = s.Column + " " + dir // funcspec puts this verbatim into ORDER BY
		}
		h["X-Sort"] = safe(strings.Join(terms, ","))
	}
	if o.Limit != nil {
		h["X-Limit"] = strconv.Itoa(*o.Limit)
	}
	if o.Offset != nil {
		h["X-Offset"] = strconv.Itoa(*o.Offset)
	}
	for name, v := range map[string]*bool{"X-Distinct": o.Distinct, "X-SkipCount": o.SkipCount, "X-SkipCache": o.SkipCache} {
		if v != nil {
			h[name] = strconv.FormatBool(*v)
		}
	}
	switch o.ResponseFormat {
	case "simple":
		h["X-SimpleApi"] = "true"
	case "detail":
		h["X-DetailApi"] = "true"
	case "syncfusion":
		h["X-Syncfusion"] = "true"
	}
	return h
}

// BuildQuery builds query-string values: bools -> true/false, slices -> repeated keys, nil skipped.
func BuildQuery(p Params) url.Values {
	q := url.Values{}
	for k, v := range p {
		switch x := v.(type) {
		case nil:
		case []string:
			for _, e := range x {
				q.Add(k, safe(e))
			}
		case []int:
			for _, e := range x {
				q.Add(k, strconv.Itoa(e))
			}
		case []any:
			for _, e := range x {
				q.Add(k, safe(scalar(e)))
			}
		default:
			q.Add(k, safe(scalar(v)))
		}
	}
	return q
}

var contentRange = regexp.MustCompile(`(\d+)-(\d+)/(\d+)`)

func metadata(h http.Header, o *FuncSpecOptions) *Metadata {
	m := &Metadata{}
	if g := contentRange.FindStringSubmatch(h.Get("Content-Range")); g != nil {
		start, _ := strconv.ParseInt(g[1], 10, 64)
		end, _ := strconv.ParseInt(g[2], 10, 64)
		total, _ := strconv.ParseInt(g[3], 10, 64)
		m.Total, m.Count, m.Filtered, m.Offset = total, end-start, total, int(start)
	}
	if o != nil && o.Limit != nil {
		m.Limit = *o.Limit
	}
	return m
}

func (c *FuncSpecClient) call(ctx context.Context, method, path string, p Params, o *FuncSpecOptions, withMeta bool) (*Response, error) {
	u := c.cfg.baseURL + "/" + strings.TrimLeft(path, "/")
	if q := BuildQuery(p); len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := c.cfg.newRequest(ctx, strings.ToUpper(method), u, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range BuildHeaders(o) {
		req.Header.Set(k, v)
	}
	resp, b, err := c.cfg.do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 { // 206 is success
		return nil, errorFrom(resp.StatusCode, b)
	}
	out := &Response{Success: true, Data: json.RawMessage(b)}
	if len(b) == 0 {
		out.Data = json.RawMessage("null")
	}
	if withMeta {
		out.Metadata = metadata(resp.Header, o)
	}
	return out, nil
}

// Query calls a single-record endpoint (SqlQuery). Data is the row object.
func (c *FuncSpecClient) Query(ctx context.Context, path string, p Params, o *FuncSpecOptions) (*Response, error) {
	return c.call(ctx, http.MethodGet, path, p, o, false)
}

// QueryList calls a list endpoint (SqlQueryList). Metadata comes from Content-Range.
func (c *FuncSpecClient) QueryList(ctx context.Context, path string, p Params, o *FuncSpecOptions) (*Response, error) {
	return c.call(ctx, http.MethodGet, path, p, o, true)
}

// Do is like Query/QueryList with an explicit HTTP method (routes are app-defined).
func (c *FuncSpecClient) Do(ctx context.Context, method, path string, p Params, o *FuncSpecOptions, list bool) (*Response, error) {
	return c.call(ctx, method, path, p, o, list)
}
