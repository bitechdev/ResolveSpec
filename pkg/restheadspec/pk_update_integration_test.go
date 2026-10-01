//go:build integration

package restheadspec

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
)

type pkAsset struct {
	bun.BaseModel `bun:"table:public.t_pkasset,alias:t_pkasset"`
	Category      string `json:"category" bun:"category,type:citext"`
	Description   string `json:"description" bun:"description,type:citext,pk"`
}

func setupPKTestDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	pg, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:15-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_USER": "testuser", "POSTGRES_PASSWORD": "testpass", "POSTGRES_DB": "testdb",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = pg.Terminate(ctx) })

	host, err := pg.Host(ctx)
	require.NoError(t, err)
	port, err := pg.MappedPort(ctx, "5432")
	require.NoError(t, err)

	db, err := sql.Open("pgx", fmt.Sprintf("postgres://testuser:testpass@%s:%s/testdb?sslmode=disable", host, port.Port()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.Ping())

	_, err = db.Exec(`
		CREATE EXTENSION IF NOT EXISTS citext;
		CREATE TABLE public.t_pkasset (
			category    citext,
			description citext PRIMARY KEY
		);
		INSERT INTO public.t_pkasset VALUES ('cat', 'old-pk');`)
	require.NoError(t, err)
	return db
}

func pkUpdateCtx(base context.Context) context.Context {
	ctx := WithSchema(base, "public")
	ctx = WithEntity(ctx, "t_pkasset")
	ctx = WithTableName(ctx, "t_pkasset")
	return WithModel(ctx, pkAsset{})
}

func countPK(t *testing.T, db *sql.DB, pk string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM public.t_pkasset WHERE description = $1`, pk).Scan(&n))
	return n
}

func TestUpdateChangesPrimaryKeyWhenURLIDGiven(t *testing.T) {
	db := setupPKTestDB(t)
	h := NewHandler(database.NewBunAdapter(bun.NewDB(db, pgdialect.New())), modelregistry.NewModelRegistry())

	update := func(id string, body map[string]interface{}) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		w, _ := common.WrapHTTPRequest(rec, httptest.NewRequest(http.MethodPut, "/", nil))
		base, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		h.handleUpdate(pkUpdateCtx(base), w, id, nil, body, ExtendedRequestOptions{})
		return rec
	}

	// URL id = old PK, body carries a different PK: the PK is changed.
	rec := update("old-pk", map[string]interface{}{"description": "new-pk", "category": "cat2"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 0, countPK(t, db, "old-pk"))
	require.Equal(t, 1, countPK(t, db, "new-pk"))
	var cat string
	require.NoError(t, db.QueryRow(`SELECT category FROM public.t_pkasset WHERE description = 'new-pk'`).Scan(&cat))
	require.Equal(t, "cat2", cat)

	// Body PK equal to the URL id: ordinary update, PK untouched.
	rec = update("new-pk", map[string]interface{}{"description": "new-pk", "category": "cat3"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, countPK(t, db, "new-pk"))

	// Body without a PK: ordinary update.
	rec = update("new-pk", map[string]interface{}{"category": "cat4"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Null PK in the body is ignored, not applied.
	rec = update("new-pk", map[string]interface{}{"description": nil, "category": "cat5"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, countPK(t, db, "new-pk"))
	var total int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM public.t_pkasset`).Scan(&total))
	require.Equal(t, 1, total)
	require.NoError(t, db.QueryRow(`SELECT category FROM public.t_pkasset WHERE description = 'new-pk'`).Scan(&cat))
	require.Equal(t, "cat5", cat)
}
