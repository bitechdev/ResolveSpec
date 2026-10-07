package funcspec

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/uptrace/bun/driver/sqliteshim"
)

const joinedBaseSQL = "SELECT p.id, p.name, c.name AS country_name FROM province_state p LEFT JOIN country c ON c.id = p.rid_country"

// funcspec filters are appended to author-written SQL with no model, so columns
// are used verbatim: clients disambiguate by sending "alias.column", and the
// dot must survive ValidSQL and every filter path.
func TestApplyFilters_QualifiedColumnsPreservedWithJoin(t *testing.T) {
	h := NewHandler(&MockDatabase{})

	cases := []struct {
		name   string
		params *RequestParameters
		want   string
	}{
		{"field filter", &RequestParameters{FieldFilters: map[string]string{"p.name": "abc"}}, "p.name = abc"},
		{"search filter", &RequestParameters{SearchFilters: map[string]string{"p.name": "abc"}}, "CAST(p.name AS TEXT) ILIKE '%abc%'"},
		{"search op eq", &RequestParameters{SearchOps: map[string]FilterOperator{"p.name": {Operator: "eq", Value: "abc", Logic: "AND"}}}, "p.name = 'abc'"},
		{"search op contains", &RequestParameters{SearchOps: map[string]FilterOperator{"p.name": {Operator: "contains", Value: "abc", Logic: "AND"}}}, "CAST(p.name AS TEXT) ILIKE '%abc%'"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := h.ApplyFilters(joinedBaseSQL, c.params)
			if !strings.Contains(got, c.want) {
				t.Fatalf("SQL %q does not contain %q", got, c.want)
			}
			if !strings.Contains(got, "LEFT JOIN country") || !strings.Contains(got, " WHERE ") {
				t.Fatalf("join/where lost: %q", got)
			}
		})
	}
}

func TestApplyFilters_WithAndWithoutJoin_RealQuery(t *testing.T) {
	sqldb, err := sql.Open(sqliteshim.ShimName, "file:funcspecjoin?mode=memory&cache=private")
	if err != nil {
		t.Fatal(err)
	}
	defer sqldb.Close()
	for _, stmt := range []string{
		"CREATE TABLE country (id INTEGER PRIMARY KEY, name TEXT)",
		"CREATE TABLE province_state (id INTEGER PRIMARY KEY, name TEXT, rid_country INTEGER)",
		"INSERT INTO country VALUES (1,'Abcland'),(2,'Other')",
		"INSERT INTO province_state VALUES (1,'abc one',1),(2,'xyz two',1),(3,'nope',2)",
	} {
		if _, err := sqldb.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}

	h := NewHandler(&MockDatabase{})
	count := func(base string, params *RequestParameters) (int, error) {
		rows, err := sqldb.Query(h.ApplyFilters(base, params))
		if err != nil {
			return 0, err
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			n++
		}
		return n, rows.Err()
	}

	const noJoin = "SELECT p.id, p.name FROM province_state p"
	params := func(col string) *RequestParameters {
		return &RequestParameters{SearchOps: map[string]FilterOperator{col: {Operator: "eq", Value: "nope", Logic: "AND"}}}
	}

	if n, err := count(noJoin, params("name")); err != nil || n != 1 {
		t.Fatalf("no join, unqualified: n=%d err=%v", n, err)
	}
	if n, err := count(joinedBaseSQL, params("p.name")); err != nil || n != 1 {
		t.Fatalf("join, qualified: n=%d err=%v", n, err)
	}
	if n, err := count(joinedBaseSQL, params("c.name")); err != nil || n != 0 {
		t.Fatalf("join, joined-table column: n=%d err=%v", n, err)
	}
	// Documented limit: with a join in the author's SQL, an unqualified shared
	// column is ambiguous and the client must send "alias.column".
	if _, err := count(joinedBaseSQL, params("name")); err == nil || !strings.Contains(strings.ToLower(err.Error()), "ambiguous") {
		t.Fatalf("expected ambiguous error for unqualified shared column, got %v", err)
	}
}
