package adamic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// Registration lookup lets precisely these fixtures demonstrate the pre-change hole.
func ambientSubject() rule.Rule {
	for _, registration := range rule.Registered() {
		if registration.Rule.Name == "adamic/no-ambient-declarations" {
			return registration.Rule
		}
	}
	return rule.Rule{Name: "adamic/no-ambient-declarations", Run: func(rule.Context, any) rule.Listeners { return nil }}
}

func ambientFiles(t *testing.T, name string) map[string]string {
	t.Helper()
	files := map[string]string{}
	paths, err := filepath.Glob("testdata/ambient-declarations/" + name + "/*.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files[filepath.Base(path)] = string(data)
	}
	if len(files) == 0 {
		t.Fatal("missing probe", name)
	}
	return files
}

func TestNoAmbientDeclarationsRefused(t *testing.T) {
	t.Parallel()
	for name, span := range map[string]string{
		"const": "limit", "let": "limit", "var": "limit", "function": "missing", "class": "Missing",
		"global-value": "global", "global-method": "global", "module": "'./module.js'", "reference": "./globals.d.ts",
		"reference-library-name": "./lib.es2024.d.ts", "reference-types": "./globals.d.ts", "reference-lib": "dom", "reference-transitive": "./relay.d.ts",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			files := ambientFiles(t, name)
			result := rule_testing.RunTypedFilesWithSetup(t, ambientSubject(), files, "main.ts", writeAdamicSupport(t))
			rule_testing.ExpectFindings(t, result, "ambientDeclaration")
			expectSpans(t, files["main.ts"], result, span)
			if len(result.Diagnostics) == 1 {
				message := result.Diagnostics[0].Message.Description
				if !strings.Contains(message, "runtime") || !strings.Contains(message, "`"+span+"`") {
					t.Errorf("message must name %q and explain its missing runtime definition: %s", span, message)
				}
			}
		})
	}
}

func TestNoAmbientDeclarationsNeighbours(t *testing.T) {
	t.Parallel()
	for name, source := range map[string]string{
		"defined function":         "function value(): number { return 1; } console.log(String(value()));",
		"unused type reference":    "/// <reference types=\"./globals.d.ts\" />\nconsole.log('defined');",
		"unused library reference": "/// <reference lib=\"es2024\" />\nconsole.log('defined');",
		"type query reference":     "/// <reference path=\"./globals.d.ts\" />\ntype Limit = typeof limit; console.log('defined');",
		"shadowed reference":       "/// <reference path=\"./globals.d.ts\" />\nexport {}; const limit = 1; console.log(String(limit));",
		"defined":                  "const limit: number = 1; console.log(String(limit + 1));",
		"field":                    ambientFiles(t, "field")["main.ts"],
		"export":                   "export const limit = 1;",
		"import":                   "import { Box } from './module.js'; console.log(String(new Box()));",
		"narrowed write":           "let value: number | string = 1; if (typeof value === 'number') { value = 2; }",
		"void return":              "const value: () => void = () => 1; value();",
		"unused reference":         "/// <reference path=\"./globals.d.ts\" />\nconsole.log('defined');",
		"ordinary comment":         "// <reference path=\"./globals.d.ts\" />\nconsole.log('defined');",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			files := map[string]string{"main.ts": source, "module.ts": "export class Box {}", "globals.d.ts": "declare const limit: number;"}
			run := func() rule_testing.Result {
				return rule_testing.RunTypedFilesWithSetup(t, ambientSubject(), files, "main.ts", writeAdamicSupport(t))
			}
			rule_testing.ExpectClean(t, run())
			// A missing runtime value planted into each clean neighbour must be reported.
			files["main.ts"] += "\ndeclare const absent: number; console.log(String(absent));"
			rule_testing.ExpectFindings(t, run(), "ambientDeclaration")
		})
	}
}

func TestNoAmbientDeclarationsFileScope(t *testing.T) {
	t.Parallel()
	source := "declare const limit: number;"
	for _, name := range []string{"main.ts", "main.tsx", "main.a", "main.d.ts", "main.d.mts", "main.d.cts", "main.js", "main.jsx", "main.mts", "main.cts"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result := rule_testing.Run(t, ambientSubject(), name, source)
			if name == "main.ts" || name == "main.tsx" || name == "main.a" {
				rule_testing.ExpectFindings(t, result, "ambientDeclaration")
			} else {
				rule_testing.ExpectClean(t, result)
				// The same source in an in-scope file is the positive control for this exclusion.
				rule_testing.ExpectFindings(t, rule_testing.Run(t, ambientSubject(), "control.ts", source), "ambientDeclaration")
			}
		})
	}
}
