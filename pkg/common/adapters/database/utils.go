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

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// PostgreSQL identifier length limit (63 bytes + null terminator = 64 bytes total)
const postgresIdentifierLimit = 63

// checkAliasLength checks if a preload relation path will generate aliases that exceed PostgreSQL's limit
// Returns true if the alias is likely to be truncated
func checkAliasLength(relation string) bool {
	// Bun generates aliases like: parentalias__childalias__columnname
	// For nested preloads, it uses the pattern: relation1__relation2__relation3__columnname
	parts := strings.Split(relation, ".")
	if len(parts) <= 1 {
		return false // Single level relations are fine
	}

	// Calculate the actual alias prefix length that Bun will generate
	// Bun uses double underscores (__) between each relation level
	// and converts the relation names to lowercase with underscores
	aliasPrefix := strings.ToLower(strings.Join(parts, "__"))
	aliasPrefixLen := len(aliasPrefix)

	// We need to add 2 more underscores for the column name separator plus column name length
	// Column names in the error were things like "rid_mastertype_hubtype" (23 chars)
	// To be safe, assume the longest column name could be around 35 chars
	maxColumnNameLen := 35
	estimatedMaxLen := aliasPrefixLen + 2 + maxColumnNameLen

	// Check if this would exceed PostgreSQL's identifier limit
	if estimatedMaxLen > postgresIdentifierLimit {
		logger.Warn("Preload relation '%s' will generate aliases up to %d chars (prefix: %d + column: %d), exceeding PostgreSQL's %d char limit",
			relation, estimatedMaxLen, aliasPrefixLen, maxColumnNameLen, postgresIdentifierLimit)
		return true
	}

	// Also check if just the prefix is getting close (within 15 chars of limit)
	// This gives room for column names
	if aliasPrefixLen > (postgresIdentifierLimit - 15) {
		logger.Warn("Preload relation '%s' has alias prefix of %d chars, which may cause truncation with longer column names (limit: %d)",
			relation, aliasPrefixLen, postgresIdentifierLimit)
		return true
	}

	return false
}

// parseTableName splits a table name that may contain schema into separate schema and table
// For example: "public.users" -> ("public", "users")
//
//	"users" -> ("", "users")
//
// For SQLite, schema.table is translated to schema_table since SQLite doesn't support schemas
// in the same way as PostgreSQL/MSSQL
func parseTableName(fullTableName, driverName string) (schema, table string) {
	if idx := strings.LastIndex(fullTableName, "."); idx != -1 {
		schema = fullTableName[:idx]
		table = fullTableName[idx+1:]

		// For SQLite, convert schema.table to schema_table
		if driverName == "sqlite" || driverName == "sqlite3" {
			table = schema + "_" + table
			schema = ""
		}
		return schema, table
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
