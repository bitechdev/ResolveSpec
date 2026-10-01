package dialect

import (
	"strconv"
	"strings"
	"time"
)

// --- postgres ---------------------------------------------------------------

type postgres struct{}

func (postgres) Name() string { return "postgres" }
func (postgres) Matches(driver string) bool {
	return strings.Contains(driver, "pgx") || strings.Contains(driver, "lib/pq") ||
		strings.Contains(driver, "postgres")
}
func (postgres) Placeholder(n int) string            { return "$" + strconv.Itoa(n) }
func (postgres) Quote(ident string) string           { return quoteWith(ident, `"`, `"`) }
func (postgres) Bool(v bool) any                     { return v }
func (postgres) ScanBool(src any) (bool, error)      { return scanBool(src) }
func (postgres) ScanTime(src any) (time.Time, error) { return scanTime(src) }
func (postgres) EncodeJSON(v any) (any, error)       { return encodeJSON(v) }
func (postgres) DecodeJSON(src any, dst any) error   { return decodeJSON(src, dst) }
func (d postgres) InsertReturningID(table string, cols []string, idCol string) Insert {
	return Insert{SQL: insertSQL(d, table, cols, "", "RETURNING "+d.Quote(idCol), "DEFAULT VALUES"), Strategy: ReturningQuery}
}

// --- sqlite -----------------------------------------------------------------

type sqlite struct{}

func (sqlite) Name() string { return "sqlite" }
func (sqlite) Matches(driver string) bool {
	return strings.Contains(driver, "sqlite")
}
func (sqlite) Placeholder(int) string              { return "?" }
func (sqlite) Quote(ident string) string           { return quoteWith(ident, `"`, `"`) }
func (sqlite) Bool(v bool) any                     { return boolInt(v) }
func (sqlite) ScanBool(src any) (bool, error)      { return scanBool(src) }
func (sqlite) ScanTime(src any) (time.Time, error) { return scanTime(src) }
func (sqlite) EncodeJSON(v any) (any, error)       { return encodeJSON(v) }
func (sqlite) DecodeJSON(src any, dst any) error   { return decodeJSON(src, dst) }
func (d sqlite) InsertReturningID(table string, cols []string, _ string) Insert {
	return Insert{SQL: insertSQL(d, table, cols, "", "", "DEFAULT VALUES"), Strategy: LastInsertID}
}

// --- mysql / mariadb ----------------------------------------------------------

type mysql struct{}

func (mysql) Name() string { return "mysql" }
func (mysql) Matches(driver string) bool {
	return strings.Contains(driver, "mysql") || strings.Contains(driver, "mariadb")
}
func (mysql) Placeholder(int) string              { return "?" }
func (mysql) Quote(ident string) string           { return quoteWith(ident, "`", "`") }
func (mysql) Bool(v bool) any                     { return boolInt(v) }
func (mysql) ScanBool(src any) (bool, error)      { return scanBool(src) }
func (mysql) ScanTime(src any) (time.Time, error) { return scanTime(src) }
func (mysql) EncodeJSON(v any) (any, error)       { return encodeJSON(v) }
func (mysql) DecodeJSON(src any, dst any) error   { return decodeJSON(src, dst) }
func (d mysql) InsertReturningID(table string, cols []string, _ string) Insert {
	// MySQL has no DEFAULT VALUES; an empty column list is spelled "() VALUES ()".
	s := insertSQL(d, table, cols, "", "", "() VALUES ()")
	return Insert{SQL: s, Strategy: LastInsertID}
}

// --- mssql (SQL Server) --------------------------------------------------------

type mssql struct{}

func (mssql) Name() string { return "mssql" }
func (mssql) Matches(driver string) bool {
	return strings.Contains(driver, "mssql") || strings.Contains(driver, "sqlserver")
}
func (mssql) Placeholder(n int) string            { return "@p" + strconv.Itoa(n) }
func (mssql) Quote(ident string) string           { return quoteWith(ident, "[", "]") }
func (mssql) Bool(v bool) any                     { return v }
func (mssql) ScanBool(src any) (bool, error)      { return scanBool(src) }
func (mssql) ScanTime(src any) (time.Time, error) { return scanTime(src) }
func (mssql) EncodeJSON(v any) (any, error)       { return encodeJSON(v) }
func (mssql) DecodeJSON(src any, dst any) error   { return decodeJSON(src, dst) }
func (d mssql) InsertReturningID(table string, cols []string, idCol string) Insert {
	return Insert{SQL: insertSQL(d, table, cols, "OUTPUT INSERTED."+d.Quote(idCol), "", "DEFAULT VALUES"), Strategy: ReturningQuery}
}

func boolInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}
