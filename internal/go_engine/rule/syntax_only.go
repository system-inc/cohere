package rule

import (
	"fmt"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"

	"github.com/system-inc/cohere/internal/go_engine/rules/marker"
	"github.com/system-inc/cohere/internal/go_engine/rules/positions"
	"github.com/system-inc/cohere/internal/go_engine/staticcheck_configuration"
)

// syntaxOnlyAnswers are the analyzers a syntax-only context answers through ResultOf (see Listener.SyntaxOnly), each
// one whose answer on a file is read from that file's bytes alone, or, for staticcheck's configuration, from the
// package's directory, which every build of a run shares: the line table, the comment indexes, the inspector and the
// merged staticcheck.conf.
var syntaxOnlyAnswers = []*analysis.Analyzer{
	positions.Analyzer,
	marker.Analyzer,
	inspect.Analyzer,
	staticcheck_configuration.Analyzer,
}

// AnswersSyntaxOnly is whether a syntax-only context answers analyzer's result, so a rule marked syntax-only may
// require it.
func AnswersSyntaxOnly(analyzer *analysis.Analyzer) bool {
	return slices.Contains(syntaxOnlyAnswers, analyzer)
}

// ValidateSyntaxOnlyWalk refuses to mark marked a syntax-only walk, a rule whose own analyzer walks pass.Files itself
// and is run through SyntaxOnlyRun, where the mark cannot hold: a dispatched rule, whose listener carries its own mark
// (Listener.SyntaxOnly), a rule that reads type information, and an analyzer that requires what a syntax-only context
// does not answer, or the inspector, whose walk covers every file of the package, the inherited ones among them, which
// SyntaxOnlyRun leaves out of the files it hands the analyzer and of nothing else.
func ValidateSyntaxOnlyWalk(marked Rule) error {
	if marked.Listener != nil {
		return fmt.Errorf("rule %q is marked a syntax-only walk and has a listener; mark the listener SyntaxOnly "+
			"instead", marked.Name)
	}
	if marked.TypeAware {
		return fmt.Errorf("rule %q is marked a syntax-only walk and is TypeAware, and a rule that reads type "+
			"information is not syntax-only", marked.Name)
	}
	for _, required := range marked.Analyzer.Requires {
		if !AnswersSyntaxOnly(required) || required == inspect.Analyzer {
			return fmt.Errorf("rule %q is marked a syntax-only walk and its analyzer requires %s, which a "+
				"syntax-only walk may not read", marked.Name, required.Name)
		}
	}
	return nil
}
