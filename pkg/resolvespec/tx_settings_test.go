package resolvespec

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/security"
)

// stubProvider satisfies security.SecurityProvider; its methods are never called
// because the model rules are public and no rules are loaded for this test.
type stubProvider struct{ security.SecurityProvider }

func TestSecurityHooksStampTxSettingsFirstOnEveryTx(t *testing.T) {
	h, mock, _ := newDeleteHarness(t)
	list, err := security.NewSecurityList(stubProvider{})
	if err != nil {
		t.Fatal(err)
	}
	list.SetTxSettings(func(security.SecurityContext) (map[string]string, error) {
		return map[string]string{"app.user_id": "7"}, nil
	})
	RegisterSecurityHooks(h, list)

	mock.ExpectBegin()
	mock.ExpectExec(`set_config\('app\.user_id'`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, "a"))
	mock.ExpectExec(`DELETE FROM`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	rec := httptest.NewRecorder()
	w, _ := common.WrapHTTPRequest(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	base, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	base = context.WithValue(base, security.UserContextKey, &security.UserContext{UserID: 7, UserName: "u"})
	ctx := WithRequestData(base, "public", "items", "items", &delItem{}, &delItem{})
	h.handleDelete(ctx, w, "7", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSecurityHooksTxSettingsErrorRollsBack(t *testing.T) {
	h, mock, _ := newDeleteHarness(t)
	list, _ := security.NewSecurityList(stubProvider{})
	list.SetTxSettings(func(security.SecurityContext) (map[string]string, error) {
		return map[string]string{"bad name": "1"}, nil
	})
	RegisterSecurityHooks(h, list)

	mock.ExpectBegin()
	mock.ExpectRollback()

	rec := httptest.NewRecorder()
	w, _ := common.WrapHTTPRequest(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	base, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	base = context.WithValue(base, security.UserContextKey, &security.UserContext{UserID: 7, UserName: "u"})
	ctx := WithRequestData(base, "public", "items", "items", &delItem{}, &delItem{})
	h.handleDelete(ctx, w, "7", nil)

	if rec.Code == http.StatusOK {
		t.Fatal("a failed stamp must not let the delete proceed")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
