package database

import (
	"database/sql"
	"strings"

	"github.com/uptrace/bun/dialect/mssqldialect"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

// parseTableName splits a table name that may contain schema into separate schema and table
// For example: "public.users" -> ("public", "users")
//
//	"users" -> ("", "users")
func parseTableName(fullTableName string) (schema, table string) {
	if idx := strings.LastIndex(fullTableName, "."); idx != -1 {
		return fullTableName[:idx], fullTableName[idx+1:]
	}
	return "", fullTableName
}

// GetPostgresDialect returns a Bun PostgreSQL dialect
func GetPostgresDialect() *pgdialect.Dialect {
	return pgdialect.New()
}

// GetSQLiteDialect returns a Bun SQLite dialect
func GetSQLiteDialect() *sqlitedialect.Dialect {
	return sqlitedialect.New()
}

// GetMSSQLDialect returns a Bun MSSQL dialect
func GetMSSQLDialect() *mssqldialect.Dialect {
	return mssqldialect.New()
}

// GetPostgresDialector returns a GORM PostgreSQL dialector
func GetPostgresDialector(db *sql.DB) gorm.Dialector {
	return postgres.New(postgres.Config{
		Conn: db,
	})
}

// GetSQLiteDialector returns a GORM SQLite dialector
func GetSQLiteDialector(db *sql.DB) gorm.Dialector {
	return sqlite.Dialector{
		Conn: db,
	}
}

// GetMSSQLDialector returns a GORM MSSQL dialector
func GetMSSQLDialector(db *sql.DB) gorm.Dialector {
	return sqlserver.New(sqlserver.Config{
		Conn: db,
	})
}
