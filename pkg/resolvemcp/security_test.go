package resolvemcp

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
	"github.com/bitechdev/ResolveSpec/pkg/security"
)

type wItem struct {
	ID       int     `json:"id" bun:"id,pk"`
	Name     string  `json:"name" bun:"name"`
	FullName string  `json:"fullName" bun:"full_name"`
	Note     *string `json:"note" bun:"note"`
	Owner    int     `json:"-" bun:"-"`
}

func TestWriteColumns(t *testing.T) {
	m := &wItem{}
	got, err := writeColumns(m, map[string]interface{}{"fullName": "a", "NAME": "b", "note": nil})
	if err != nil {
		t.Fatal(err)
	}
	if got["full_name"] != "a" || got["name"] != "b" {
		t.Errorf("json/column names must resolve to columns: %v", got)
	}
	if v, ok := got["note"]; !ok || v != nil {
		t.Errorf("explicit null must be kept: %v", got)
	}
	for name, data := range map[string]map[string]interface{}{
		"unknown":     {"nope": 1},
		"injection":   {"name = 'x', id": 1},
		"unmapped":    {"owner": 1},
		"both forms":  {"fullName": 1, "full_name": 2},
		"empty key":   {"": 1},
		"quoted char": {`"name"`: 1},
	} {
		if _, err := writeColumns(m, data); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestCreateRejectsUnknownKeys(t *testing.T) {
	h, mock, ctx := newTxHarness(t)
	mock.ExpectBegin()
	mock.ExpectRollback()
	_, err := h.executeCreate(ctx, "public", "items", map[string]interface{}{"name": "a", "is_admin": true})
	if err == nil || !strings.Contains(err.Error(), "is_admin") {
		t.Fatalf("want unknown field error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateSetsOnlyGivenKeysAndAllowsNull(t *testing.T) {
	db := wHarness(t)
	h, mock, ctx := db.h, db.mock, db.ctx
	cols := []string{"id", "name", "full_name", "note"}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows(cols).AddRow(7, "a", "A", "n"))
	// Only "note" is set (to NULL); the id in the payload addresses the row and is not rewritten.
	mock.ExpectExec(`UPDATE .* SET "?note"? = \$1 WHERE`).WithArgs(nil, "7").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows(cols).AddRow(7, "a", "A", nil))
	mock.ExpectCommit()

	if _, err := h.executeUpdate(ctx, "public", "witems", "7", map[string]interface{}{"id": 7, "note": nil}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateRejectsUnknownKeys(t *testing.T) {
	db := wHarness(t)
	db.mock.ExpectBegin()
	db.mock.ExpectRollback()
	if _, err := db.h.executeUpdate(db.ctx, "public", "witems", "7", map[string]interface{}{"role": "admin"}); err == nil {
		t.Fatal("expected error")
	}
	if err := db.mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type wh struct {
	h    *Handler
	mock sqlmock.Sqlmock
	ctx  context.Context
}

// wHarness is newTxHarness with a model that has more than id/name.
func wHarness(t *testing.T) wh {
	t.Helper()
	h, mock, ctx := newTxHarness(t)
	if err := h.RegisterModel("public", "witems", &wItem{}); err != nil {
		t.Fatal(err)
	}
	return wh{h, mock, ctx}
}

// rlsProvider returns a fixed row security template.
type rlsProvider struct{ stubProvider }

func (rlsProvider) GetRowSecurity(_ context.Context, userRef any, schema, table string) (security.RowSecurity, error) {
	return security.RowSecurity{Schema: schema, Tablename: table, Template: "owner_id = {UserID}", UserID: userRef}, nil
}

func securedHandler(t *testing.T, prov security.SecurityProvider) (*Handler, sqlmock.Sqlmock, context.Context) {
	t.Helper()
	h, mock, ctx := newTxHarness(t)
	list, err := security.NewSecurityList(prov)
	if err != nil {
		t.Fatal(err)
	}
	RegisterSecurityHooks(h, list)
	ctx = context.WithValue(ctx, security.UserContextKey, &security.UserContext{UserID: 7, UserName: "u"})
	ctx = context.WithValue(ctx, security.UserIDKey, 7)
	return h, mock, ctx
}

// A row hidden by row security is "not found" for update and delete, and nothing is written.
func TestWritesHonourRowSecurity(t *testing.T) {
	cols := []string{"id", "name"}
	t.Run("update", func(t *testing.T) {
		h, mock, ctx := securedHandler(t, rlsProvider{})
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT .*owner_id`).WillReturnRows(sqlmock.NewRows(cols))
		mock.ExpectRollback()
		if _, err := h.executeUpdate(ctx, "public", "items", "7", map[string]interface{}{"name": "x"}); err == nil {
			t.Fatal("expected not found")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("delete", func(t *testing.T) {
		h, mock, ctx := securedHandler(t, rlsProvider{})
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT .*owner_id`).WillReturnRows(sqlmock.NewRows(cols))
		mock.ExpectRollback()
		if _, err := h.executeDelete(ctx, "public", "items", "7"); err == nil {
			t.Fatal("expected not found")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}

// Rules set with RegisterModelWithRules must reach the security hooks.
func TestModelRulesReachHooks(t *testing.T) {
	h, mock, ctx := securedHandler(t, stubProvider{})
	if err := h.RegisterModelWithRules("public", "locked", &txItem{}, modelregistry.ModelRules{CanRead: true}); err != nil {
		t.Fatal(err)
	}
	for name, op := range map[string]func() error{
		"create": func() error {
			_, err := h.executeCreate(ctx, "public", "locked", map[string]interface{}{"name": "a"})
			return err
		},
		"update": func() error {
			_, err := h.executeUpdate(ctx, "public", "locked", "7", map[string]interface{}{"name": "a"})
			return err
		},
		"delete": func() error {
			_, err := h.executeDelete(ctx, "public", "locked", "7")
			return err
		},
	} {
		// Each denies inside its transaction, before any statement.
		mock.ExpectBegin()
		mock.ExpectRollback()
		if err := op(); err == nil || !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("%s: want 'not allowed', got %v", name, err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAnnotationToolIsOptIn(t *testing.T) {
	h, _, _ := newTxHarness(t)
	if h.mcpServer.GetTool(annotationToolName) != nil {
		t.Fatal("annotation tool must be off by default")
	}
	on := NewHandler(h.db, modelregistry.NewModelRegistry(), Config{EnableAnnotations: true})
	if on.mcpServer.GetTool(annotationToolName) == nil {
		t.Fatal("annotation tool missing when enabled")
	}
}
