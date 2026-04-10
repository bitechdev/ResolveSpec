package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/bitechdev/ResolveSpec/pkg/metrics"
)

type queryMetricCall struct {
	operation string
	schema    string
	entity    string
	table     string
}

type capturingMetricsProvider struct {
	mu    sync.Mutex
	calls []queryMetricCall
}

func (c *capturingMetricsProvider) RecordHTTPRequest(method, path, status string, duration time.Duration) {
}
func (c *capturingMetricsProvider) IncRequestsInFlight() {}
func (c *capturingMetricsProvider) DecRequestsInFlight() {}
func (c *capturingMetricsProvider) RecordDBQuery(operation, schema, entity, table string, duration time.Duration, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, queryMetricCall{
		operation: operation,
		schema:    schema,
		entity:    entity,
		table:     table,
	})
}
func (c *capturingMetricsProvider) RecordCacheHit(provider string)  {}
func (c *capturingMetricsProvider) RecordCacheMiss(provider string) {}
func (c *capturingMetricsProvider) UpdateCacheSize(provider string, size int64) {
}
func (c *capturingMetricsProvider) RecordEventPublished(source, eventType string) {}
func (c *capturingMetricsProvider) RecordEventProcessed(source, eventType, status string, duration time.Duration) {
}
func (c *capturingMetricsProvider) UpdateEventQueueSize(size int64) {}
func (c *capturingMetricsProvider) RecordPanic(methodName string)   {}
func (c *capturingMetricsProvider) Handler() http.Handler           { return http.NewServeMux() }

func (c *capturingMetricsProvider) snapshot() []queryMetricCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]queryMetricCall, len(c.calls))
	copy(out, c.calls)
	return out
}

type queryMetricsGormUser struct {
	ID   int `gorm:"primaryKey"`
	Name string
}

func (queryMetricsGormUser) TableName() string {
	return "metrics_gorm_users"
}

type queryMetricsBunUser struct {
	bun.BaseModel `bun:"table:metrics_bun_users"`
	ID            int64  `bun:"id,pk,autoincrement"`
	Name          string `bun:"name"`
}

func TestPgSQLAdapterRecordsSchemaEntityTableMetrics(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	provider := &capturingMetricsProvider{}
	prev := metrics.GetProvider()
	metrics.SetProvider(provider)
	defer metrics.SetProvider(prev)

	mock.ExpectExec(`UPDATE users SET name = \$1 WHERE id = \$2`).
		WithArgs("Alice", 1).
		WillReturnResult(sqlmock.NewResult(0, 1))

	adapter := NewPgSQLAdapter(db)
	_, err = adapter.NewUpdate().
		Table("public.users").
		Set("name", "Alice").
		Where("id = ?", 1).
		Exec(context.Background())

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())

	calls := provider.snapshot()
	require.Len(t, calls, 1)
	assert.Equal(t, "UPDATE", calls[0].operation)
	assert.Equal(t, "public", calls[0].schema)
	assert.Equal(t, "users", calls[0].entity)
	assert.Equal(t, "users", calls[0].table)
}

func TestPgSQLAdapterDisableMetricsSuppressesEmission(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	provider := &capturingMetricsProvider{}
	prev := metrics.GetProvider()
	metrics.SetProvider(provider)
	defer metrics.SetProvider(prev)

	mock.ExpectExec(`DELETE FROM users WHERE id = \$1`).
		WithArgs(1).
		WillReturnResult(sqlmock.NewResult(0, 1))

	adapter := NewPgSQLAdapter(db).SetMetricsEnabled(false)
	_, err = adapter.NewDelete().
		Table("users").
		Where("id = ?", 1).
		Exec(context.Background())

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
	assert.Empty(t, provider.snapshot())
}

func TestGormAdapterRecordsEntityAndTableMetrics(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	require.NoError(t, db.AutoMigrate(&queryMetricsGormUser{}))
	require.NoError(t, db.Create(&queryMetricsGormUser{Name: "Alice"}).Error)

	provider := &capturingMetricsProvider{}
	prev := metrics.GetProvider()
	metrics.SetProvider(provider)
	defer metrics.SetProvider(prev)

	adapter := NewGormAdapter(db)
	var users []queryMetricsGormUser
	err = adapter.NewSelect().Model(&users).Scan(context.Background(), &users)

	require.NoError(t, err)
	require.NotEmpty(t, users)

	calls := provider.snapshot()
	require.Len(t, calls, 1)
	assert.Equal(t, "SELECT", calls[0].operation)
	assert.Equal(t, "default", calls[0].schema)
	assert.Equal(t, "query_metrics_gorm_user", calls[0].entity)
	assert.Equal(t, "metrics_gorm_users", calls[0].table)
}

