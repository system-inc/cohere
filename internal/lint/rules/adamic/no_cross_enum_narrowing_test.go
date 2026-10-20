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

func crossEnumRun(t *testing.T, source string) rule_testing.Result {
	t.Helper()
	return rule_testing.RunTypedFilesWithSetup(t, crossEnumSubject(), map[string]string{"Case.ts": source}, "Case.ts", func(directory string) {
		// Enum syntax is the subject here; Adamic's shared harness forbids all enum syntax.
		configuration := strings.Replace(adamicConfiguration, `"erasableSyntaxOnly": true`, `"erasableSyntaxOnly": false`, 1)
		if err := os.WriteFile(filepath.Join(directory, "tsconfig.json"), []byte(configuration), 0600); err != nil {
			t.Fatal(err)
		}
	})
}

func TestCrossEnumNarrowingRefused(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("testdata/cross-enum/*.ts")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 17 {
		t.Fatalf("fixtures: %d, want 17", len(files))
	}
	for _, file := range files {
		t.Run(strings.TrimSuffix(filepath.Base(file), ".ts"), func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			result := crossEnumRun(t, string(data))
			count := 1
			if strings.HasSuffix(file, "declared-before-flow.ts") {
				count = 2
			}
			rule_testing.ExpectFindings(t, result, repeated("crossEnumNarrowing", count)...)
			if len(result.Diagnostics) == 1 {
				message := result.Diagnostics[0].Message.Description
				for _, text := range []string{"Other.", "Kind.", "both", "TypeScript", "run time"} {
					if !strings.Contains(message, text) {
						t.Errorf("message %q lacks %q", message, text)
					}
				}
			}
		})
	}
}

func TestCrossEnumNarrowingLegal(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/cross-enum/t98a.ts")
	if err != nil {
		t.Fatal(err)
	}
	base := strings.NewReplacer("return v.value.toFixed(1);", "return String(v.value);", "console.log(f(p));", "console.log('ok');").Replace(string(data))
	for name, source := range map[string]string{
		"different member (tsc already rejects)": strings.Replace(base, "type Q = { kind: Other.X;", "type Q = { kind: Other.Y;", 1),
		"self comparison":                        strings.Replace(base, "v.kind === Other.X", "v.kind === v.kind", 1),
		"different values":                       strings.Replace(base, "enum Other { X, Y }", "enum Other { X = 2, Y }", 1),
		"same enum":                              strings.NewReplacer("Other.X", "Kind.B", "type R = { kind: Kind.B; value: boolean };", "type R = { kind: Kind.A; value: string };").Replace(base),
		"number literal":                         strings.Replace(base, "v.kind === Other.X", "v.kind === 0", 1),
		"string literal":                         strings.NewReplacer("enum Kind { A, B }", "enum Kind { A = 'a', B = 'b' }", "enum Other { X, Y }", "enum Other { X = 'a', Y = 'y' }", "v.kind === Other.X", "v.kind === 'a'").Replace(base),
		"only compared enum":                     strings.Replace(base, "v: P | Q | R", "v: Q", 1),
		"no overlap (tsc already rejects)":       strings.Replace(base, "v: P | Q | R", "v: P", 1),
		"ordering":                               strings.Replace(base, "v.kind === Other.X", "v.kind < Other.X", 1),
		"defined value":                          `const value: number = 1; console.log(value);`,
		"narrowed write":                         `let value: string | number = 'ok'; if (typeof value === 'string') { value = 'still ok'; }`,
		"void return":                            `const f: () => void = () => 1; f();`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rule_testing.ExpectClean(t, crossEnumRun(t, source))
			// Plant the demonstrated unsafe comparison into each otherwise clean program.
			rule_testing.ExpectFindings(t, crossEnumRun(t, source+"\nnamespace Plant {\n"+strings.ReplaceAll(string(data), "export {};", "")+"\n}"), "crossEnumNarrowing")
		})
	}
}

func TestWholeComputedEnumNarrowing(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/cross-enum-whole-computed.ts")
	if err != nil {
		t.Fatal(err)
	}
	rule_testing.ExpectFindings(t, crossEnumRun(t, string(data)), "crossEnumNarrowing")
}
