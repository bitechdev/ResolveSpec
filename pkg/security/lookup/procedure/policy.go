package procedure

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

// Policy implements lookup.PolicyStore with the column and row security procedures.
type Policy struct {
	run   Runner
	procs lookup.ProcNames
}

var _ lookup.PolicyStore = (*Policy)(nil)

// NewPolicy creates the procedure-backed PolicyStore.
func NewPolicy(run Runner, procs lookup.ProcNames) *Policy { return &Policy{run: run, procs: procs} }

// ColumnSecurity implements lookup.PolicyStore.
func (p *Policy) ColumnSecurity(ctx context.Context, userID int, schema, table string) ([]sectypes.ColumnSecurity, error) {
	var success bool
	var errorMsg sql.NullString
	var rulesJSON []byte
	err := p.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error, p_rules FROM %s($1, $2, $3)`, p.procs.ColumnSecurity)
		return db.QueryRowContext(ctx, query, userID, schema, table).Scan(&success, &errorMsg, &rulesJSON)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to load column security: %w", err)
	}
	if !success {
		return nil, failure(errorMsg, "failed to load column security")
	}

	type securityRecord struct {
		Control    string `json:"control"`
		Accesstype string `json:"accesstype"`
		JSONValue  string `json:"jsonvalue"`
	}
	var records []securityRecord
	if err := json.Unmarshal(rulesJSON, &records); err != nil {
		return nil, fmt.Errorf("failed to parse security rules: %w", err)
	}

	var rules []sectypes.ColumnSecurity
	for _, rec := range records {
		parts := strings.Split(rec.Control, ".")
		if len(parts) < 3 {
			continue
		}
		rules = append(rules, sectypes.ColumnSecurity{
			Schema:     schema,
			Tablename:  table,
			Path:       parts[2:],
			Accesstype: rec.Accesstype,
			UserID:     userID,
		})
	}
	return rules, nil
}

// RowSecurity implements lookup.PolicyStore. userRef is unwrapped to a scalar user id
// because the procedure's p_user_id is an integer.
func (p *Policy) RowSecurity(ctx context.Context, userRef any, schema, table string) (sectypes.RowSecurity, error) {
	switch v := userRef.(type) {
	case *sectypes.UserContext:
		if v != nil {
			userRef = v.UserID
		}
	case sectypes.UserContext:
		userRef = v.UserID
	}

	var template sql.NullString
	var hasBlock sql.NullBool
	err := p.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_template, p_block FROM %s($1, $2, $3)`, p.procs.RowSecurity)
		return db.QueryRowContext(ctx, query, schema, table, userRef).Scan(&template, &hasBlock)
	})
	if err != nil {
		return sectypes.RowSecurity{}, fmt.Errorf("failed to load row security: %w", err)
	}
	return sectypes.RowSecurity{
		Schema:    schema,
		Tablename: table,
		UserID:    userRef,
		Template:  template.String,
		HasBlock:  hasBlock.Bool,
	}, nil
}
