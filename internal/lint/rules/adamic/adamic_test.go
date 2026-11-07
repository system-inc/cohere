package adamic

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

/*
 * Every adamic rule is tested under Adamic's own compiler options (adamic docs/0.1.md, "Compiler
 * options"), because a hole is a hole only under them: without exactOptionalPropertyTypes or
 * noUncheckedIndexedAccess several of tsc's answers change. The fixtures are the probes on #drbrp8c, each
 * accepted by tsc 6.0.3 and failing on Node 24.14.1 when it fires, and each control running right when it
 * stays clean. Adamic's ten programs, which Adamic 0.1 must compile, are the clean corpus every rule
 * shares (testdata/programs, byte for byte from the tarball on #8vpywxp).
 */

// adamicConfiguration is Adamic 0.1's compiler options, with es2024's library and a console declaration
// standing in for Adamic's own library, as Adamic's verification did.
const adamicConfiguration = `{
	"compilerOptions": {
		"strict": true,
		"noUncheckedIndexedAccess": true,
		"exactOptionalPropertyTypes": true,
		"noImplicitReturns": true,
		"noFallthroughCasesInSwitch": true,
		"erasableSyntaxOnly": true,
		"verbatimModuleSyntax": true,
		"allowImportingTsExtensions": true,
		"noEmit": true,
		"module": "nodenext",
		"target": "es2024",
		"lib": ["es2024"],
		"types": []
	},
	"include": ["**/*.ts"]
}`

// adamicSupport is what Adamic's verification put beside the programs: a console, and the runtime's
// `panic` as the package `adamic`, with the prelude's Weak beside it, byte for byte from Adamic's
// internal/load/prelude.d.ts (#sse0s6s).
var adamicSupport = map[string]string{
	"tsconfig.json":                    adamicConfiguration,
	"package.json":                     `{ "type": "module" }`,
	"console.d.ts":                     `declare const console: { log(message: string): void; error(message: string): void };`,
	"node_modules/adamic/package.json": `{ "name": "adamic", "type": "module", "types": "index.d.ts" }`,
	"node_modules/adamic/index.d.ts": `export declare function panic(message: string): never;
export interface WeakBrand {
	readonly adamicWeak?: never;
}
export type Weak<Target extends object> = (Target & WeakBrand) | undefined;`,
}

// writeAdamicSupport writes adamicSupport into a fixture's directory, over the harness's default tsconfig.
func writeAdamicSupport(test *testing.T) func(directory string) {
	return func(directory string) {
		for name, contents := range adamicSupport {
			path := filepath.Join(directory, name)
			if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
				test.Fatal(createDirectoryError)
			}
			if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
				test.Fatal(writeError)
			}
		}
	}
}

// runAdamic runs one rule over one fixture, Case.ts, under Adamic's options.
func runAdamic[Options any](test *testing.T, subject rule.Rule[Options], source string) rule_testing.Result {
	test.Helper()
	return rule_testing.RunTypedFilesWithSetup(test, subject, map[string]string{"Case.ts": source}, "Case.ts",
		writeAdamicSupport(test))
}

// runAdamicWithOptions is runAdamic for a rule configured with options.
func runAdamicWithOptions[Options any](
	test *testing.T,
	subject rule.Rule[Options],
	source string,
	options Options,
) rule_testing.Result {
	test.Helper()
	return rule_testing.RunTypedFilesWithSetupAndOptions(test, subject, map[string]string{"Case.ts": source},
		"Case.ts", options, writeAdamicSupport(test))
}

// spans is the text each finding covers, in order, from the fixture as the harness wrote it.
func spans(source string, result rule_testing.Result) []string {
	text := rule_testing.FixtureText(source)
	found := make([]string, 0, len(result.Diagnostics))
	for _, diagnostic := range result.Diagnostics {
		found = append(found, text[diagnostic.Range.Pos():diagnostic.Range.End()])
	}
	sort.Strings(found)
	return found
}

// expectSpans asserts exactly the spans the findings cover, so a finding at the wrong node fails as surely
// as a missing one. The ids are asserted beside it, with rule_testing.ExpectFindings, at each call.
func expectSpans(test *testing.T, source string, result rule_testing.Result, want ...string) {
	test.Helper()
	sort.Strings(want)
	if got := spans(source, result); strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		test.Errorf("spans %q, want %q", got, want)
	}
}

// repeated is id once per finding, for ExpectFindings.
func repeated(id string, count int) []string {
	ids := make([]string, count)
	for index := range ids {
		ids[index] = id
	}
	return ids
}

// adamicPrograms reads Adamic's ten programs, keyed by their path under testdata/programs.
func adamicPrograms(test *testing.T) map[string]string {
	test.Helper()
	programs := map[string]string{}
	root := filepath.Join("testdata", "programs")
	walkError := filepath.WalkDir(root, func(path string, entry os.DirEntry, pathError error) error {
		if pathError != nil || entry.IsDir() {
			return pathError
		}
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		relative, relativeError := filepath.Rel(root, path)
		programs[filepath.ToSlash(relative)] = string(contents)
		return relativeError
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	// Eleven files: ten programs, the seventh in two modules.
	if len(programs) != 11 {
		test.Fatalf(
			"read %d program files, want 11, so the clean corpus is not the one Adamic must compile",
			len(programs),
		)
	}
	return programs
}

// expectProgramsClean runs a rule over each of Adamic's programs and expects nothing: each is a program
// Adamic 0.1 must compile, so a finding on one is a rule refusing real Adamic.
func expectProgramsClean[Options any](test *testing.T, subject rule.Rule[Options]) {
	test.Helper()
	programs := adamicPrograms(test)
	for name := range programs {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedFilesWithSetup(test, subject, programs, name, writeAdamicSupport(test))
			rule_testing.ExpectClean(test, result)
		})
	}
}
