// Package direct is the table-backed implementation of the lookup stores. SQL is built
// from the configured Schema (table and column names) and Dialect (placeholders, quoting,
// booleans, insert-returning-id); no statement is written per database and no ORM is used.
package direct

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup/dialect"
)

// Runner runs a database operation, reconnecting once when the *sql.DB has been closed.
// procedure.Runner (and procedure.DB) satisfy it.
type Runner interface {
	Run(run func(*sql.DB) error) error
}

// Querier is implemented by *sql.DB and *sql.Tx.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Base is the state shared by every direct store: the runner, dialect, schema and clock.
type Base struct {
	run    Runner
	d      dialect.Dialect
	schema lookup.Schema
	// Now is the clock; tests replace it.
	Now func() time.Time
}

// NewBase creates the shared state. The schema is merged with the defaults and validated.
func NewBase(run Runner, d dialect.Dialect, schema lookup.Schema) (*Base, error) {
	if run == nil {
		return nil, fmt.Errorf("direct: nil runner")
	}
	if d == nil {
		return nil, fmt.Errorf("direct: nil dialect")
	}
	merged := lookup.DefaultSchema().Merge(schema)
	if err := merged.Validate(); err != nil {
		return nil, err
	}
	return &Base{run: run, d: d, schema: merged, Now: time.Now}, nil
}

// Dialect returns the dialect in use.
func (b *Base) Dialect() dialect.Dialect { return b.d }

// do runs fn against the database without a transaction.
func (b *Base) do(fn func(q Querier) error) error {
	return b.run.Run(func(db *sql.DB) error { return fn(db) })
}

// tx runs fn in one transaction; an error rolls back.
func (b *Base) tx(ctx context.Context, fn func(q Querier) error) error {
	return b.run.Run(func(db *sql.DB) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := fn(tx); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	})
}

// tableRef returns the (possibly schema-qualified) physical table name of an entity.
func (b *Base) tableRef(e lookup.Entity) string {
	t := b.schema[e]
	name := t.Name
	if name == "" {
		name = string(e)
	}
	if t.Schema != "" {
		return t.Schema + "." + name
	}
	return name
}

// colName returns the physical column name of a logical column.
func (b *Base) colName(c lookup.Column) string {
	if t, ok := b.schema[c.Entity]; ok {
		if n := t.Columns[c.Name]; n != "" {
			return n
		}
	}
	return c.Name
}

// arg converts a Go value to a bind argument (booleans go through the dialect).
func (b *Base) arg(v any) any {
	if bv, ok := v.(bool); ok {
		return b.d.Bool(bv)
	}
	// Timestamp columns carry no zone: bind every instant as UTC so drivers that send an
	// offset (SQL Server) and ones that drop it agree on the stored wall clock.
	if tv, ok := v.(time.Time); ok {
		return tv.UTC()
	}
	if tp, ok := v.(*time.Time); ok {
		if tp == nil {
			return nil
		}
		return tp.UTC()
	}
	return v
}

// timeDest scans a time column through the dialect, so drivers returning strings work.
type timeDest struct {
	d dialect.Dialect
	v *time.Time
}

func (t timeDest) Scan(src any) error {
	v, err := t.d.ScanTime(src)
	if err != nil {
		return err
	}
	*t.v = v
	return nil
}

type boolDest struct {
	d dialect.Dialect
	v *bool
}

func (t boolDest) Scan(src any) error {
	v, err := t.d.ScanBool(src)
	if err != nil {
		return err
	}
	*t.v = v
	return nil
}

func (b *Base) timeDest(v *time.Time) sql.Scanner { return timeDest{d: b.d, v: v} }
func (b *Base) boolDest(v *bool) sql.Scanner      { return boolDest{d: b.d, v: v} }

// --- query builder --------------------------------------------------------

// builder accumulates bind arguments and renders column references.
type builder struct {
	b       *Base
	args    []any
	aliases map[lookup.Entity]string
	nalias  int
}

func (bl *builder) ph(v any) string {
	bl.args = append(bl.args, bl.b.arg(v))
	return bl.b.d.Placeholder(len(bl.args))
}

// col renders a column; with aliases set (select queries) it is qualified by its table alias.
func (bl *builder) col(c lookup.Column) string {
	name := bl.b.d.Quote(bl.b.colName(c))
	if bl.aliases != nil {
		if a, ok := bl.aliases[c.Entity]; ok {
			return a + "." + name
		}
	}
	return name
}

// Cond renders one boolean condition.
type Cond func(*builder) string

// Eq is `col = value`.
func Eq(c lookup.Column, v any) Cond {
	return func(bl *builder) string { return bl.col(c) + " = " + bl.ph(v) }
}

// EqFold is a case-insensitive `LOWER(col) = value` match (the value is lowered in Go).
func EqFold(c lookup.Column, v string) Cond {
	return func(bl *builder) string { return "LOWER(" + bl.col(c) + ") = " + bl.ph(strings.ToLower(v)) }
}

// Ne is `col <> value`.
func Ne(c lookup.Column, v any) Cond {
	return func(bl *builder) string { return bl.col(c) + " <> " + bl.ph(v) }
}

