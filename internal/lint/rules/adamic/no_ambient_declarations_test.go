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

func ambientFixture(t *testing.T, name string) map[string]string {
	t.Helper()
	files := map[string]string{}
	root := filepath.Join("testdata/ambient", name)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
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
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestNoAmbientDeclarations(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"const", "let", "var", "function", "class", "global-method", "global-value", "module", "reference", "reference-types", "reference-lib"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			files := ambientFixture(t, name)
			ambientCheckTypes(t, files)
			result := rule_testing.RunTypedFilesWithSetup(t, ambientSubject(), files, "Case.ts", ambientSetup(t, files))
			rule_testing.ExpectFindings(t, result, "ambientDeclaration")
			expected := map[string]string{"const": "limit", "let": "limit", "var": "limit", "function": "missing", "class": "Missing", "global-method": "global", "global-value": "global", "module": "./value.js", "reference": "./phantom.d.ts", "reference-types": "phantom", "reference-lib": "dom"}[name]
			if !strings.Contains(strings.Join(spans(files["Case.ts"], result), ""), expected) {
				t.Errorf("finding does not name %q", expected)
			}
			if len(result.Diagnostics) == 1 {
				message := result.Diagnostics[0].Message.Description
				if !strings.Contains(message, "declar") || !strings.Contains(message, "runtime") || !strings.Contains(message, expected) {
					t.Errorf("message lacks reason: %s", message)
				}
			}
		})
	}
}

func TestNoAmbientDeclarationsNeighbours(t *testing.T) {
	t.Parallel()
	for name, source := range map[string]string{
		"runtime legal probe":           ambientFixture(t, "accepted")["Case.ts"],
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
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			files := map[string]string{"Case.ts": source, "phantom.d.ts": "declare const missing: number; declare const unused: number;", "value.ts": "export class Value { readonly present = 1; }"}
			ambientCheckTypes(t, files)
			rule_testing.ExpectClean(t, rule_testing.RunTypedFilesWithSetup(t, ambientSubject(), files, "Case.ts", ambientSetup(t, files)))
			// Plant a phantom ambient binding into every clean fixture. This makes
			// an accidentally disabled rule fail the clean test's own control.
			files["Case.ts"] += "\ndeclare const planted: number; planted.toFixed();"
			ambientCheckTypes(t, files)
			rule_testing.ExpectFindings(t, rule_testing.RunTypedFilesWithSetup(t, ambientSubject(), files, "Case.ts", ambientSetup(t, files)), "ambientDeclaration")
		})
	}
}

func TestNoAmbientDeclarationsFileScope(t *testing.T) {
	t.Parallel()
	for _, extension := range []string{".ts", ".tsx", ".a", ".d.ts", ".d.mts", ".d.cts", ".mts", ".cts", ".js"} {
		t.Run(extension, func(t *testing.T) {
			t.Parallel()
			source := "declare const missing: number;"
			filename := "Case" + extension
			setup := func(directory string) {
				writeAdamicSupport(t)(directory)
				config := strings.Replace(strings.Replace(adamicConfiguration, `"strict": true`, `"strict": true, "allowJs": true`, 1), `"include": ["**/*.ts"]`, `"sourceExtensions": [".a"], "include": ["**/*"]`, 1)
				if err := os.WriteFile(filepath.Join(directory, "tsconfig.json"), []byte(config), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			result := rule_testing.RunTypedFilesWithSetup(t, ambientSubject(), map[string]string{filename: source}, filename, setup)
			if extension == ".ts" || extension == ".tsx" || extension == ".a" {
				rule_testing.ExpectFindings(t, result, "ambientDeclaration")
			} else {
				rule_testing.ExpectClean(t, result)
				// The same declaration in a source .ts file must report.
				rule_testing.ExpectFindings(t, runAdamic(t, ambientSubject(), source), "ambientDeclaration")
			}
		})
	}
}

func ambientCheckTypes(t *testing.T, files map[string]string) {
	t.Helper()
	directory := t.TempDir()
	ambientSetup(t, files)(directory)
	for name, source := range files {
		path := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	graph, err := program.Build(program.Options{ConfigFileName: filepath.Join(directory, "tsconfig.json")})
	if err != nil {
		t.Fatal(err)
	}
	if diagnostics := graph.Program.GetSyntacticDiagnostics(context.Background(), nil); len(diagnostics) != 0 {
		t.Fatalf("syntactic diagnostics: %d, want 0", len(diagnostics))
	}
	if diagnostics := graph.Program.GetSemanticDiagnostics(context.Background(), nil); len(diagnostics) != 0 {
		t.Fatalf("semantic diagnostics: %d, want 0: %v", len(diagnostics), diagnostics)
	}
}

// The DOM probe brings its own console. The ordinary Adamic harness supplies a
// minimal console only when there is no DOM library, avoiding a duplicate binding.
func ambientSetup(t *testing.T, files map[string]string) func(string) {
	t.Helper()
	return func(directory string) {
		writeAdamicSupport(t)(directory)
		if strings.Contains(files["Case.ts"], `<reference lib="dom"`) {
			if err := os.Remove(filepath.Join(directory, "console.d.ts")); err != nil {
				t.Fatal(err)
			}
		}
	}
}
