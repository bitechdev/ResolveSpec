package lookup_test

import (
	"os/exec"
	"strings"
	"testing"
)

// lookup and its backends sit above sectypes and below security: they must not import the
// core package (security imports lookup) or any sibling sub package.
func TestNoUpwardImports(t *testing.T) {
	for _, pkg := range []string{".", "./procedure", "./direct", "./conformance"} {
		checkNoUpwardImports(t, pkg)
	}
}

func checkNoUpwardImports(t *testing.T, pkg string) {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", pkg).Output()
	if err != nil {
		t.Skipf("go list unavailable: %v", err)
	}
	for _, p := range strings.Fields(string(out)) {
		if strings.Contains(p, "uptrace/bun") || strings.Contains(p, "gorm.io") {
			t.Errorf("%s must not depend on an ORM, found %s", pkg, p)
		}
		if !strings.Contains(p, "/pkg/security") || strings.HasSuffix(p, "/lookup") ||
			strings.HasSuffix(p, "/sectypes") || strings.HasSuffix(p, "/lookup/dialect") ||
			strings.HasSuffix(p, "/lookup/procedure") || strings.HasSuffix(p, "/lookup/direct") || strings.HasSuffix(p, "/lookup/conformance") {
			continue
		}
		t.Errorf("%s must not import %s", pkg, p)
	}
}
