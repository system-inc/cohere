package adamic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	rule_testing "github.com/system-inc/cohere/internal/lint/testing"
)

// Resolve by registration so the same fixtures expose the hole on the old engine.
func crossEnumSubject() rule.Rule {
	for _, entry := range rule.Registered() {
		if entry.Rule.Name == "adamic/no-cross-enum-narrowing" {
			return entry.Rule
		}
	}
	return rule.Rule{Name: "adamic/no-cross-enum-narrowing", Run: func(rule.Context, any) rule.Listeners { return nil }}
}

func crossEnumRun(test *testing.T, source string) rule_testing.Result {
	test.Helper()
	return rule_testing.RunTypedFilesWithSetup(test, crossEnumSubject(), map[string]string{
		"Case.ts": source,
	}, "Case.ts", func(directory string) {
		// Enum syntax is the subject here; Adamic's shared harness forbids all enum syntax.
		configuration := strings.Replace(
			adamicConfiguration,
			`"erasableSyntaxOnly": true`,
			`"erasableSyntaxOnly": false`,
			1,
		)
		if writeError := os.WriteFile(
			filepath.Join(directory, "tsconfig.json"),
			[]byte(configuration),
			0o600,
		); writeError != nil {
			test.Fatal(writeError)
		}
	})
}

func TestCrossEnumNarrowingRefused(test *testing.T) {
	test.Parallel()
	files, globError := filepath.Glob("testdata/cross-enum/*.ts")
	if globError != nil {
		test.Fatal(globError)
	}
	if len(files) != 22 {
		test.Fatalf("fixtures: %d, want 22", len(files))
	}
	for _, file := range files {
		test.Run(strings.TrimSuffix(filepath.Base(file), ".ts"), func(test *testing.T) {
			test.Parallel()
			data, readError := os.ReadFile(file)
			if readError != nil {
				test.Fatal(readError)
			}
			result := crossEnumRun(test, string(data))
			count := 1
			if strings.HasSuffix(file, "declared-before-flow.ts") {
				count = 2
			}
			rule_testing.ExpectFindings(test, result, repeated("crossEnumNarrowing", count)...)
			if len(result.Diagnostics) == 1 {
				message := result.Diagnostics[0].Message.Description
				for _, text := range []string{"Other.", "Kind.", "both", "TypeScript", "run time"} {
					if !strings.Contains(message, text) {
						test.Errorf("message %q lacks %q", message, text)
					}
				}
			}
		})
	}
}

func TestCrossEnumNarrowingLegal(test *testing.T) {
	test.Parallel()
	data, err := os.ReadFile("testdata/cross-enum/t98a.ts")
	if err != nil {
		test.Fatal(err)
	}
	literal, err := os.ReadFile("testdata/cross-enum-j.ts")
	if err != nil {
		test.Fatal(err)
	}
	quiet := map[string]string{}
	for _, name := range []string{
		"any",
		"number-intersection",
		"literal-union",
		"string-literal",
		"wrong-number",
		"wrong-string",
	} {
		bytes, readError := os.ReadFile("testdata/cross-enum-" + name + ".ts")
		if readError != nil {
			test.Fatal(readError)
		}
		quiet[name] = string(bytes)
	}
	base := strings.NewReplacer(
		"return v.value.toFixed(1);",
		"return String(v.value);",
		"console.log(f(p));",
		"console.log('ok');",
	).Replace(string(data))
	for name, source := range map[string]string{
		"wrong number primitive":               quiet["wrong-number"],
		"wrong string primitive":               quiet["wrong-string"],
		"any sibling (tsc keeps P)":            quiet["any"],
		"intersection sibling (tsc keeps P)":   quiet["number-intersection"],
		"literal union sibling (tsc keeps P)":  quiet["literal-union"],
		"string literal sibling (tsc keeps P)": quiet["string-literal"],
		"literal sibling (tsc keeps P)":        string(literal),
		"different member (tsc already rejects)": strings.Replace(
			base,
			"type Q = { kind: Other.X;",
			"type Q = { kind: Other.Y;",
			1,
		),
		"self comparison": strings.Replace(base, "v.kind === Other.X", "v.kind === v.kind", 1),
		"different values": strings.Replace(
			base,
			"enum Other { X, Y }",
			"enum Other { X = 2, Y }",
			1,
		),
		"same enum": strings.NewReplacer(
			"Other.X",
			"Kind.B",
			"type R = { kind: Kind.B; value: boolean };",
			"type R = { kind: Kind.A; value: string };",
		).Replace(base),
		"number literal": strings.Replace(base, "v.kind === Other.X", "v.kind === 0", 1),
		"string literal": strings.NewReplacer(
			"enum Kind { A, B }",
			"enum Kind { A = 'a', B = 'b' }",
			"enum Other { X, Y }",
			"enum Other { X = 'a', Y = 'y' }",
			"v.kind === Other.X",
			"v.kind === 'a'",
		).Replace(base),
		"only compared enum":               strings.Replace(base, "v: P | Q | R", "v: Q", 1),
		"no overlap (tsc already rejects)": strings.Replace(base, "v: P | Q | R", "v: P", 1),
		"ordering":                         strings.Replace(base, "v.kind === Other.X", "v.kind < Other.X", 1),
		"defined value":                    `const value: number = 1; console.log(value);`,
		"narrowed write":                   `let value: string | number = 'ok'; if (typeof value === 'string') { value = 'still ok'; }`,
		"void return":                      `const f: () => void = () => 1; f();`,
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test, crossEnumRun(test, source))
			// Plant the demonstrated unsafe comparison into each otherwise clean program.
			rule_testing.ExpectFindings(test, crossEnumRun(
				test,
				source+"\nnamespace Plant {\n"+strings.ReplaceAll(string(data), "export {};", "")+"\n}",
			), "crossEnumNarrowing")
		})
	}
}

func TestWholeComputedEnumNarrowing(test *testing.T) {
	test.Parallel()
	data, readError := os.ReadFile("testdata/cross-enum-whole-computed.ts")
	if readError != nil {
		test.Fatal(readError)
	}
	rule_testing.ExpectFindings(test, crossEnumRun(test, string(data)), "crossEnumNarrowing")
}
