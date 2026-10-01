package sectypes_test

import (
	"os/exec"
	"strings"
	"testing"
)

// sectypes is the bottom layer: it may import the standard library only.
func TestOnlyStdlibImports(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", ".").Output()
	if err != nil {
		t.Skipf("go list unavailable: %v", err)
	}
	for _, p := range strings.Fields(string(out)) {
		if !strings.HasSuffix(p, "/sectypes") {
			t.Errorf("sectypes must import only the standard library, found %s", p)
		}
	}
}
