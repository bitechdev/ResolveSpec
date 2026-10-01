package direct

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

// PolicyOptions tunes Policy.
type PolicyOptions struct {
	// NoGroups skips the group membership table: only rules addressed to the user directly
	// apply. Use it when the sec_group_members table is not deployed.
	NoGroups bool
}

// Policy implements lookup.PolicyStore on the rule tables.
//
// Applicable rules are the active rules whose user_id is the caller plus the rules of every
// group the caller belongs to; schema and table match case-insensitively and exactly (never a
// prefix). Column security returns the union of the matching rules. Row security: any
// applicable has_block rule wins, otherwise the templates are combined with AND, each in
// parentheses. No rule is an empty result; failures are errors so callers fail closed.
type Policy struct {
	*Base
	opts PolicyOptions
}

var _ lookup.PolicyStore = (*Policy)(nil)

// NewPolicy creates the direct PolicyStore.
func NewPolicy(b *Base, opts PolicyOptions) *Policy { return &Policy{Base: b, opts: opts} }

// applicable restricts a rule query to the rules that apply to userID.
func (p *Policy) applicable(userCol, groupCol lookup.Column, userID int64) Cond {
	if p.opts.NoGroups {
		return Eq(userCol, userID)
	}
	members := p.From(lookup.EntitySecGroupMembers).Cols(lookup.GroupMembersGroupID).Where(Eq(lookup.GroupMembersUserID, userID))
	return Or(Eq(userCol, userID), InSelect(groupCol, members))
}

// ColumnSecurity implements lookup.PolicyStore.
func (p *Policy) ColumnSecurity(ctx context.Context, userID int, schema, table string) ([]sectypes.ColumnSecurity, error) {
	var rules []sectypes.ColumnSecurity
	err := p.do(func(q Querier) error {
		rules = nil
		rows, err := p.From(lookup.EntitySecColumnRules).
			Cols(lookup.ColRulesID, lookup.ColRulesColumnPath, lookup.ColRulesAccessType, lookup.ColRulesMaskStart,
				lookup.ColRulesMaskEnd, lookup.ColRulesMaskInvert, lookup.ColRulesMaskChar, lookup.ColRulesExtraFilters).
			Where(
				Eq(lookup.ColRulesIsActive, true),
				EqFold(lookup.ColRulesSchemaName, schema),
				EqFold(lookup.ColRulesTableName, table),
				p.applicable(lookup.ColRulesUserID, lookup.ColRulesGroupID, int64(userID)),
			).OrderBy(lookup.ColRulesID).Query(ctx, q)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id int
			var path, access string
			var start, end sql.NullInt64
			var invert sql.NullBool
			var maskChar sql.NullString
			var extra any
			var inv any
			if err := rows.Scan(&id, &path, &access, &start, &end, &inv, &maskChar, &extra); err != nil {
				return err
			}
			if inv != nil {
				b, err := p.d.ScanBool(inv)
				if err != nil {
					return err
				}
				invert = sql.NullBool{Bool: b, Valid: true}
			}
			rule := sectypes.ColumnSecurity{
				ID:         id,
				Schema:     schema,
				Tablename:  table,
				Path:       strings.Split(path, "."),
				Accesstype: access,
				UserID:     userID,
				MaskStart:  int(start.Int64),
				MaskEnd:    int(end.Int64),
				MaskInvert: invert.Bool,
				MaskChar:   "*",
				Control:    schema + "." + table + "." + path,
			}
			if maskChar.Valid && maskChar.String != "" {
				rule.MaskChar = maskChar.String
			}
			if err := p.d.DecodeJSON(extra, &rule.ExtraFilters); err != nil {
				return err
			}
			rules = append(rules, rule)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("failed to load column security: %w", err)
	}
	return rules, nil
}

// numericUser reduces a user reference to the integer the rule tables key on. Structured
// values are rejected, and so are non-numeric strings: a reference that cannot be matched
// must fail closed rather than silently load no rules.
func numericUser(ref any) (int64, error) {
	switch v := ref.(type) {
	case *sectypes.UserContext:
		if v == nil {
			return 0, fmt.Errorf("row security: nil user context")
		}
		return int64(v.UserID), nil
	case sectypes.UserContext:
		return int64(v.UserID), nil
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		return int64(v), nil //nolint:gosec // user ids fit int64
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		return int64(v), nil //nolint:gosec // user ids fit int64
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("row security: user reference %q is not a numeric id", v)
		}
		return n, nil
	case nil:
		return 0, fmt.Errorf("row security: no user reference")
	}
	return 0, fmt.Errorf("row security: unsupported user reference type %T", ref)
}

// RowSecurity implements lookup.PolicyStore.
func (p *Policy) RowSecurity(ctx context.Context, userRef any, schema, table string) (sectypes.RowSecurity, error) {
	uid, err := numericUser(userRef)
	if err != nil {
		return sectypes.RowSecurity{}, err
	}
	var templates []string
	block := false
	err = p.do(func(q Querier) error {
		templates, block = nil, false
		rows, err := p.From(lookup.EntitySecRowRules).Cols(lookup.RowRulesTemplate, lookup.RowRulesHasBlock).
			Where(
				Eq(lookup.RowRulesIsActive, true),
				EqFold(lookup.RowRulesSchemaName, schema),
				EqFold(lookup.RowRulesTableName, table),
				p.applicable(lookup.RowRulesUserID, lookup.RowRulesGroupID, uid),
			).OrderBy(lookup.RowRulesID).Query(ctx, q)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var tpl sql.NullString
			var hb bool
			if err := rows.Scan(&tpl, p.boolDest(&hb)); err != nil {
				return err
			}
			if hb {
				block = true
			}
			if t := strings.TrimSpace(tpl.String); t != "" {
				templates = append(templates, "("+t+")")
			}
		}
		return rows.Err()
	})
	if err != nil {
		return sectypes.RowSecurity{}, fmt.Errorf("failed to load row security: %w", err)
	}
	rs := sectypes.RowSecurity{Schema: schema, Tablename: table, UserID: userRef, HasBlock: block}
	if !block {
		rs.Template = strings.Join(templates, " AND ")
	}
	return rs, nil
}
