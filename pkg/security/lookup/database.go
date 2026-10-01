package lookup

import (
	"database/sql"
	"fmt"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup/dialect"
)

// FromDatabase extracts the *sql.DB and the dialect name from an application's
// common.Database (bun, gorm or pgsql adapter), so callers do not have to dig the
// connection out or set Config.Dialect by hand. The returned name is the adapter's
// normalised DriverName ("postgres", "sqlite", "mssql", "mysql") and is empty when
// the adapter reports a driver the dialect registry does not know; set
// Config.Dialect explicitly in that case.
//
// Transaction adapters do not expose a *sql.DB and are rejected.
func FromDatabase(db common.Database) (*sql.DB, string, error) {
	if db == nil {
		return nil, "", fmt.Errorf("lookup: nil database")
	}
	p, ok := db.(common.SQLDBProvider)
	if !ok {
		return nil, "", fmt.Errorf("lookup: %T does not expose a *sql.DB (transaction adapter or unsupported adapter)", db)
	}
	sqlDB := p.SQLDB()
	if sqlDB == nil {
		return nil, "", fmt.Errorf("lookup: %T has no *sql.DB", db)
	}
	name := db.DriverName()
	if _, err := dialect.Get(name); err != nil {
		name = ""
	}
	return sqlDB, name, nil
}

// ResolveDialect returns the dialect for db: the configured one, or detected from the driver.
func (c Config) ResolveDialect(db *sql.DB) (dialect.Dialect, error) {
	if c.Dialect != "" {
		return dialect.Get(c.Dialect)
	}
	return dialect.Detect(db)
}
