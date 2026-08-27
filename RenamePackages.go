//go:build ignore

// RenamePackages spells out the abbreviated package names and rewrites every import that names them.
//
// # Why
//
// Nine packages jam two or three words together or abbreviate them, which reads badly at the call
// site and is the one place this tree departs from writing things out. `type_checking.IsThenable` and
// `high_level_intermediate_representation.ForFunction` do not say what they are; `type_checking.IsThenable` and
// `high_level_intermediate_representation.ForFunction` do.
//
// Underscores in a package name are legal Go and unidiomatic — the stdlib jams instead
// (`httputil`, `cookiejar`). That is a deliberate house departure: a hyphen is ILLEGAL in a package
// identifier, so underscore is the only separator available, and readability at 3,367 call sites is
// worth more than matching stdlib aesthetics inside a private repo.
//
// # What it does NOT do
//
// It does not touch `nextjs`, which is the framework's own name, nor any single-word package.
// `ecmascript`, `suppression` and `differential` look long and are one word each; a rename driven by
// a length heuristic would have mangled all three, which is why the list below is explicit rather
// than computed.
//
// It also leaves `shim/ast` alone. That is Microsoft's package from the vendored typescript-go, not
// ours to rename, and aliasing it at ~500 import sites would make every call read
// `abstract_syntax_tree.KindCallExpression` for no gain we control.
//
// # Running it
//
//	go run RenamePackages.go            # dry run
//	go run RenamePackages.go -apply     # writes
//
// Then `go build ./... && go test ./...`. Run on a quiet tree: this rewrites imports across
// every package, so anything mid-edit will conflict.
package main

