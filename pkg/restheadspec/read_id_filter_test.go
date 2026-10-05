package restheadspec

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
)

// readCapturingSQL runs handleRead and returns every SELECT it issued.
func readCapturingSQL(t *testing.T, id string, options ExtendedRequestOptions) []string {
	queries, _ := readCapturingSQLAndBody(t, id, options)
	return queries
}

// readCapturingSQLAndBody is readCapturingSQL that also returns the response body.
// The mocked row carries the requested id so the body can be checked against it.
func readCapturingSQLAndBody(t *testing.T, id string, options ExtendedRequestOptions) ([]string, string) {
	t.Helper()
	resetTotalCache(t)
	var queries []string
	matcher := sqlmock.QueryMatcherFunc(func(_, actual string) error {
		queries = append(queries, actual)
		return nil
	})
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	h := NewHandler(database.NewBunAdapter(bun.NewDB(sqlDB, pgdialect.New())), modelregistry.NewModelRegistry())

	rowID, err := strconv.Atoi(id)
	if err != nil {
		rowID = 7
	}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(rowID, "a"))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectCommit()

	rec := httptest.NewRecorder()
	w, _ := common.WrapHTTPRequest(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	h.handleRead(itemCtx(t), w, id, options)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	return queries, rec.Body.String()
}

func TestReadByIDIgnoresLimitOffsetAndCursor(t *testing.T) {
	limit, offset := 50, 10
	queries := readCapturingSQL(t, "7", ExtendedRequestOptions{
		RequestOptions: common.RequestOptions{
			Limit:  &limit,
			Offset: &offset,
		},
	})
	last := queries[len(queries)-1]
	if !strings.Contains(last, "LIMIT 1") || strings.Contains(last, "OFFSET") {
		t.Fatalf("read by id must be LIMIT 1 with no OFFSET: %s", last)
	}
}

func TestReadWithoutIDKeepsRequestedLimit(t *testing.T) {
	limit := 50
	queries := readCapturingSQL(t, "", ExtendedRequestOptions{
		RequestOptions: common.RequestOptions{Limit: &limit},
	})
	if last := queries[len(queries)-1]; !strings.Contains(last, "LIMIT 50") {
		t.Fatalf("list read must keep its limit: %s", last)
	}
}

// topLevelOr reports whether the WHERE clause has an OR outside any parentheses,
// i.e. one that would let rows bypass the AND-ed primary key condition.
func topLevelOr(sql string) bool {
	where := sql[strings.Index(sql, "WHERE")+len("WHERE"):]
	depth, inStr := 0, false
	for i := 0; i < len(where); i++ {
		switch c := where[i]; {
		case c == '\'':
			inStr = !inStr
		case inStr:
		case c == '(':
			depth++
		case c == ')':
			depth--
		case depth == 0 && strings.HasPrefix(where[i:], " OR "):
			return true
		}
	}
	return false
}

func TestReadByIDCustomSQLOrCannotEscapePrimaryKey(t *testing.T) {
	queries := readCapturingSQL(t, "7", ExtendedRequestOptions{
		RequestOptions: common.RequestOptions{
			Filters: []common.FilterOption{{Column: "name", Operator: "eq", Value: "a"}},
		},
		CustomSQLOr: "name = 'x'",
	})
	last := queries[len(queries)-1]
	if !strings.Contains(last, `"id" = '7'`) && !strings.Contains(last, `"id" = 7`) {
		t.Fatalf("primary key filter missing: %s", last)
	}
	if topLevelOr(last) {
		t.Fatalf("OR escapes the primary key filter: %s", last)
	}
}

func TestReadByIDFiltersAndReturnsRequestedRecord(t *testing.T) {
	queries, body := readCapturingSQLAndBody(t, "42", ExtendedRequestOptions{})
	last := queries[len(queries)-1]
	if !strings.Contains(last, `"items"."id" = '42'`) && !strings.Contains(last, `"items"."id" = 42`) {
		t.Fatalf("query must filter the primary key to 42: %s", last)
	}
	if strings.Contains(last, "= 7") || strings.Contains(last, "= '7'") {
		t.Fatalf("query filters a different id: %s", last)
	}
	// every query that touches rows (count and select) must carry the id filter
	for _, q := range queries {
		if strings.Contains(q, "FROM") && !strings.Contains(q, "42") {
			t.Fatalf("query without the id filter: %s", q)
		}
	}
	var rows []struct {
		ID int `json:"id"`
	}
	data := body
	if i := strings.Index(body, `"data"`); i >= 0 {
		data = body[i+len(`"data"`):]
	}
	if i := strings.Index(data, "["); i >= 0 {
		data = data[i:]
	}
	dec := json.NewDecoder(strings.NewReader(data))
	if err := dec.Decode(&rows); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	if len(rows) != 1 || rows[0].ID != 42 {
		t.Fatalf("response must contain exactly the record with id 42: %s", body)
	}
}