// Gt is `col > value`.
func Gt(c lookup.Column, v any) Cond {
	return func(bl *builder) string { return bl.col(c) + " > " + bl.ph(v) }
}

// Lt is `col < value`.
func Lt(c lookup.Column, v any) Cond {
	return func(bl *builder) string { return bl.col(c) + " < " + bl.ph(v) }
}

// IsNull is `col IS NULL`.
func IsNull(c lookup.Column) Cond {
	return func(bl *builder) string { return bl.col(c) + " IS NULL" }
}

// EqCol is `a = b` between two columns (join conditions).
func EqCol(a, c lookup.Column) Cond {
	return func(bl *builder) string { return bl.col(a) + " = " + bl.col(c) }
}

// In is `col IN (v...)`; an empty list renders a condition that is never true.
func In(c lookup.Column, vs ...any) Cond {
	return func(bl *builder) string {
		if len(vs) == 0 {
			return "1 = 0"
		}
		ph := make([]string, len(vs))
		for i, v := range vs {
			ph[i] = bl.ph(v)
		}
		return bl.col(c) + " IN (" + strings.Join(ph, ", ") + ")"
	}
}

// InSelect is `col IN (subselect)`; the subselect's arguments share the outer numbering.
func InSelect(c lookup.Column, sub *Select) Cond {
	return func(bl *builder) string { return bl.col(c) + " IN (" + sub.render(bl) + ")" }
}

// Or joins conditions with OR inside parentheses.
func Or(cs ...Cond) Cond { return joinConds("OR", cs) }

// And joins conditions with AND inside parentheses.
func And(cs ...Cond) Cond { return joinConds("AND", cs) }

func joinConds(op string, cs []Cond) Cond {
	return func(bl *builder) string {
		parts := make([]string, len(cs))
		for i, c := range cs {
			parts[i] = c(bl)
		}
		return "(" + strings.Join(parts, " "+op+" ") + ")"
	}
}

func (bl *builder) where(cs []Cond) string {
	if len(cs) == 0 {
		return ""
	}
	parts := make([]string, len(cs))
	for i, c := range cs {
		parts[i] = c(bl)
	}
	return " WHERE " + strings.Join(parts, " AND ")
}

// Stmt is a rendered statement.
type Stmt struct {
	SQL  string
	Args []any
}

// Select builds a SELECT.
type Select struct {
	b     *Base
	from  lookup.Entity
	joins []join
	cols  []lookup.Column
	conds []Cond
	order []lookup.Column
}

type join struct {
	e  lookup.Entity
	on Cond
}

// From starts a SELECT on e.
func (b *Base) From(e lookup.Entity) *Select { return &Select{b: b, from: e} }

// Cols sets the selected columns.
func (s *Select) Cols(cs ...lookup.Column) *Select { s.cols = cs; return s }

// Join adds `JOIN e ON on`.
func (s *Select) Join(e lookup.Entity, on Cond) *Select {
	s.joins = append(s.joins, join{e: e, on: on})
	return s
}

// Where adds AND-ed conditions.
func (s *Select) Where(cs ...Cond) *Select { s.conds = append(s.conds, cs...); return s }

// OrderBy adds ascending order columns.
func (s *Select) OrderBy(cs ...lookup.Column) *Select { s.order = append(s.order, cs...); return s }

// Build renders the statement.
func (s *Select) Build() Stmt {
	bl := &builder{b: s.b}
	sqlText := s.render(bl)
	return Stmt{SQL: sqlText, Args: bl.args}
}

// render writes the select into bl, giving every table a fresh alias so a subselect cannot
// clash with the statement around it.
func (s *Select) render(bl *builder) string {
	saved := bl.aliases
	defer func() { bl.aliases = saved }()
	bl.aliases = map[lookup.Entity]string{}
	alias := func() string { a := fmt.Sprintf("t%d", bl.nalias); bl.nalias++; return a }
	bl.aliases[s.from] = alias()
	for _, j := range s.joins {
		bl.aliases[j.e] = alias()
	}
	sel := make([]string, len(s.cols))
	for i, c := range s.cols {
		sel[i] = bl.col(c)
	}
	var sb strings.Builder
	sb.WriteString("SELECT " + strings.Join(sel, ", "))
	sb.WriteString(" FROM " + s.b.d.Quote(s.b.tableRef(s.from)) + " " + bl.aliases[s.from])
	for _, j := range s.joins {
		sb.WriteString(" JOIN " + s.b.d.Quote(s.b.tableRef(j.e)) + " " + bl.aliases[j.e] + " ON " + j.on(bl))
	}
	sb.WriteString(bl.where(s.conds))
	if len(s.order) > 0 {
		o := make([]string, len(s.order))
		for i, c := range s.order {
			o[i] = bl.col(c)
		}
		sb.WriteString(" ORDER BY " + strings.Join(o, ", "))
	}
	return sb.String()
}

// QueryRow runs the select and scans the first row into dest.
func (s *Select) QueryRow(ctx context.Context, q Querier, dest ...any) error {
	st := s.Build()
	return q.QueryRowContext(ctx, st.SQL, st.Args...).Scan(dest...) //nolint:gosec // G701: identifiers come from the validated schema, values are bound parameters
}

