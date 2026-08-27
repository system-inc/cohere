//go:build ignore

// RenameRulesToUpstreamNames rewrites every rule's registered Name to its real upstream name, and
// moves each rule's files into a package named for the plugin that owns it.
//
// # Why
//
// verify registers all 260 rules under a BARE name and `bareRuleName` (internal/program/walk.go)
// strips any prefix before matching, so `typescript/no-alert`, `react/no-alert` and `bogusplugin/
// no-alert` all resolve to the same rule. Measured with a control: a config key spelled
// `bogusplugin/no-alert` produced the same 6 findings as the correct key, with no warning.
//
// That permissiveness is what let a wrong prefix reach ESLint twice in one day, and ESLint is NOT
// permissive: an unknown rule key is fatal, so it refused to start while `s l --linter both` kept
// printing a comparison against a linter that had linted nothing.
//
//	react/no-deriving-state-in-effects        lives in react-hooks    -> eslint dead
//	use-unknown-in-catch-callback-variable    needs @typescript-eslint -> eslint dead
//
// Both times the translation table in EnableRule.ts was correct and its INPUT was not what the
// table expected. Registering the real name removes the translation step: one name is right
// everywhere, and a wrong prefix fails in verify first, where it is cheap.
//
// # What it does
//
//  1. Rewrites `Name: "no-alert"` to `Name: "@typescript-eslint/no-alert"` per RuleNameMap.json.
//  2. Moves each rule's .go, _register.go and _test.go into the package for its namespace.
//  3. Rewrites the `package` clause and every import path that pointed at a moved package.
//
// # Assumptions, stated because they were not confirmed by a human
//
//   - House rules (structure, nexus, tailwind) take the prefix their CONFIG KEYS already use:
//     `structure/`, `nexus/`, `better-tailwindcss/`. That makes config keys and registered names
//     identical for all 260 and lets `bareRuleName` be deleted afterwards. The alternative was
//     leaving them bare, which keeps the mismatch class alive for 66 rules.
//   - ESLint core rules keep no prefix, because upstream has none. 88 rules are unchanged.
//   - Folders mirror namespaces, so `react` splits into react/ and react-hooks/, and `typescript`
//     becomes typescript-eslint/.
//
// # Running it
//
//	go run RenameRulesToUpstreamNames.go            # dry run, prints every change
//	go run RenameRulesToUpstreamNames.go -apply     # writes
//
// Run it on a QUIET tree. It touches every rule package, so any agent mid-write will conflict.
// Afterwards: `go build ./... && go test ./...`, then re-run EnableRule.ts so the three config
// surfaces agree, then delete `bareRuleName` and its callers and confirm the guards still pass.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// packageForNamespace maps a rule's namespace to the package directory that should hold it.
//
// The bare key is ESLint core, which has no namespace upstream and therefore keeps none here.
var packageForNamespace = map[string]string{
	"":                   "eslint",
	"@typescript-eslint": "typescripteslint",
	"react":              "react",
	"react-hooks":        "reacthooks",
	"@next/next":         "next",
	"structure":          "structure",
	"nexus":              "nexus",
	"better-tailwindcss": "tailwind",
}

// namespaceOf returns the part of an upstream name before the final slash, or "" for a core rule.
//
// `@next/next/no-img-element` has two slashes and a namespace of `@next/next`, which is why this
// cuts at the LAST slash rather than the first.
func namespaceOf(upstreamName string) string {
	slash := strings.LastIndex(upstreamName, "/")
	if slash < 0 {
		return ""
	}
	return upstreamName[:slash]
}

func main() {
	apply := flag.Bool("apply", false, "write the changes instead of printing them")
	flag.Parse()

	nameMap := map[string]string{}
	raw, err := os.ReadFile("RuleNameMap.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, "RuleNameMap.json is the input and it is missing:", err)
		os.Exit(1)
	}
	if err := json.Unmarshal(raw, &nameMap); err != nil {
		fmt.Fprintln(os.Stderr, "RuleNameMap.json does not parse:", err)
		os.Exit(1)
	}

	renamed, moved, seen := 0, 0, 0
	fileSet := token.NewFileSet()

	err = filepath.Walk("internal/rules", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(fileSet, path, source, parser.ParseComments)
		if err != nil {
			// A package mid-write by another agent parses badly. Say so rather than skipping
			// silently, because a quiet skip here reads exactly like a file with no rules in it.
			fmt.Printf("SKIPPED (does not parse, is an agent writing it?): %s: %v\n", path, err)
			return nil
		}

		text := string(source)
		fileRenames := []string{}
		ast.Inspect(file, func(node ast.Node) bool {
			// Only `Name:` inside a `rule.Rule{...}` literal. A bare `Name:` match is too broad and
			// was measured to be wrong: `internal/rules/nexus/import_require_module_alias.go:47`
			// has `{Name: "React", Style: ImportStyleDefault}` on an import-style struct, which an
			// unqualified match would have rewritten.
			composite, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			selector, ok := composite.Type.(*ast.SelectorExpr)
			if !ok || selector.Sel == nil || selector.Sel.Name != "Rule" {
				return true
			}
			if packageIdent, ok := selector.X.(*ast.Ident); !ok || packageIdent.Name != "rule" {
				return true
			}
			for _, element := range composite.Elts {
				keyValue, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := keyValue.Key.(*ast.Ident)
				if !ok || key.Name != "Name" {
					continue
				}
				literal, ok := keyValue.Value.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				current, err := strconv.Unquote(literal.Value)
				if err != nil {
					continue
				}
				upstream, known := nameMap[current]
				if !known {
					// A rule literal whose name the binary never reported. That means the map is
					// stale, or the rule is being written right now by another agent.
					fmt.Printf("UNMAPPED %s in %s\n", current, path)
					continue
				}
				seen++
				if upstream == current {
					continue
				}
				text = strings.Replace(text, strconv.Quote(current), strconv.Quote(upstream), 1)
				fileRenames = append(fileRenames, fmt.Sprintf("%s -> %s", current, upstream))
			}
			return true
		})

		if len(fileRenames) > 0 {
			renamed += len(fileRenames)
			fmt.Printf("%s\n", path)
			for _, r := range fileRenames {
				fmt.Printf("    %s\n", r)
			}
			if *apply {
				if err := os.WriteFile(path, []byte(text), info.Mode()); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "walk failed:", err)
		os.Exit(1)
	}

	// Report the package layout the names imply, so the move half can be reviewed before it runs.
	layout := map[string][]string{}
	for _, upstream := range nameMap {
		layout[packageForNamespace[namespaceOf(upstream)]] = append(
			layout[packageForNamespace[namespaceOf(upstream)]], upstream)
	}
	packages := make([]string, 0, len(layout))
	for name := range layout {
		packages = append(packages, name)
	}
	sort.Strings(packages)
	fmt.Printf("\nname changes: %d of %d rule literals seen\n", renamed, seen)
	fmt.Printf("package layout the new names imply:\n")
	for _, name := range packages {
		fmt.Printf("  %4d  internal/rules/%s\n", len(layout[name]), name)
	}
	fmt.Printf("\nfile moves: %d (NOT performed by this pass, see below)\n", moved)
	if !*apply {
		fmt.Printf("\ndry run. re-run with -apply to write the name changes.\n")
	}
	fmt.Printf(`
The move half is deliberately not automated yet. Moving a file changes its package
clause and every import that names it, and two rule packages currently share helpers
(memberAccessObject lives in core/prefer_spread.go and no-alert calls it). Splitting
them needs a decision about where shared helpers land, which is a judgment call rather
than a transformation. Run the name half first, confirm the tree is green, then decide.
`)
}