func TestPgSQLAdapterRecordsErrorMetric(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	provider := &capturingMetricsProvider{}
	prev := metrics.GetProvider()
	metrics.SetProvider(provider)
	defer metrics.SetProvider(prev)

	mock.ExpectExec(`INSERT INTO users`).
		WillReturnError(fmt.Errorf("unique constraint violation"))

	adapter := NewPgSQLAdapter(db)
	_, err = adapter.NewInsert().
		Table("users").
		Value("name", "Alice").
		Exec(context.Background())

	require.Error(t, err)

	calls := provider.snapshot()
	require.Len(t, calls, 1)
	assert.Equal(t, "INSERT", calls[0].operation)
	assert.Equal(t, "users", calls[0].table)
}

func TestPgSQLAdapterRecordsExistsMetric(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	provider := &capturingMetricsProvider{}
	prev := metrics.GetProvider()
	metrics.SetProvider(provider)
	defer metrics.SetProvider(prev)

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	adapter := NewPgSQLAdapter(db)
	exists, err := adapter.NewSelect().Table("users").Exists(context.Background())

	require.NoError(t, err)
	assert.True(t, exists)

	calls := provider.snapshot()
	require.Len(t, calls, 1)
	assert.Equal(t, "EXISTS", calls[0].operation)
	assert.Equal(t, "users", calls[0].table)
}

func TestPgSQLAdapterRecordsCountMetric(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	provider := &capturingMetricsProvider{}
	prev := metrics.GetProvider()
	metrics.SetProvider(provider)
	defer metrics.SetProvider(prev)

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	adapter := NewPgSQLAdapter(db)
	count, err := adapter.NewSelect().Table("users").Count(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 5, count)

	calls := provider.snapshot()
	require.Len(t, calls, 1)
	assert.Equal(t, "COUNT", calls[0].operation)
	assert.Equal(t, "users", calls[0].table)
}

func TestPgSQLAdapterRawExecRecordsMetric(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	provider := &capturingMetricsProvider{}
	prev := metrics.GetProvider()
	metrics.SetProvider(provider)
	defer metrics.SetProvider(prev)

	mock.ExpectExec(`UPDATE public\.orders SET status = \$1`).
		WithArgs("shipped").
		WillReturnResult(sqlmock.NewResult(0, 2))

	adapter := NewPgSQLAdapter(db)
	_, err = adapter.Exec(context.Background(), `UPDATE public.orders SET status = $1`, "shipped")

	require.NoError(t, err)

	calls := provider.snapshot()
	require.Len(t, calls, 1)
	assert.Equal(t, "UPDATE", calls[0].operation)
	assert.Equal(t, "public", calls[0].schema)
	assert.Equal(t, "orders", calls[0].table)
}

func TestBunAdapterRecordsEntityAndTableMetrics(t *testing.T) {
	sqldb, err := sql.Open(sqliteshim.ShimName, "file::memory:?cache=shared")
	require.NoError(t, err)
	defer sqldb.Close()

	db := bun.NewDB(sqldb, sqlitedialect.New())
	defer db.Close()

	_, err = db.NewCreateTable().
		Model((*queryMetricsBunUser)(nil)).
		IfNotExists().
		Exec(context.Background())
	require.NoError(t, err)

	_, err = db.NewInsert().Model(&queryMetricsBunUser{Name: "Alice"}).Exec(context.Background())
	require.NoError(t, err)

	provider := &capturingMetricsProvider{}
	prev := metrics.GetProvider()
	metrics.SetProvider(provider)
	defer metrics.SetProvider(prev)

	adapter := NewBunAdapter(db)
	var users []queryMetricsBunUser
	err = adapter.NewSelect().Model(&users).Scan(context.Background(), &users)

	require.NoError(t, err)
	require.NotEmpty(t, users)

	calls := provider.snapshot()
	require.Len(t, calls, 1)
	assert.Equal(t, "SELECT", calls[0].operation)
	assert.Equal(t, "default", calls[0].schema)
	assert.Equal(t, "query_metrics_bun_user", calls[0].entity)
	assert.Equal(t, "metrics_bun_users", calls[0].table)
}
