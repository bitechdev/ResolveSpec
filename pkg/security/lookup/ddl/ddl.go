// Package ddl holds the reference table schemas for the lookup direct backend, one per
// dialect. They use the lookup.DefaultSchema table and column names; copy and adapt them
// when you override names through lookup.Config.Schema.
//
// The Postgres file creates tables only. The stored-procedure schema
// (lookup/database_schema.sql) is a separate script with native bytea / text[] columns and
// must not be combined with it.
package ddl

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed postgres.sql sqlite.sql mysql.sql mssql.sql
var files embed.FS

// SQL returns the schema script for a dialect name ("postgres", "sqlite", "mysql", "mssql").
func SQL(dialect string) (string, error) {
	b, err := files.ReadFile(dialect + ".sql")
	if err != nil {
		return "", fmt.Errorf("ddl: no reference schema for dialect %q", dialect)
	}
	return string(b), nil
}

// Statements returns the schema as separate statements, for drivers that reject
// multi-statement execution. Comment-only lines are dropped.
func Statements(dialect string) ([]string, error) {
	s, err := SQL(dialect)
	if err != nil {
		return nil, err
	}
	var out []string
	var cur strings.Builder
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "--") {
			continue
		}
		cur.WriteString(line)
		cur.WriteString("\n")
		if strings.HasSuffix(t, ";") {
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		}
	}
	return out, nil
}
