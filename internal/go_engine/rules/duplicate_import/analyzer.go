// Package duplicate_import is staticcheck's `ST1019`, a file importing one package more than once under different
// names, which is seldom meant and usually left behind by a refactoring, ported onto the engine's syntax dispatcher.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 stylecheck/st1019/st1019.go, whose message and related
// information it reports word for word, at the file's first import of the path, with every later import of it
// related, and withholds in a generated file as the original's FilterGenerated does. Like the original, it leaves out
// a blank import, which may sit beside a named one to keep a package's side effects, and the package unsafe, which
// cgo's translation imports under a name of its own beside the user's import. The original reads only each file's
// imports, as the port does, so neither builds an intermediate representation.
//
// The original visits the paths of a file in a map's order, and the port in the order the file first imports each, so
// the port reports the same findings in an order that never changes between runs.
//
// The original's MergeIf, MergeIfAny, tells staticcheck's runner to keep a finding any one build configuration makes,
// and the engine runs one configuration, so there is nothing for the port to merge.
package duplicate_import

import (
	"fmt"
	"go/ast"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Name is the original's code, which the port takes, so the rule, its settings and every `//cohere:ignore` naming it
// read the same as before the port took the original's place.
const Name = "ST1019"

// Doc is the original's title.
const Doc = "Importing the same package multiple times"

// Listener is what the port hears of each package from the dispatcher: each file as the walk enters it, whose imports
// are all the port reads of the syntax.
var Listener = rule.Listener{FileEnter: checkFile}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener.
var Analyzer = dispatch.RuleAnalyzer(rule.Namespace+Name, Name, Doc, &Listener)

// checkFile reports each path file imports more than once, blank imports aside, at its first import.
func checkFile(context *rule.Context, file *ast.File) {
	// importsByPath is each path's imports in the order the file writes them, keyed by the path's literal as written,
	// quotes and all, as the original keys them, and paths is each path in the order the file first imports it.
	importsByPath := map[string][]*ast.ImportSpec{}
	paths := []string{}
	for _, importSpecification := range file.Imports {
		// A blank import is never counted, so one may sit beside an import of its path that keeps the package's side
		// effects should the other stop being used. A second blank import of a path is not counted either, since
		// goimports removes it.
		if importSpecification.Name != nil && importSpecification.Name.Name == "_" {
			continue
		}
		path := importSpecification.Path.Value
		if _, seen := importsByPath[path]; !seen {
			paths = append(paths, path)
		}
		importsByPath[path] = append(importsByPath[path], importSpecification)
	}
	for _, path := range paths {
		// cgo's translation imports unsafe as _cgo_unsafe beside any import of it the file makes itself.
		if path[1:len(path)-1] == "unsafe" || len(importsByPath[path]) < 2 {
			continue
		}
		options := []staticcheck_report.Option{staticcheck_report.FilterGenerated()}
		for _, other := range importsByPath[path][1:] {
			options = append(options, staticcheck_report.Related(other, fmt.Sprintf("other import of %s", path)))
		}
		staticcheck_report.Report(context, importsByPath[path][0],
			fmt.Sprintf("package %s is being imported more than once", path), options...)
	}
}
