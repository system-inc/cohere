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
var allowedRuleImports = map[string]bool{
	"github.com/system-inc/cohere/internal/rule": true,

	// The generated Tailwind collapse table and its gate. Exact rather than a prefix, because a
	// prefix without a trailing slash would also admit `internal/tailwindanything`, and one with a
	// trailing slash matches no package at all when the package is the directory itself.
	"github.com/system-inc/cohere/internal/tailwind": true,
}

// allowedRuleImportPrefixes are subtrees a rule package may depend on wholesale.
//
// What makes an import expensive is not depth but movement: a rebuild is triggered by a dependency
// changing, and neither of these changes when someone edits a rule. So they cost a rule edit
// nothing, and refusing them would push legitimate dependencies into a worse shape for no measured
// gain.
//
// `internal/utilities/` is the shared utility layer, which is the thing rules are supposed to reach
// for. It also now holds what vendoring leaves behind: `ecmascript/` is a ported ECMAScript regex
// parser the regex rules read patterns with, and `typecheck/` is tsgolint's type-checker helper
// shelf. Neither is re-synced against anything, so both move when we change a shared decision
// rather than when an upstream releases.
//
// Measured twice, independently, when `no-invalid-regexp` first reached the parser: editing a
// rule that imports the parser and editing one that does not both rebuild in 0.30s warm, and a rule edit stays in the
// ~1.9s band it was in before the import existed. Editing the parser itself costs 3.24s, which is
// the real depth cost and is paid by whoever edits the parser rather than by rule authors.
//
// `internal/tailwind/` is the generated collapse table and its gate. It is regenerated when Tailwind
// releases and at no other time, which is the movement property this list is about rather than an
// exception to it. Measured on 2026-08-23, three warm rounds each: editing a tailwind rule that
// imports it rebuilds in 0.06s against 0.07s for a rule that imports nothing, which is noise. Editing
// the table itself costs 1.34s for a full binary rebuild, and that is paid by whoever regenerates it
// rather than by rule authors, which is the same shape as the regex parser above.
//
// Widened from `ecmascript/` to `utils/` on 2026-08-23, when the first lift out of a rule package
// landed and this guard refused it. `jsx/` is our own code rather than vendored, which looked like
// the distinction that mattered and is not: the rationale above is movement rather than provenance,
// and a shared helper moves when someone changes a shared decision, which is exactly the event a
// rebuild should follow. Measured on the same tree, three warm rounds:
//
//	editing a rule that imports jsx/     1.24s
//	editing a rule that imports nothing  1.20s
//	editing jsx/ itself                  0.42s
//
// A 0.04s difference is noise, and there is no cliff to protect against. Refusing the import would
// have pushed every lifted helper back into one rule package, which is the drift the retrofit guard
// in this same file exists to prevent. Two guards of ours would have been pulling against each
// other, and the measurement is what says which one was wrong.
var allowedRuleImportPrefixes = []string{
	"github.com/system-inc/cohere/internal/utilities/",
}

// A rule edit must not trigger a deep rebuild, and this is a build-time check rather than a review
// convention because the cost of losing it is measured and large.
//
// Measured on this tree: editing a rule package rebuilds in about 1.8s, while editing something
// deep in the import graph costs 8.52s — a 4.2x cliff on the loop a rule author sits in all day.
// The depth that causes it is invisible in review, because the import that does it looks completely
// reasonable in the file that adds it. So it is asserted here, where it fails loudly the moment it
// stops being true.
//
// The constraint is deliberately "do not depend on things that move" rather than the simpler "do
// not depend on anything." Those came apart the first time a rule package legitimately needed
// vendored upstream: the simpler rule would have rejected an import that costs nothing, which is
// how a guard stops being trusted and starts being worked around.
func TestRulePackagesStayLeaves(t *testing.T) {
	rulePackages := listPackages(t, "./internal/rules/...")
	if len(rulePackages) == 0 {
		// A check that found nothing to check passes for the wrong reason, which is the same shape
		// as the defect it guards against.
		t.Fatal("found no rule packages, so this test proved nothing")
	}

	for _, rulePackage := range rulePackages {
		for _, imported := range listImports(t, rulePackage) {
			if !strings.HasPrefix(imported, "github.com/system-inc/cohere/") {
				// Third-party and standard-library imports are not what this guards: the compiler
				// shim is most of what a rule touches, and it is not ours to be deep in.
				continue
			}
			if allowedRuleImports[imported] || hasAllowedPrefix(imported) {
				continue
			}

			t.Errorf(
				"rule package %s imports %s, which pushes rules off the leaf — a leaf edit rebuilds in about 1.8s, a deep one in about 8.5s",
				rulePackage, imported,
			)
		}
	}
}

// hasAllowedPrefix reports whether an import sits in a subtree rules may depend on wholesale.
func hasAllowedPrefix(imported string) bool {
	for _, prefix := range allowedRuleImportPrefixes {
		if strings.HasPrefix(imported, prefix) {
			return true
		}
	}
	return false
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
