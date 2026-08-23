package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A rule that reads ctx.Program must declare ReadsProgram, and this is what makes that structural
// rather than remembered.
//
// ctx.Program reaches every source file in the run, so a rule that touches it is not pure per-file.
// The findings cache keys on the linted file's hash, so an undeclared cross-file rule serves a
// stale result when the other file changes: zero findings, forever, indistinguishable from a clean
// tree. Nothing downstream notices, which is why the check lives here instead of in a convention.
//
// The check is textual on purpose. A rule reaching for the program writes ctx.Program somewhere in
// its own file, and a grep for that is a check nobody can quietly route around, where a check on
// behavior would need a program to run against and would then only cover the rules it happened to
// exercise.
func TestRulesReadingProgramDeclareIt(t *testing.T) {
	ruleFiles := ruleSourceFiles(t)
	if len(ruleFiles) == 0 {
		// A check that found nothing to check passes for the wrong reason, which is the same shape
		// as the defect it guards against.
		t.Fatal("found no rule source files, so this test proved nothing")
	}

	sawProgramReader := false
	for _, path := range ruleFiles {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		source := string(contents)

		if !strings.Contains(source, "ctx.Program") && !strings.Contains(source, "context.Program") {
			continue
		}
		sawProgramReader = true

		if !strings.Contains(source, "ReadsProgram:") {
			t.Errorf("%s reads the program and does not declare ReadsProgram, so a findings cache "+
				"keyed on one file's hash would serve a stale result when the other file changes",
				path)
		}
	}

	if !sawProgramReader {
		// Two rules and the upstream adapter read the program today. Zero would mean the search is
		// looking in the wrong place rather than that the tree got cleaner.
		t.Fatal("no rule source references ctx.Program, which is implausible: this check is " +
			"looking at the wrong files")
	}
}

// ruleSourceFiles lists the non-test Go files under internal/rules.
func ruleSourceFiles(t *testing.T) []string {
	t.Helper()

	var found []string
	err := filepath.Walk("../rules", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		found = append(found, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walking rule packages: %v", err)
	}
	return found
}
