package adamic

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	rule_testing "github.com/system-inc/cohere/internal/lint/testing"
	"github.com/system-inc/cohere/internal/types/program"
)

func ambientSubject() rule.Rule {
	for _, registration := range rule.Registered() {
		if registration.Rule.Name == "adamic/no-ambient-declarations" {
			return registration.Rule
		}
	}
	return rule.Rule{Name: "adamic/no-ambient-declarations", Run: func(rule.Context, any) rule.Listeners { return nil }}
}

func ambientFixture(test *testing.T, name string) map[string]string {
	test.Helper()
	files := map[string]string{}
	root := filepath.Join("testdata/ambient", name)
	walkError := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = string(data)
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	return files
}

func TestNoAmbientDeclarations(test *testing.T) {
	test.Parallel()
	for _, name := range []string{"const", "let", "var", "function", "class", "global-method", "global-value", "module", "reference", "reference-types", "reference-lib"} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			files := ambientFixture(test, name)
			ambientCheckTypes(test, files)
			result := rule_testing.RunTypedFilesWithSetup(test, ambientSubject(), files, "Case.ts", ambientSetup(test, files))
			rule_testing.ExpectFindings(test, result, "ambientDeclaration")
			expected := map[string]string{"const": "limit", "let": "limit", "var": "limit", "function": "missing", "class": "Missing", "global-method": "global", "global-value": "global", "module": "./value.js", "reference": "./phantom.d.ts", "reference-types": "phantom", "reference-lib": "dom"}[name]
			if !strings.Contains(strings.Join(spans(files["Case.ts"], result), ""), expected) {
				test.Errorf("finding does not name %q", expected)
			}
			if len(result.Diagnostics) == 1 {
				message := result.Diagnostics[0].Message.Description
				if !strings.Contains(message, "declar") || !strings.Contains(message, "runtime") || !strings.Contains(message, expected) {
					test.Errorf("message lacks reason: %s", message)
				}
			}
		})
	}
}

func TestNoAmbientDeclarationsNeighbours(test *testing.T) {
	test.Parallel()
	for name, source := range map[string]string{
		"runtime legal probe":           ambientFixture(test, "accepted")["Case.ts"],
		"defined value":                 `const limit = 1; limit.toFixed();`,
		"defined function":              `function present(): number { return 1; } present().toFixed();`,
		"defined class":                 `class Present { readonly value = 1; } new Present().value.toFixed();`,
		"backed field annotation":       `class Present { declare readonly value: number; constructor() { this.value = 1; } } new Present().value.toFixed();`,
		"inherited field annotation":    `class Base { readonly value: number = 1; } class Present extends Base { declare readonly value: number; } new Present().value.toFixed();`,
		"ordinary import export":        `import { Value } from "./value.js"; export const value = new Value();`,
		"narrowed write":                `let value: string | number = 1; if (typeof value === "number") { value = 2; }`,
		"value returning void callback": `const callback: () => void = () => 1; callback();`,
		"reference to defined import":   "/// <reference path=\"./value.ts\" />\nimport { Value } from \"./value.js\"; new Value().present.toFixed();",
		"unused reference":              "/// <reference path=\"./phantom.d.ts\" />\nconst present = 1; present.toFixed();",
		"type reference only":           "/// <reference path=\"./phantom.d.ts\" />\nlet value: typeof missing = 1; value.toFixed();",
		"unrelated runtime value":       "/// <reference path=\"./phantom.d.ts\" />\nconsole.log(\"present\");",
		"comment after statement":       "const present = 1;\n/// <reference path=\"./phantom.d.ts\" />\npresent.toFixed();",
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			files := map[string]string{"Case.ts": source, "phantom.d.ts": "declare const missing: number; declare const unused: number;", "value.ts": "export class Value { readonly present = 1; }"}
			ambientCheckTypes(test, files)
			rule_testing.ExpectClean(test, rule_testing.RunTypedFilesWithSetup(test, ambientSubject(), files, "Case.ts", ambientSetup(test, files)))
			// Plant a phantom ambient binding into every clean fixture. This makes
			// an accidentally disabled rule fail the clean test's own control.
			files["Case.ts"] += "\ndeclare const planted: number; planted.toFixed();"
			ambientCheckTypes(test, files)
			rule_testing.ExpectFindings(test, rule_testing.RunTypedFilesWithSetup(test, ambientSubject(), files, "Case.ts", ambientSetup(test, files)), "ambientDeclaration")
		})
	}
}

func TestNoAmbientDeclarationsFileScope(test *testing.T) {
	test.Parallel()
	for _, extension := range []string{".ts", ".tsx", ".a", ".d.ts", ".d.mts", ".d.cts", ".mts", ".cts", ".js"} {
		test.Run(extension, func(test *testing.T) {
			test.Parallel()
			source := "declare const missing: number;"
			runtimeSource := source + " missing.toFixed();"
			if extension == ".ts" || extension == ".tsx" || extension == ".a" {
				source = runtimeSource
			}
			filename := "Case" + extension
			setup := func(directory string) {
				writeAdamicSupport(test)(directory)
				config := strings.Replace(strings.Replace(adamicConfiguration, `"strict": true`, `"strict": true, "allowJs": true`, 1), `"include": ["**/*.ts"]`, `"sourceExtensions": [".a"], "include": ["**/*"]`, 1)
				if writeError := os.WriteFile(filepath.Join(directory, "tsconfig.json"), []byte(config), 0o644); writeError != nil {
					test.Fatal(writeError)
				}
			}
			result := rule_testing.RunTypedFilesWithSetup(test, ambientSubject(), map[string]string{filename: source}, filename, setup)
			if extension == ".ts" || extension == ".tsx" || extension == ".a" {
				rule_testing.ExpectFindings(test, result, "ambientDeclaration")
			} else {
				rule_testing.ExpectClean(test, result)
				// The same declaration in a source .ts file must report.
				rule_testing.ExpectFindings(test, runAdamic(test, ambientSubject(), runtimeSource), "ambientDeclaration")
			}
		})
	}
}

