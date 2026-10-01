package security

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The core package must contain no SQL: every database access goes through pkg/security/lookup.
func TestCoreContainsNoSQL(t *testing.T) {
	forbidden := []*regexp.Regexp{
		regexp.MustCompile(`(?i)"[^"\n]*\b(select\s.+\sfrom|insert\s+into|delete\s+from|update\s+\w+\s+set|create\s+table)\b`),
		regexp.MustCompile("(?i)`[^`]*\\b(select\\s.+\\sfrom|insert\\s+into|delete\\s+from|update\\s+\\w+\\s+set)\\b"),
		regexp.MustCompile(`\b(db|sqlDB|conn)\.(Query|QueryRow|Exec|Prepare|Begin)(Context|Tx)?\(`),
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		// Example files are documentation, not library code.
		if strings.HasSuffix(f, "_test.go") || strings.HasPrefix(f, "examples") || strings.HasSuffix(f, "_examples.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if c := strings.TrimSpace(line); strings.HasPrefix(c, "//") {
				continue
			}
			for _, re := range forbidden {
				if re.MatchString(line) {
					t.Errorf("%s:%d looks like SQL / direct database access: %s", f, i+1, strings.TrimSpace(line))
				}
			}
		}
	}
}
