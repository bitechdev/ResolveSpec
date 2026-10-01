package direct

import (
	"context"
	"testing"
)

func TestPolicyColumnAndRowSecurity(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	p := NewPolicy(newTestBase(t, db, nil), PolicyOptions{})
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO sec_group_members (group_id, user_id) VALUES (10, 1), (10, 3)`)

	// column rules: user 1 direct, group 10 (user 1 and 3), another user, inactive, other table, prefix table
	exec(`INSERT INTO sec_column_rules (user_id, group_id, schema_name, table_name, column_path, access_type, mask_start, mask_end, mask_invert, mask_char, extra_filters, is_active)
	      VALUES (1, NULL, 'Public', 'Users', 'email', 'mask', 2, 1, 1, '#', '{"k":"v"}', 1),
	             (NULL, 10, 'public', 'users', 'profile.ssn', 'hide', NULL, NULL, NULL, NULL, NULL, 1),
	             (2, NULL, 'public', 'users', 'other', 'hide', 0, 0, 0, '*', NULL, 1),
	             (1, NULL, 'public', 'users', 'off', 'hide', 0, 0, 0, '*', NULL, 0),
	             (1, NULL, 'public', 'orders', 'x', 'hide', 0, 0, 0, '*', NULL, 1),
	             (1, NULL, 'public', 'users_archive', 'y', 'hide', 0, 0, 0, '*', NULL, 1)`)

	rules, err := p.ColumnSecurity(ctx, 1, "public", "users")
	if err != nil || len(rules) != 2 {
		t.Fatalf("%d %v %+v", len(rules), err, rules)
	}
	m := rules[0]
	if m.Accesstype != "mask" || m.MaskStart != 2 || m.MaskEnd != 1 || !m.MaskInvert || m.MaskChar != "#" ||
		m.ExtraFilters["k"] != "v" || len(m.Path) != 1 || m.Path[0] != "email" || m.UserID != 1 {
		t.Fatalf("%+v", m)
	}
	h := rules[1]
	if len(h.Path) != 2 || h.Path[1] != "ssn" || h.MaskChar != "*" || h.Accesstype != "hide" {
		t.Fatalf("%+v", h)
	}
	if r, err := p.ColumnSecurity(ctx, 3, "public", "users"); err != nil || len(r) != 1 {
		t.Fatalf("group member: %d %v", len(r), err)
	}
	if r, err := p.ColumnSecurity(ctx, 99, "public", "users"); err != nil || len(r) != 0 {
		t.Fatalf("no rules must be empty: %d %v", len(r), err)
	}

	// row rules
	exec(`INSERT INTO sec_row_rules (user_id, group_id, schema_name, table_name, template, has_block, is_active) VALUES
	      (1, NULL, 'public', 'orders', 'owner_id = {UserID}', 0, 1),
	      (NULL, 10, 'public', 'orders', 'region = 1', 0, 1),
	      (NULL, 10, 'public', 'orders', 'ignored', 0, 0),
	      (3, NULL, 'public', 'secret', NULL, 1, 1),
	      (NULL, 10, 'public', 'secret', 'x = 1', 0, 1)`)
	rs, err := p.RowSecurity(ctx, 1, "public", "orders")
	if err != nil || rs.Template != "(owner_id = {UserID}) AND (region = 1)" || rs.HasBlock {
		t.Fatalf("%+v %v", rs, err)
	}
	if rs, err := p.RowSecurity(ctx, "3", "PUBLIC", "Secret"); err != nil || !rs.HasBlock || rs.Template != "" {
		t.Fatalf("block must win: %+v %v", rs, err)
	}
	if rs, err := p.RowSecurity(ctx, 99, "public", "orders"); err != nil || rs.Template != "" || rs.HasBlock {
		t.Fatalf("%+v %v", rs, err)
	}
	for _, bad := range []any{nil, "abc", []int{1}, 1.5} {
		if _, err := p.RowSecurity(ctx, bad, "public", "orders"); err == nil {
			t.Fatalf("user ref %#v accepted", bad)
		}
	}
}

func TestPolicyNoGroups(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	p := NewPolicy(newTestBase(t, db, nil), PolicyOptions{NoGroups: true})
	_, _ = db.Exec(`DROP TABLE sec_group_members`)
	_, _ = db.Exec(`INSERT INTO sec_column_rules (user_id, group_id, schema_name, table_name, column_path, access_type, is_active) VALUES
	  (1, NULL, 's', 't', 'a', 'hide', 1), (NULL, 5, 's', 't', 'b', 'hide', 1)`)
	rules, err := p.ColumnSecurity(ctx, 1, "s", "t")
	if err != nil || len(rules) != 1 || rules[0].Path[0] != "a" {
		t.Fatalf("%+v %v", rules, err)
	}
}

func TestPolicyFailsClosedOnMissingTable(t *testing.T) {
	db := newTestDB(t)
	p := NewPolicy(newTestBase(t, db, nil), PolicyOptions{})
	_, _ = db.Exec(`DROP TABLE sec_row_rules`)
	if _, err := p.RowSecurity(context.Background(), 1, "s", "t"); err == nil {
		t.Fatal("expected error for missing table")
	}
}