// Query runs the select.
func (s *Select) Query(ctx context.Context, q Querier) (*sql.Rows, error) {
	st := s.Build()
	return q.QueryContext(ctx, st.SQL, st.Args...) //nolint:gosec // G701: identifiers come from the validated schema, values are bound parameters
}

// Exists reports whether the select returns at least one row.
func (s *Select) Exists(ctx context.Context, q Querier) (bool, error) {
	s.cols = []lookup.Column{s.firstCol()}
	rows, err := s.Query(ctx, q)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	ok := rows.Next()
	return ok, rows.Err()
}

func (s *Select) firstCol() lookup.Column {
	if len(s.cols) > 0 {
		return s.cols[0]
	}
	return lookup.Column{Entity: s.from, Name: lookup.FirstColumn(s.from)}
}

// Assignment is one `col = value` of an UPDATE or INSERT.
type Assignment struct {
	Col lookup.Column
	Val any
}

// Set builds an Assignment.
func Set(c lookup.Column, v any) Assignment { return Assignment{Col: c, Val: v} }

// Update builds an UPDATE.
type Update struct {
	b     *Base
	e     lookup.Entity
	sets  []Assignment
	conds []Cond
}

// Update starts an UPDATE of e.
func (b *Base) Update(e lookup.Entity) *Update { return &Update{b: b, e: e} }

// Set adds assignments.
func (u *Update) Set(as ...Assignment) *Update { u.sets = append(u.sets, as...); return u }

// Where adds AND-ed conditions.
func (u *Update) Where(cs ...Cond) *Update { u.conds = append(u.conds, cs...); return u }

// Exec runs the update and returns the affected row count.
func (u *Update) Exec(ctx context.Context, q Querier) (int64, error) {
	bl := &builder{b: u.b}
	set := make([]string, len(u.sets))
	for i, a := range u.sets {
		set[i] = bl.col(a.Col) + " = " + bl.ph(a.Val)
	}
	sqlText := "UPDATE " + u.b.d.Quote(u.b.tableRef(u.e)) + " SET " + strings.Join(set, ", ") + bl.where(u.conds)
	res, err := q.ExecContext(ctx, sqlText, bl.args...) //nolint:gosec // G701: identifiers come from the validated schema, values are bound parameters
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Delete builds a DELETE.
type Delete struct {
	b     *Base
	e     lookup.Entity
	conds []Cond
}

// Delete starts a DELETE on e.
func (b *Base) Delete(e lookup.Entity) *Delete { return &Delete{b: b, e: e} }

// Where adds AND-ed conditions.
func (d *Delete) Where(cs ...Cond) *Delete { d.conds = append(d.conds, cs...); return d }

// Exec runs the delete and returns the affected row count.
func (d *Delete) Exec(ctx context.Context, q Querier) (int64, error) {
	bl := &builder{b: d.b}
	sqlText := "DELETE FROM " + d.b.d.Quote(d.b.tableRef(d.e)) + bl.where(d.conds)
	res, err := q.ExecContext(ctx, sqlText, bl.args...) //nolint:gosec // G701: identifiers come from the validated schema, values are bound parameters
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Insert builds an INSERT.
type Insert struct {
	b    *Base
	e    lookup.Entity
	sets []Assignment
}

// Insert starts an INSERT into e.
func (b *Base) Insert(e lookup.Entity) *Insert { return &Insert{b: b, e: e} }

// Set adds assignments.
func (i *Insert) Set(as ...Assignment) *Insert { i.sets = append(i.sets, as...); return i }

func (i *Insert) colsAndArgs() (cols []string, args []any) {
	cols = make([]string, len(i.sets))
	args = make([]any, len(i.sets))
	for n, a := range i.sets {
		cols[n] = i.b.colName(a.Col)
		args[n] = i.b.arg(a.Val)
	}
	return cols, args
}

// Exec runs the insert.
func (i *Insert) Exec(ctx context.Context, q Querier) error {
	cols, args := i.colsAndArgs()
	ph := make([]string, len(cols))
	qc := make([]string, len(cols))
	for n, c := range cols {
		qc[n] = i.b.d.Quote(c)
		ph[n] = i.b.d.Placeholder(n + 1)
	}
	sqlText := "INSERT INTO " + i.b.d.Quote(i.b.tableRef(i.e)) + " (" + strings.Join(qc, ", ") + ") VALUES (" + strings.Join(ph, ", ") + ")"
	_, err := q.ExecContext(ctx, sqlText, args...) //nolint:gosec // G701: identifiers come from the validated schema, values are bound parameters
	return err
}

// ExecID runs the insert and returns the generated value of idCol, using the dialect's
// insert-returning-id strategy.
func (i *Insert) ExecID(ctx context.Context, q Querier, idCol lookup.Column) (int64, error) {
	cols, args := i.colsAndArgs()
	ins := i.b.d.InsertReturningID(i.b.tableRef(i.e), cols, i.b.colName(idCol))
	return ins.Run(ctx, q, args...)
}
