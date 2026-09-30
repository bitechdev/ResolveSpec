package restheadspec

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

func TestQueryTotalCacheKeyIncludesRecordID(t *testing.T) {
	list := buildExtendedQueryCacheKey("items", "", nil, nil, "", "", nil, nil, false, "", "")
	one := buildExtendedQueryCacheKey("items", "7", nil, nil, "", "", nil, nil, false, "", "")
	other := buildExtendedQueryCacheKey("items", "8", nil, nil, "", "", nil, nil, false, "", "")
	if list == one || one == other {
		t.Fatal("list, id 7 and id 8 must not share a cache key")
	}
	if one != buildExtendedQueryCacheKey("items", "7", nil, nil, "", "", nil, nil, false, "", "") {
		t.Fatal("the key must be stable for the same query")
	}
}

// A read by id counts 1 row; before the id was part of the key, that total was
// cached under the list query's key and served as the list total for 2 minutes.
func TestReadByIDDoesNotPoisonListTotal(t *testing.T) {
	resetTotalCache(t)
	h, mock := newBunHarness(t)
	cols := []string{"id", "name"}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows(cols).AddRow(7, "a"))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectCommit()
	// The list must run its own count query, not reuse the id read's total.
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows(cols).AddRow(7, "a").AddRow(8, "b"))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectCommit()

	read := func(id string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		w, _ := common.WrapHTTPRequest(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		h.handleRead(itemCtx(t), w, id, ExtendedRequestOptions{})
		return rec
	}
	if rec := read("7"); rec.Code != http.StatusOK {
		t.Fatalf("read by id: status %d body %s", rec.Code, rec.Body)
	}
	if rec := read(""); rec.Code != http.StatusOK {
		t.Fatalf("list: status %d body %s", rec.Code, rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("the list must run its own count query: %v", err)
	}
}
