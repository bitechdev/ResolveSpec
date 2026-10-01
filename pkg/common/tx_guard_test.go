package common

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Source-level regression guard for the single-transaction-per-request rule
// (audit/single_tran.md). Runtime tests prove the current paths; this catches a
// new code path that quietly reaches for the pool.

var guardedSpecs = []string{"resolvespec", "restheadspec", "websocketspec", "mqttspec", "resolvemcp", "funcspec"}

var (
	directTxRE   = regexp.MustCompile(`\.(RunInTransaction|BeginTx)\(`)
	poolHookTxRE = regexp.MustCompile(`\bTx(:\s+|\s*=\s*)(h|handler|h\.handler)\.db\b`)
	poolQueryRE  = regexp.MustCompile(`\b(h|handler)\.db\.(NewSelect|NewInsert|NewUpdate|NewDelete|Exec|Query)\(`)
)

// allowedPoolHookTx: hook contexts that start life on the pool before the handler
// opens its transaction (BeforeHandle runs before any tx and must be DB-free).
// runInTx replaces Tx with the transaction before any other hook runs.
var allowedPoolHookTx = map[string]int{
	"resolvespec/handler.go":   1,
	"websocketspec/handler.go": 1,
	"resolvemcp/handler.go":    4,
}

// allowedPoolQuery: statements outside the request path.
var allowedPoolQuery = map[string]int{
	"resolvemcp/annotation.go": 2, // tool annotations, not a data request
}

func guardedFiles(t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, spec := range guardedSpecs {
		files, err := filepath.Glob(filepath.Join("..", spec, "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no sources found for %s: %v", spec, err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var lines []string
			for _, l := range strings.Split(string(raw), "\n") {
				if s := strings.TrimSpace(l); strings.HasPrefix(s, "//") {
					continue
				}
				lines = append(lines, l)
			}
			out[spec+"/"+filepath.Base(f)] = lines
		}
	}
	return out
}

func countMatches(lines []string, re *regexp.Regexp) int {
	n := 0
	for _, l := range lines {
		if re.MatchString(l) {
			n++
		}
	}
	return n
}

func TestNoDirectTransactionsInSpecHandlers(t *testing.T) {
	for file, lines := range guardedFiles(t) {
		if n := countMatches(lines, directTxRE); n > 0 {
			t.Errorf("%s opens a transaction directly (%d): use the handler's runInTx so OnTxBegin fires", file, n)
		}
	}
}

func TestHookContextsDoNotRetainThePool(t *testing.T) {
	files := guardedFiles(t)
	for file, lines := range files {
		if got, want := countMatches(lines, poolHookTxRE), allowedPoolHookTx[file]; got != want {
			t.Errorf("%s has %d hook contexts set to the pool, allowed %d: hooks must get the transaction", file, got, want)
		}
	}
}

func TestSpecHandlersDoNotQueryThePoolDirectly(t *testing.T) {
	for file, lines := range guardedFiles(t) {
		if got, want := countMatches(lines, poolQueryRE), allowedPoolQuery[file]; got != want {
			t.Errorf("%s runs %d statements on the pool, allowed %d: use the transaction", file, got, want)
		}
	}
}

// Hook types that are defined but deliberately or knowingly never executed.
// Anything else defined in a spec's hooks.go must have an Execute call site: an
// unwired hook silently disables whatever is registered on it (resolvespec's
// AfterRead skipped column-level security masking until it was wired).
var unwiredHooks = map[string]string{}

var hookConstRE = regexp.MustCompile(`(?m)^\s*([A-Z][A-Za-z0-9]*)\s+HookType\s*=`)

func TestEveryDefinedHookHasACallSite(t *testing.T) {
	for _, spec := range []string{"resolvespec", "restheadspec", "websocketspec", "resolvemcp", "funcspec"} {
		raw, err := os.ReadFile(filepath.Join("..", spec, "hooks.go"))
		if err != nil {
			t.Fatal(err)
		}
		var src strings.Builder
		files, _ := filepath.Glob(filepath.Join("..", spec, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") || strings.HasSuffix(f, "hooks.go") || strings.HasSuffix(f, "hooks_example.go") {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			src.Write(b)
		}
		for _, m := range hookConstRE.FindAllStringSubmatch(string(raw), -1) {
			name := m[1]
			if name == "BeforeOp" || name == "OnTxBegin" { // fired by the registry / runInTx
				continue
			}
			if _, ok := unwiredHooks[spec+"/"+name]; ok {
				continue
			}
			call := regexp.MustCompile(`Execute(BeforeOp)?\(` + name + `\b`)
			if !call.MatchString(src.String()) {
				t.Errorf("%s: hook %s is defined but never executed", spec, name)
			}
		}
	}
}
