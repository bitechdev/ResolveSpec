package resolvemcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

// Parameter types a function may declare.
const (
	ParamString  = "string"
	ParamInteger = "integer"
	ParamNumber  = "number"
	ParamBoolean = "boolean"
	ParamObject  = "object"
	ParamArray   = "array"
)

// FunctionParam declares one argument of a registered function.
type FunctionParam struct {
	Name        string
	Type        string // one of the Param* constants
	Description string
	Required    bool
}

// FunctionFunc is a Go function callable through call_function. tx is the transaction the call
// runs in (OnTxBegin already fired on it); args are validated against the declared params.
type FunctionFunc func(ctx context.Context, tx common.Database, args map[string]any) (any, error)

// Function is a callable registered with Handler.RegisterFunction. Exactly one of Handler (a Go
// callback) and Procedure (a SQL function called by name) is set.
type Function struct {
	Name        string
	Description string
	Params      []FunctionParam

	// Handler is the Go callback.
	Handler FunctionFunc
	// Procedure is the SQL function to call, optionally schema-qualified. Declared params are
	// passed positionally in declaration order; an omitted optional param is NULL. Rows
	// returned are the result.
	Procedure string

	// Authorize, when set, decides per caller whether the function is listed and callable.
	// Return nil to allow.
	Authorize func(ctx context.Context) error
}

var (
	functionNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
	procedureRe    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)
	paramNameRe    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
)

type functionRegistry struct {
	mu    sync.RWMutex
	funcs map[string]Function
}

// RegisterFunction makes f callable through the call_function meta tool. Only registered
// functions are callable; there is no way to reach an arbitrary SQL function.
func (h *Handler) RegisterFunction(f Function) error {
	if !functionNameRe.MatchString(f.Name) {
		return fmt.Errorf("resolvemcp: invalid function name %q", f.Name)
	}
	if (f.Handler == nil) == (f.Procedure == "") {
		return fmt.Errorf("resolvemcp: function %q needs exactly one of Handler and Procedure", f.Name)
	}
	if f.Procedure != "" && !procedureRe.MatchString(f.Procedure) {
		return fmt.Errorf("resolvemcp: invalid procedure name %q", f.Procedure)
	}
	seen := map[string]bool{}
	for _, p := range f.Params {
		if !paramNameRe.MatchString(p.Name) || seen[p.Name] {
			return fmt.Errorf("resolvemcp: function %q: invalid or duplicate param %q", f.Name, p.Name)
		}
		seen[p.Name] = true
		switch p.Type {
		case ParamString, ParamInteger, ParamNumber, ParamBoolean, ParamObject, ParamArray:
		default:
			return fmt.Errorf("resolvemcp: function %q param %q: unknown type %q", f.Name, p.Name, p.Type)
		}
	}
	h.functions.mu.Lock()
	defer h.functions.mu.Unlock()
	if h.functions.funcs == nil {
		h.functions.funcs = map[string]Function{}
	}
	if _, dup := h.functions.funcs[f.Name]; dup {
		return fmt.Errorf("resolvemcp: function %q already registered", f.Name)
	}
	h.functions.funcs[f.Name] = f
	return nil
}

func (h *Handler) function(name string) (Function, bool) {
	h.functions.mu.RLock()
	defer h.functions.mu.RUnlock()
	f, ok := h.functions.funcs[name]
	return f, ok
}

// visibleFunctions returns the functions the caller may call, sorted by name.
func (h *Handler) visibleFunctions(ctx context.Context) []Function {
	h.functions.mu.RLock()
	out := make([]Function, 0, len(h.functions.funcs))
	for _, f := range h.functions.funcs {
		out = append(out, f)
	}
	h.functions.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	visible := out[:0]
	for _, f := range out {
		if f.Authorize == nil || f.Authorize(ctx) == nil {
			visible = append(visible, f)
		}
	}
	return visible
}

