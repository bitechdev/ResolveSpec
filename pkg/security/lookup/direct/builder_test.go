package direct

import (
	"database/sql"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup/dialect"
)

type noRun struct{}

func (noRun) Run(func(*sql.DB) error) error { return nil }

func TestBuilderRendersPerDialect(t *testing.T) {
	schema := lookup.Schema{lookup.EntityUserSessions: {Schema: "auth", Name: "sessions"}}
	cases := map[string]string{
		"postgres": `SELECT t0."session_token" FROM "auth"."sessions" t0`,
		"mysql":    "SELECT t0.`session_token` FROM `auth`.`sessions` t0",
		"mssql":    `SELECT t0.[session_token] FROM [auth].[sessions] t0`,
	}
	tails := map[string]string{
		"postgres": ` WHERE t0."user_id" = $1 AND t0."session_token" IN ($2, $3)`,
		"mysql":    " WHERE t0.`user_id` = ? AND t0.`session_token` IN (?, ?)",
		"mssql":    ` WHERE t0.[user_id] = @p1 AND t0.[session_token] IN (@p2, @p3)`,
	}
	for name, head := range cases {
		d, err := dialect.Get(name)
		if err != nil {
			t.Fatal(err)
		}
		b, err := NewBase(noRun{}, d, schema)
		if err != nil {
			t.Fatal(err)
		}
		st := b.From(lookup.EntityUserSessions).Cols(lookup.SessionsToken).
			Where(Eq(lookup.SessionsUserID, 7), In(lookup.SessionsToken, "a", "b")).Build()
		if st.SQL != head+tails[name] || len(st.Args) != 3 {
			t.Errorf("%s:\n got  %s\n want %s", name, st.SQL, head+tails[name])
		}
	}
}

func TestBuilderBoolsGoThroughDialect(t *testing.T) {
	d, _ := dialect.Get("sqlite")
	b, _ := NewBase(noRun{}, d, nil)
	st := b.From(lookup.EntityUsers).Cols(lookup.UsersID).Where(Eq(lookup.UsersIsActive, true)).Build()
	if st.Args[0] != d.Bool(true) {
		t.Fatalf("bool not converted: %#v", st.Args[0])
	}
}

func TestBuilderSubselectSharesArguments(t *testing.T) {
	d, _ := dialect.Get("postgres")
	b, _ := NewBase(noRun{}, d, nil)
	sub := b.From(lookup.EntitySecGroupMembers).Cols(lookup.GroupMembersGroupID).Where(Eq(lookup.GroupMembersUserID, 5))
	st := b.From(lookup.EntitySecRowRules).Cols(lookup.RowRulesID).
		Where(Eq(lookup.RowRulesTableName, "t"), InSelect(lookup.RowRulesGroupID, sub), Eq(lookup.RowRulesSchemaName, "s")).Build()
	want := `SELECT t0."id" FROM "sec_row_rules" t0 WHERE t0."table_name" = $1 AND t0."group_id" IN (SELECT t1."group_id" FROM "sec_group_members" t1 WHERE t1."user_id" = $2) AND t0."schema_name" = $3`
	if st.SQL != want || len(st.Args) != 3 {
		t.Fatalf("got  %s\nwant %s", st.SQL, want)
	}
}

func TestSchemaRejectsUnsafeIdentifiers(t *testing.T) {
	d, _ := dialect.Get("postgres")
	bad := lookup.Schema{lookup.EntityUsers: {Name: `users"; DROP TABLE x; --`}}
	if _, err := NewBase(noRun{}, d, bad); err == nil {
		t.Fatal("unsafe table name accepted")
	}
	bad = lookup.Schema{lookup.EntityUsers: {Columns: map[string]string{"username": "a b"}}}
	if _, err := NewBase(noRun{}, d, bad); err == nil {
		t.Fatal("unsafe column name accepted")
	}
}
