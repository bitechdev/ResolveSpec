package lookup

import (
	"regexp"
	"strings"
	"testing"

	ddlpkg "github.com/bitechdev/ResolveSpec/pkg/security/lookup/ddl"
)

// TestDefaultSchemaMatchesSQLiteDDL keeps the default schema in step with the reference DDL:
// every table and column in the DDL must be a known logical column and vice versa.
func TestDefaultSchemaMatchesSQLiteDDL(t *testing.T) {
	ddl, err := ddlpkg.SQL("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	tableRe := regexp.MustCompile(`(?s)CREATE TABLE IF NOT EXISTS (\w+) \((.*?)\n\);`)
	colRe := regexp.MustCompile(`^\s*(\w+)\s+[A-Z]+`)
	def := DefaultSchema()
	seen := map[Entity]bool{}
	for _, m := range tableRe.FindAllStringSubmatch(string(ddl), -1) {
		e := Entity(m[1])
		tbl, ok := def[e]
		if !ok {
			t.Errorf("DDL table %s has no entity", e)
			continue
		}
		seen[e] = true
		cols := map[string]bool{}
		for _, line := range strings.Split(m[2], "\n") {
			if cm := colRe.FindStringSubmatch(line); cm != nil && cm[1] != "PRIMARY" && cm[1] != "CHECK" && cm[1] != "FOREIGN" {
				cols[cm[1]] = true
				if _, ok := tbl.Columns[cm[1]]; !ok {
					t.Errorf("DDL column %s.%s is not a logical column", e, cm[1])
				}
			}
		}
		for c := range tbl.Columns {
			if !cols[c] {
				t.Errorf("logical column %s.%s is not in the DDL", e, c)
			}
		}
	}
	for e := range def {
		if !seen[e] {
			t.Errorf("entity %s is not in the DDL", e)
		}
	}
}

func TestDefaultSchemaValid(t *testing.T) {
	if err := DefaultSchema().Validate(); err != nil {
		t.Fatal(err)
	}
	if err := DefaultProcNames().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSchemaMergeAndLookup(t *testing.T) {
	cfg := Config{Schema: Schema{
		EntityUsers: {Name: "app_users", Schema: "auth", Columns: map[string]string{"username": "login_name"}},
	}}
	r, err := cfg.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Schema.TableName(EntityUsers); got != "app_users" {
		t.Errorf("table = %q", got)
	}
	if got := r.Schema.SchemaName(EntityUsers); got != "auth" {
		t.Errorf("schema = %q", got)
	}
	if got := r.Schema.Col(UsersUsername); got != "login_name" {
		t.Errorf("username col = %q", got)
	}
	if got := r.Schema.Col(UsersEmail); got != "email" {
		t.Errorf("email col = %q, want default", got)
	}
	// Merge must not mutate the defaults.
	if DefaultSchema().Col(UsersUsername) != "username" {
		t.Error("default schema was mutated")
	}
}

func TestSchemaZeroValueUsesDefaults(t *testing.T) {
	var s Schema
	if s.TableName(EntityUserKeys) != "user_keys" || s.Col(KeysKeyHash) != "key_hash" {
		t.Error("zero schema should fall back to defaults")
	}
}

func TestResolveRejectsBadConfig(t *testing.T) {
	bad := map[string]Config{
		"table injection":  {Schema: Schema{EntityUsers: {Name: "users; DROP TABLE users"}}},
		"column injection": {Schema: Schema{EntityUsers: {Columns: map[string]string{"id": "id) --"}}}},
		"schema injection": {Schema: Schema{EntityUsers: {Schema: "a.b"}}},
		"unknown entity":   {Schema: Schema{"nope": {Name: "x"}}},
		"unknown column":   {Schema: Schema{EntityUsers: {Columns: map[string]string{"nope": "x"}}}},
		"proc injection":   {Procs: ProcNames{Login: "f(); --"}},
		"bad mode":         {Mode: "sometimes"},
		"bad override":     {Overrides: map[Op]Mode{OpLogin: "x"}},
		"bad dialect":      {Dialect: "oracle"},
	}
	for name, cfg := range bad {
		if _, err := cfg.Resolve(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	// A single schema qualifier is allowed on table and procedure names.
	ok := Config{
		Schema: Schema{EntityUsers: {Name: "auth.users"}},
		Procs:  ProcNames{Login: "auth.resolvespec_login"},
	}
	if _, err := ok.Resolve(); err != nil {
		t.Errorf("qualified names should be valid: %v", err)
	}
}

func TestProcNamesMerge(t *testing.T) {
	m := DefaultProcNames().Merge(ProcNames{Login: "custom_login"})
	if m.Login != "custom_login" {
		t.Errorf("Login = %q", m.Login)
	}
	if m.Register != "resolvespec_register" {
		t.Errorf("Register = %q, want default", m.Register)
	}
	if DefaultProcNames().LoginAPIKey != "resolvespec_login_api_key" {
		t.Error("LoginAPIKey default missing")
	}
	if DefaultProcNames().KeystoreValidateKey != "resolvespec_keystore_validate_key" {
		t.Error("keystore defaults missing")
	}
}

func TestEffectiveMode(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		dialect string
		want    Mode
		wantErr bool
	}{
		{"pg default", Config{}, DialectPostgres, ModeProcedure, false},
		{"sqlite default", Config{}, DialectSQLite, ModeDirect, false},
		{"mysql default", Config{}, DialectMySQL, ModeDirect, false},
		{"pg direct", Config{Mode: ModeDirect}, DialectPostgres, ModeDirect, false},
		{"pg auto probes", Config{Mode: ModeAuto}, DialectPostgres, ModeAuto, false},
		{"sqlite auto is direct", Config{Mode: ModeAuto}, DialectSQLite, ModeDirect, false},
		{"sqlite procedure rejected", Config{Mode: ModeProcedure}, DialectSQLite, "", true},
		{"override wins", Config{Overrides: map[Op]Mode{OpSession: ModeDirect}}, DialectPostgres, ModeDirect, false},
		{"override only for its op", Config{Overrides: map[Op]Mode{OpSession: ModeDirect}}, DialectPostgres, ModeProcedure, false},
	}
	for _, c := range cases {
		op := OpLogin
		if strings.HasPrefix(c.name, "override wins") {
			op = OpSession
		}
		got, err := c.cfg.EffectiveMode(op, c.dialect)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("%s: got (%q, %v), want (%q, err=%v)", c.name, got, err, c.want, c.wantErr)
		}
	}
}
