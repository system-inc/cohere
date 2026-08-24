package program_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/verify/internal/program"
)

// TestShimFieldAccessorsReadTheFieldsTheyName guards the three checker accessors that are reached
// through an unsafe pointer cast rather than through a method.
//
// `shim/checker/shim.go` mirrors upstream's `Checker` struct and reads `numberType`, `booleanType`
// and `globalRegExpType` by casting a `*checker.Checker` to that mirror. The cast is only correct
// while the mirror reproduces upstream's field offsets exactly, and nothing about it fails loudly
// when it does not: the read returns whatever pointer happens to sit at the wrong offset, which is
// a perfectly valid `*checker.Type` that simply is not the one asked for.
//
// That is not hypothetical. The mirror substitutes stand-in types for upstream's unexported ones,
// configured in `shim/checker/extra-shim.json`, and `checker.symbolArenaLinkStore` was substituted
// with `core.PagedLinkStore`. Upstream's type is a PagedLinkStore of pointers PLUS an Arena of
// values, so the stand-in was 24 bytes short at field 99 of 319 and every field after it read at
// the wrong offset. `Checker_numberType` returned the `null` type and `Checker_booleanType`
// returned `false`.
//
// Nothing in the tree noticed. Every package compiled, every test passed, and the only symptom was
// that `no-for-in-array` reported nothing on any input, because its whole predicate asks whether a
// type has an index signature keyed by `Checker_numberType`. It asked for a `null` index, got nil
// for every input in existence, and declined the entire corpus. A rule that is present and blind is
// the exact failure this project exists to prevent, and it arrived from a layout drift two packages
// away from any rule.
//
// So this asks the checker to name what those accessors return. It is the cheapest possible check
// and it fails loudly on any future drift, including a drift introduced by re-vendoring upstream
// rather than by editing anything here.
func TestShimFieldAccessorsReadTheFieldsTheyName(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "Subject.ts")
	if err := os.WriteFile(sourcePath, []byte("export const value = 1;\n"), 0o644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	configPath := filepath.Join(directory, "tsconfig.json")
	config := `{"compilerOptions":{"strict":true,"target":"ES2022","lib":["ES2022"],"types":[]},"include":["*.ts"]}`
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("writing the tsconfig: %v", err)
	}

	graph, err := program.Build(program.Options{
		ConfigFileName:   configPath,
		CurrentDirectory: directory,
		SingleThreaded:   true,
	})
	if err != nil {
		t.Fatalf("building the type graph: %v", err)
	}
	files := graph.ProjectFiles()
	if len(files) == 0 {
		t.Fatal("the program contains no project files, so this test would prove nothing")
	}

	fileChecker, release := graph.CheckerForFile(context.Background(), files[0])
	defer release()

	// TypeToString is the checker's own name for a type, so this compares what the accessor
	// returned against what the checker calls it rather than against another shim read.
	cases := []struct {
		accessor string
		actual   *checker.Type
		want     string
	}{
		{"Checker_numberType", checker.Checker_numberType(fileChecker), "number"},
		{"Checker_booleanType", checker.Checker_booleanType(fileChecker), "boolean"},
	}
	for _, testCase := range cases {
		if testCase.actual == nil {
			t.Fatalf("%s returned nil, so the struct mirror in shim/checker no longer matches "+
				"upstream's Checker layout", testCase.accessor)
		}
		if got := fileChecker.TypeToString(testCase.actual); got != testCase.want {
			t.Fatalf("%s returned the %q type rather than %q.\n"+
				"The mirror in shim/checker/shim.go has drifted from upstream's Checker layout, so "+
				"this accessor is reading a different field. Check the TypeSubstitutions in "+
				"shim/checker/extra-shim.json: every stand-in must match the size AND alignment of "+
				"the unexported type it replaces, and a short one silently shifts every field "+
				"declared after it.", testCase.accessor, got, testCase.want)
		}
	}

	// globalRegExpType has no live caller today, so it is checked for presence rather than name:
	// it sits past the same drift point and would go nil or wrong under the same failure.
	if checker.Checker_globalRegExpType(fileChecker) == nil {
		t.Fatal("Checker_globalRegExpType returned nil, which points at the same layout drift " +
			"the two accessors above guard against")
	}
}