func ambientCheckTypes(test *testing.T, files map[string]string) {
	test.Helper()
	directory := test.TempDir()
	ambientSetup(test, files)(directory)
	for name, source := range files {
		path := filepath.Join(directory, name)
		if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
		if writeError := os.WriteFile(path, []byte(source), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	graph, buildError := program.Build(program.Options{ConfigFileName: filepath.Join(directory, "tsconfig.json")})
	if buildError != nil {
		test.Fatal(buildError)
	}
	if diagnostics := graph.Program.GetSyntacticDiagnostics(context.Background(), nil); len(diagnostics) != 0 {
		test.Fatalf("syntactic diagnostics: %d, want 0", len(diagnostics))
	}
	if diagnostics := graph.Program.GetSemanticDiagnostics(context.Background(), nil); len(diagnostics) != 0 {
		test.Fatalf("semantic diagnostics: %d, want 0: %v", len(diagnostics), diagnostics)
	}
}

// The DOM probe brings its own console. The ordinary Adamic harness supplies a
// minimal console only when there is no DOM library, avoiding a duplicate binding.
func ambientSetup(test *testing.T, files map[string]string) func(string) {
	test.Helper()
	return func(directory string) {
		writeAdamicSupport(test)(directory)
		if strings.Contains(files["Case.ts"], `<reference lib="dom"`) {
			if removeError := os.Remove(filepath.Join(directory, "console.d.ts")); removeError != nil {
				test.Fatal(removeError)
			}
		}
	}
}

func TestNoAmbientDeclarationFields(test *testing.T) {
	test.Parallel()
	for _, name := range []string{"field-unbacked", "field-same", "field-narrow"} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			files := ambientFixture(test, name)
			ambientCheckTypes(test, files)
			run := func() rule_testing.Result {
				return rule_testing.RunTypedFilesWithSetup(test, ambientSubject(), files, "Case.ts", ambientSetup(test, files))
			}
			if name == "field-same" {
				rule_testing.ExpectClean(test, run())
				// Plant the Node-failing narrowing into the same inherited field slot.
				files["Case.ts"] = strings.Replace(files["Case.ts"], "readonly x: number = 1", `readonly x: number | string = "s"`, 1)
				ambientCheckTypes(test, files)
			}
			result := run()
			rule_testing.ExpectFindings(test, result, "ambientField")
			expectSpans(test, files["Case.ts"], result, "x")
			if len(result.Diagnostics) == 1 {
				message := result.Diagnostics[0].Message.Description
				for _, text := range []string{"x", "number", "runtime"} {
					if !strings.Contains(message, text) {
						test.Errorf("message %q lacks %q", message, text)
					}
				}
			}
		})
	}
}

func TestNoAmbientDeclarationTypeOnlyBindings(test *testing.T) {
	test.Parallel()
	for name, source := range map[string]string{
		"unused":           `declare const missing: number;`,
		"type-only export": `declare const missing: number; export type { missing };`,
		"brand":            ambientFixture(test, "brand-types")["Case.ts"],
		"brand shorthand":  ambientFixture(test, "brand-types")["Case.ts"],
		"class heritage":   `declare class Missing { value: number } type Instance = Missing; export type { Instance };`,
		"function":         `declare function missing(): number; type Callback = typeof missing; export type { Callback };`,
		"class":            `declare class Missing { value: number } type Instance = Missing; type Constructor = typeof Missing; export type { Instance, Constructor };`,
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			files := map[string]string{"Case.ts": source}
			ambientCheckTypes(test, files)
			rule_testing.ExpectClean(test, rule_testing.RunTypedFilesWithSetup(test, ambientSubject(), files, "Case.ts", ambientSetup(test, files)))
			plant := map[string]string{"unused": "missing.toFixed();", "type-only export": "missing.toFixed();", "brand": "translationSlotsBrand.toString();", "function": "missing().toFixed();", "class": "new Missing().value.toFixed();", "brand shorthand": "const value = { translationSlotsBrand }; value.translationSlotsBrand.toString();", "class heritage": "class D extends Missing {} new D();"}[name]
			files["Case.ts"] += "\n" + plant
			ambientCheckTypes(test, files)
			rule_testing.ExpectFindings(test, rule_testing.RunTypedFilesWithSetup(test, ambientSubject(), files, "Case.ts", ambientSetup(test, files)), "ambientDeclaration")
		})
	}
	for _, name := range []string{"brand-value", "brand-other-file", "brand-import-only", "brand-export-value", "brand-type-import"} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			files := ambientFixture(test, name)
			ambientCheckTypes(test, files)
			if name == "brand-type-import" {
				rule_testing.ExpectClean(test, rule_testing.RunTypedFilesWithSetup(test, ambientSubject(), files, "Case.ts", ambientSetup(test, files)))
				files["Read.ts"] = strings.Replace(files["Read.ts"], "import type", "import", 1)
				ambientCheckTypes(test, files)
			}
			rule_testing.ExpectFindings(test, rule_testing.RunTypedFilesWithSetup(test, ambientSubject(), files, "Case.ts", ambientSetup(test, files)), "ambientDeclaration")
		})
	}
}
