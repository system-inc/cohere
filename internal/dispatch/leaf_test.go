package dispatch

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// allowedRuleImports are the packages of ours a rule package may depend on.
//
// `internal/rule` is the interface a rule codes against, so depending on it is the entire point.
// Anything else pulls the rules package deeper into the import graph.
var allowedRuleImports = map[string]bool{
	"github.com/system-inc/verify/internal/rule": true,
}

// The rules package must stay a leaf, and this is a build-time check rather than a review
// convention because the cost of losing it is measured and large.
//
// Measured on a comparable Go binary: editing a leaf file rebuilds in 2.03s, while editing
// something deep in the import graph costs 8.52s — a 4.2x cliff on the loop a rule author sits in
// all day. The depth that causes it is invisible in review, because the import that does it looks
// completely reasonable in the file that adds it. So it is asserted here, where it fails loudly the
// moment it stops being true.
func TestRulePackagesStayLeaves(t *testing.T) {
	rulePackages := listPackages(t, "./internal/rules/...")
	if len(rulePackages) == 0 {
		// A check that found nothing to check passes for the wrong reason, which is the same shape
		// as the defect it guards against.
		t.Fatal("found no rule packages, so this test proved nothing")
	}

	for _, rulePackage := range rulePackages {
		for _, imported := range listImports(t, rulePackage) {
			if !strings.HasPrefix(imported, "github.com/system-inc/verify/") {
				// Third-party and standard-library imports are not what this guards: the compiler
				// shim is most of what a rule touches, and it is not ours to be deep in.
				continue
			}
			if allowedRuleImports[imported] {
				continue
			}

			t.Errorf(
				"rule package %s imports %s, which pushes rules off the leaf — a leaf edit rebuilds in about 2s, a deep one in about 8.5s",
				rulePackage, imported,
			)
		}
	}
}

// listPackages returns the import paths matching a pattern.
func listPackages(t *testing.T, pattern string) []string {
	t.Helper()
	return runGoList(t, "-f", "{{.ImportPath}}", pattern)
}

// listImports returns everything a package imports directly.
func listImports(t *testing.T, packagePath string) []string {
	t.Helper()
	return runGoList(t, "-f", `{{range .Imports}}{{.}}{{"\n"}}{{end}}`, packagePath)
}

// runGoList runs `go list` from the module root and returns its non-empty lines.
func runGoList(t *testing.T, arguments ...string) []string {
	t.Helper()

	command := exec.Command("go", append([]string{"list"}, arguments...)...)
	// Tests run in their own package directory, and the patterns here are written relative to the
	// module root.
	command.Dir = filepath.Join("..", "..")

	output, err := command.Output()
	if err != nil {
		t.Fatalf("go list %s: %v", strings.Join(arguments, " "), err)
	}

	lines := []string{}
	for _, line := range strings.Split(string(output), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}