import (
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// renames maps a package directory to its spelled-out name.
//
// Keys are paths under the module root so `utils` and a future `utils` elsewhere cannot collide.
// The value is the new final path segment AND the new package clause, which are always the same
// here because Go resolves a package by its directory.
var renames = map[string]string{
	"internal/utils":                 "utilities",
	"internal/config":                "configuration",
	"internal/utilities/hir":             "high_level_intermediate_representation",
	"internal/utilities/controlflow":     "control_flow_graph",
	"internal/utilities/typecheck":       "type_checking",
	"internal/formatdiff":            "formatter_comparison",
	"internal/astprobe":              "abstract_syntax_tree_probe",
	"internal/reactconformance":      "react_conformance",
	"internal/reactconformancescore": "react_conformance_score",
	"internal/ruletest":              "rule_testing",
}

// identifierRenames maps the OLD package identifier to the new one, for qualified call sites.
//
// Keyed on the identifier rather than the path because that is what appears at a call site, and
// because two of these (`utils`, `rules`) are directory names that are never used as a qualifier.
var identifierRenames = map[string]string{
	"config":                "configuration",
	"hir":                   "high_level_intermediate_representation",
	"controlflow":           "control_flow_graph",
	"typecheck":             "type_checking",
	"ruletest":              "rule_testing",
	"formatdiff":            "formatter_comparison",
	"astprobe":              "abstract_syntax_tree_probe",
	"reactconformance":      "react_conformance",
	"reactconformancescore": "react_conformance_score",
}

// modulePath is the import prefix every rewritten path carries.
const modulePath = "github.com/system-inc/verify/"

// newImportPath returns the rewritten import path, or the original when nothing applies.
//
// The `utils` rename moves every package NESTED under it as well, which is why this rewrites on a
// path prefix rather than looking the whole path up in the map. `internal/utilities/jsx` has no entry of
// its own and still has to become `internal/utilities/jsx`.
func newImportPath(importPath string) string {
	if !strings.HasPrefix(importPath, modulePath) {
		return importPath
	}
	relative := strings.TrimPrefix(importPath, modulePath)

	// Longest key first, so `internal/utilities/hir` wins over `internal/utils`.
	keys := make([]string, 0, len(renames))
	for key := range renames {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(first, second int) bool { return len(keys[first]) > len(keys[second]) })

	for _, key := range keys {
		if relative != key && !strings.HasPrefix(relative, key+"/") {
			continue
		}
		renamed := filepath.Join(filepath.Dir(key), renames[key])
		rest := strings.TrimPrefix(relative, key)
		// Re-apply the parent rename to the remainder, since a nested package under a renamed
		// parent needs BOTH segments changed.
		return modulePath + newRelativeUnderParent(renamed+rest)
	}
	return importPath
}

// newRelativeUnderParent applies any remaining rename to an already-partly-rewritten path.
func newRelativeUnderParent(relative string) string {
	for key, value := range renames {
		parentRenamed := filepath.Join(filepath.Dir(key), renames[filepath.Dir(key)])
		_ = parentRenamed
		if strings.HasPrefix(relative, key+"/") || relative == key {
			return filepath.Join(filepath.Dir(key), value) + strings.TrimPrefix(relative, key)
		}
	}
	return relative
}

// packageClauseFor returns the new `package X` identifier for a directory, or "" to leave it.
// The directories are moved with `git mv` BEFORE this runs, so a file's current directory is
// already the new name while its import strings still carry the old one. The map is keyed on the
// OLD paths for the import pass; the package clause therefore matches on the directory's final
// segment against the map's VALUES instead of its keys.
func packageClauseFor(directory string) string {
	final := filepath.Base(directory)
	for _, renamed := range renames {
		if final == renamed {
			return renamed
		}
	}
	return ""
}

func main() {
	apply := flag.Bool("apply", false, "write the changes instead of printing them")
	flag.Parse()

	importsRewritten, clausesRewritten, qualifiersRewritten, filesTouched := 0, 0, 0, 0
	fileSet := token.NewFileSet()

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// The vendored compiler and its shim are not ours to rename.
			if path == "typescript-go" || path == "shim" || path == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(fileSet, path, source, parser.ImportsOnly|parser.ParseComments)
		if err != nil {
			// A file mid-write by another author parses badly. Name it rather than skipping
			// silently: a quiet skip reads exactly like a file with no imports to rewrite.
			fmt.Printf("SKIPPED (does not parse): %s: %v\n", path, err)
			return nil
		}

		text := string(source)
		changes := []string{}

		for _, importSpec := range file.Imports {
			current, err := strconv.Unquote(importSpec.Path.Value)
			if err != nil {
				continue
			}
			updated := newImportPath(current)
			if updated == current {
				continue
			}
			text = strings.Replace(text, strconv.Quote(current), strconv.Quote(updated), 1)
			changes = append(changes, "  import "+current+" -> "+updated)
			importsRewritten++
		}

		// Every QUALIFIED reference to a renamed package: `configuration.Load` -> `configuration.Load`.
		//
		// This is the bulk of the work and it is the half a path-only rewrite misses: changing the
		// import line changes which package is bound, and the identifier at 4,593 call sites still
		// names the old one. Measured before this pass existed: rule_testing. alone is 3,366 sites.
		//
		// Word-boundary anchored on both sides so `configuration.` does not match inside `myconfig.` and
		// the trailing dot keeps it to qualified uses rather than the bare word in prose.
		for oldIdentifier, newIdentifier := range identifierRenames {
			pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(oldIdentifier) + `\.`)
			if !pattern.MatchString(text) {
				continue
			}
			count := len(pattern.FindAllString(text, -1))
			text = pattern.ReplaceAllString(text, newIdentifier+".")
			changes = append(changes, fmt.Sprintf("  %d x %s. -> %s.", count, oldIdentifier, newIdentifier))
			qualifiersRewritten += count
		}

		// The package clause, for a file that lives in a renamed directory.
		if clause := packageClauseFor(filepath.Dir(path)); clause != "" && file.Name != nil {
			old := "package " + file.Name.Name
			if file.Name.Name != clause {
				text = strings.Replace(text, old+"\n", "package "+clause+"\n", 1)
				changes = append(changes, "  "+old+" -> package "+clause)
				clausesRewritten++
			}
		}

		if len(changes) > 0 {
			filesTouched++
			fmt.Println(path)
			for _, change := range changes {
				fmt.Println(change)
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

	fmt.Printf("\n%d files, %d imports rewritten, %d package clauses rewritten\n",
		filesTouched, importsRewritten, clausesRewritten)
	fmt.Printf("\nDirectory moves this implies (git mv, NOT performed here):\n")
	keys := make([]string, 0, len(renames))
	for key := range renames {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	// Printed parent-first, and each destination is run through the rewriter, because a nested
	// package under a renamed parent needs BOTH segments changed. Printing the raw key here would
	// name `internal/utilities/hir` as a source path that no longer exists once the parent has moved.
	for _, key := range keys {
		destination := strings.TrimPrefix(newImportPath(modulePath+key), modulePath)
		fmt.Printf("  git mv %s %s\n", key, destination)
	}
	if !*apply {
		fmt.Printf("\ndry run. re-run with -apply to write.\n")
	}
}