// validateArgs checks args against the declared params: required present, no undeclared names,
// types match. The returned map is what the function receives.
func validateArgs(params []FunctionParam, args map[string]any) (map[string]any, error) {
	decl := make(map[string]FunctionParam, len(params))
	for _, p := range params {
		decl[p.Name] = p
	}
	for name := range args {
		if _, ok := decl[name]; !ok {
			return nil, invalidArg("unknown argument %q", truncate(name))
		}
	}
	out := make(map[string]any, len(args))
	for _, p := range params {
		v, ok := args[p.Name]
		if !ok || v == nil {
			if p.Required {
				return nil, invalidArg("missing required argument %q", p.Name)
			}
			continue
		}
		if !argMatches(p.Type, v) {
			return nil, invalidArg("argument %q must be %s", p.Name, p.Type)
		}
		out[p.Name] = v
	}
	return out, nil
}

func truncate(s string) string {
	if len(s) > maxKeyEcho {
		return s[:maxKeyEcho] + "..."
	}
	return s
}

func argMatches(typ string, v any) bool {
	switch typ {
	case ParamString:
		_, ok := v.(string)
		return ok
	case ParamBoolean:
		_, ok := v.(bool)
		return ok
	case ParamNumber:
		return isNumber(v)
	case ParamInteger:
		switch n := v.(type) {
		case float64:
			return n == math.Trunc(n) && !math.IsInf(n, 0)
		case int, int64:
			return true
		}
		return false
	case ParamObject:
		_, ok := v.(map[string]any)
		return ok
	case ParamArray:
		_, ok := v.([]any)
		return ok
	}
	return false
}

func isNumber(v any) bool {
	switch v.(type) {
	case float64, int, int64:
		return true
	}
	return false
}

// callProcedure runs f.Procedure on tx.
func callProcedure(ctx context.Context, tx common.Database, f Function, args map[string]any) (any, error) {
	placeholders := make([]string, len(f.Params))
	values := make([]any, len(f.Params))
	for i, p := range f.Params {
		ph := fmt.Sprintf("$%d", i+1)
		if p.Type == ParamObject || p.Type == ParamArray {
			ph += "::jsonb"
		}
		if v, ok := args[p.Name]; !ok {
			values[i] = nil
		} else if p.Type == ParamObject || p.Type == ParamArray {
			b, err := json.Marshal(v)
			if err != nil {
				return nil, invalidArg("argument %q cannot be encoded", p.Name)
			}
			values[i] = string(b)
		} else {
			values[i] = v
		}
		placeholders[i] = ph
	}
	var rows []map[string]any
	query := fmt.Sprintf("SELECT * FROM %s(%s)", f.Procedure, strings.Join(placeholders, ", "))
	if err := tx.Query(ctx, &rows, query, values...); err != nil {
		return nil, err
	}
	return rows, nil
}

// executeCall validates and runs a registered function in a transaction: BeforeHandle (auth and
// rules), Authorize, then OnTxBegin, BeforeCall, the function, AfterCall.
func (h *Handler) executeCall(ctx context.Context, name string, rawArgs map[string]any) (_ any, retErr error) {
	defer recoverPanic(&retErr)
	ctx, cancel := h.callContext(ctx)
	defer cancel()

	f, ok := h.function(name)
	if !ok {
		return nil, invalidArg("unknown function %q", truncate(name))
	}
	hookCtx := &HookContext{Context: ctx, Handler: h, Entity: name, Operation: "call_function", Tx: h.db}
	if err := h.hooks.Execute(BeforeHandle, hookCtx); err != nil {
		return nil, err
	}
	// Same answer for "not authorized" and "does not exist" so names cannot be probed.
	if f.Authorize != nil && f.Authorize(ctx) != nil {
		return nil, invalidArg("unknown function %q", truncate(name))
	}
	args, err := validateArgs(f.Params, rawArgs)
	if err != nil {
		return nil, err
	}
	hookCtx.Data = args

	var result any
	err = h.runInTx(ctx, hookCtx, func(tx common.Database) error {
		if err := h.hooks.Execute(BeforeCall, hookCtx); err != nil {
			return err
		}
		if m, ok := hookCtx.Data.(map[string]any); ok {
			args = m
		}
		var err error
		if f.Handler != nil {
			result, err = f.Handler(ctx, tx, args)
		} else {
			result, err = callProcedure(ctx, tx, f, args)
		}
		if err != nil {
			return err
		}
		hookCtx.Result = result
		return h.hooks.Execute(AfterCall, hookCtx)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
