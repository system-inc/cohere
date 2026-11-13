// Package dot_import is staticcheck's `ST1001`, an import with a dot for its name, which leaves a reader unable to
// tell a name of the importing package from one of the imported, ported onto the engine's syntax dispatcher.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 stylecheck/st1001/st1001.go, whose message it reports
// word for word, over the whole import, with no fix, as the original does, and withholds in a generated file as the
// original's FilterGenerated does. Like the original, it leaves alone every import in a test file, internal or
// external, and every import whose path the staticcheck.conf above the package lists in dot_import_whitelist, read
// through the original's config analyzer as the engine runs it (staticcheck_configuration), which reads it under the
// user's cache directory too, where the original's never does, a difference the parity fixtures record as dropped
// (#sc5qmy7). The original reads only each file's imports and the configuration, as the port does, so neither builds
// an intermediate representation.
//
// The original's MergeIf, MergeIfAny, tells staticcheck's runner to keep a finding any one build configuration makes,
// and the engine runs one configuration, so there is nothing for the port to merge.
package dot_import

import (
	"fmt"
	"go/ast"
	"slices"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/config"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
	"github.com/system-inc/cohere/internal/go_engine/staticcheck_configuration"
)

// Name is the original's code, which the port takes, so the rule, its settings and every `//cohere:ignore` naming it
// read the same as before the port took the original's place.
const Name = "ST1001"

// Doc is the original's title.
const Doc = "Dot imports are discouraged"

// message is the original's message, word for word.
const message = "should not use dot imports"

// Listener is what the port hears of each package from the dispatcher: each file as the walk enters it, whose imports
// are all the port reads of the syntax. It requires staticcheck's config analyzer, as the original does, for the
// paths dot_import_whitelist allows.
var Listener = rule.Listener{
	FileLocal: true,
	Requires:  []*analysis.Analyzer{staticcheck_configuration.Analyzer},
	FileEnter: checkFile,
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener.
var Analyzer = dispatch.RuleAnalyzer(rule.Namespace+Name, Name, Doc, &Listener)

// checkFile reports each import of file named with a dot, unless file is a test file or the configuration allows the
// import's path.
func checkFile(context *rule.Context, file *ast.File) {
	configuration, isConfiguration := context.ResultOf(staticcheck_configuration.Analyzer).(*config.Config)
	if !isConfiguration {
		// Invariant: the listener requires the config analyzer, whose result is always a configuration, so anything
		// else is a mistake in building the rule, which the dispatcher's recover makes this rule's crash on the
		// package.
		panic(fmt.Sprintf("reading the configuration: config gave %T",
			context.ResultOf(staticcheck_configuration.Analyzer)))
	}
	for _, importSpecification := range file.Imports {
		// The path is the literal with its quotes cut, never unquoted, as the original cuts it, so a path written with
		// an escape is compared to the allowed paths as written.
		path := importSpecification.Path.Value[1 : len(importSpecification.Path.Value)-1]
		if slices.Contains(configuration.DotImportWhitelist, path) {
			continue
		}
		if importSpecification.Name != nil && importSpecification.Name.Name == "." &&
			!staticcheck_code.IsInTest(context, file) {
			staticcheck_report.Report(context, importSpecification, message, staticcheck_report.FilterGenerated())
		}
	}
}
