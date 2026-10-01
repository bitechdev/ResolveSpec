package common

import (
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/config"
)

func setHardening(t *testing.T, h config.HardeningConfig) {
	t.Helper()
	old := hardeningProvider
	hardeningProvider = func() config.HardeningConfig { return h }
	t.Cleanup(func() { hardeningProvider = old })
}

var (
	hardOn  = config.HardeningConfig{CORSStrictOrigins: true, SortStrict: true, SQLStrict: true}
	hardOff = config.HardeningConfig{}
)

func TestSanitizeWhereClause_Strict(t *testing.T) {
	setHardening(t, hardOn)
	allowed := []string{
		"status = 'awaiting update approval'",
		"last_update > '2020-01-01'",
		"name = 'it''s; fine'",
		"(a = 1 or b = 2) and ifblnk(c) = 'x'",
	}
	for _, w := range allowed {
		if got := SanitizeWhereClause(w, ""); got == "" || got == "(1=0)" {
			t.Errorf("legitimate clause %q rejected: %q", w, got)
		}
	}
	hostile := []string{
		"1=1)) OR ((1=1",
		"id = 1 or (select count(*) from pg_shadow) > 0",
		"id in (select oid from pg_catalog.pg_class)",
		"id = 1 and pg_sleep(5) is not null",
		"id = 1; delete/**/from items",
		"id = 1 -- x",
		"name = 'unterminated",
	}
	for _, w := range hostile {
		if got := SanitizeWhereClause(w, "t"); got != "(1=0)" {
			t.Errorf("hostile clause %q not rejected closed: %q", w, got)
		}
	}
}

func TestSanitizeWhereClause_Subqueries(t *testing.T) {
	q := "id in (select l.id from other l where l.x = 1)"
	setHardening(t, hardOn)
	if got := SanitizeWhereClause(q, "t"); got == "(1=0)" || got == "" {
		t.Errorf("subquery should be allowed by default: %q", got)
	}
	h := hardOn
	h.SQLBlockSubqueries = true
	setHardening(t, h)
	if got := SanitizeWhereClause(q, "t"); got != "(1=0)" {
		t.Errorf("subquery should be blocked with sql_block_subqueries: %q", got)
	}
}

func TestSanitizeWhereClause_StrictOff_LegacyBehaviour(t *testing.T) {
	setHardening(t, hardOff)
	if got := SanitizeWhereClause("a = 1 and drop table x", "t"); got != "" {
		t.Errorf("legacy fail-open expected empty, got %q", got)
	}
}

func TestSortStrict(t *testing.T) {
	v := NewColumnValidator(TestModel{})
	opts := RequestOptions{
		JoinAliases: []string{""},
		Sort:        []SortOption{{Column: "x.id, (select pg_sleep(10))"}, {Column: "(select pg_sleep(1))"}},
	}
	setHardening(t, hardOn)
	if got := v.FilterRequestOptions(opts).Sort; len(got) != 0 {
		t.Errorf("strict: expected no sorts, got %v", got)
	}
	opts.JoinAliases = []string{"j"}
	opts.Sort = []SortOption{{Column: "j.id"}, {Column: "j.id, (select 1)"}, {Column: "(select max(age) from users)"}}
	if got := v.FilterRequestOptions(opts).Sort; len(got) != 2 {
		t.Errorf("strict: join column and plain subquery sort must be allowed, got %v", got)
	}
	opts.Sort = []SortOption{{Column: "j.id"}, {Column: "j.id, (select 1)"}}
	if got := v.FilterRequestOptions(opts).Sort; len(got) != 1 || got[0].Column != "j.id" {
		t.Errorf("strict: expected only j.id, got %v", got)
	}
	setHardening(t, hardOff)
	opts.JoinAliases = []string{""}
	opts.Sort = []SortOption{{Column: "x.id, (select pg_sleep(10))"}}
	if got := v.FilterRequestOptions(opts).Sort; len(got) != 1 {
		t.Errorf("legacy: expected sort kept, got %v", got)
	}
}

func TestCQLColumn(t *testing.T) {
	v := NewColumnValidator(TestModel{})
	setHardening(t, hardOn)
	if !v.IsValidColumn("cqlComputed1") || v.IsValidColumn("cql1); drop") {
		t.Error("strict cql validation wrong")
	}
	setHardening(t, hardOff)
	if !v.IsValidColumn("cql1); drop") {
		t.Error("legacy cql behaviour should be permissive")
	}
}

func TestOriginAllowed(t *testing.T) {
	list := []string{"https://app.example.com/", "http://localhost:8080"}
	if ok, _ := originAllowed("https://APP.example.com", list); !ok {
		t.Error("listed origin should match case-insensitively, ignoring trailing slash")
	}
	if ok, _ := originAllowed("https://evil.example", list); ok {
		t.Error("unlisted origin must not match")
	}
	if ok, wc := originAllowed("https://evil.example", []string{"*"}); ok || !wc {
		t.Error("wildcard must be reported separately and never as an exact match")
	}
}
