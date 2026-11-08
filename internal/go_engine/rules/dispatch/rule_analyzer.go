package dispatch

import (
	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// sharedIr is the process's one dispatcher over the intermediate representation, which RuleAnalyzer registers every
// such rule on. One, built with the package, is what makes the checker walk a package once for all its rules, and
// keeps every rule's analyzer requiring the very dispatcher that holds its listener, however many times the registry
// is read.
var sharedIr = NewIr()

// sharedSyntax is the process's one dispatcher over the syntax, shared for sharedIr's reasons.
var sharedSyntax = NewSyntax()

// RuleAnalyzer registers listener on the shared dispatcher of the walk it hears, Syntax for a syntax listener and Ir
// for any other, as the rule named ruleName, and returns the rule's own analyzer, named analyzerName and documented by
// doc, which requires that dispatcher. A house rule's name and its analyzer's differ, `cohere-go/consistency-no-label`
// and `consistency_no_label`, so both are given.
func RuleAnalyzer(ruleName string, analyzerName string, doc string, listener *rule.Listener) *analysis.Analyzer {
	if listener.IsSyntax() {
		return sharedSyntax.RuleAnalyzer(ruleName, analyzerName, doc, listener)
	}
	return sharedIr.RuleAnalyzer(ruleName, analyzerName, doc, listener)
}

// SharedSyntax is the process's one dispatcher over the syntax, for a rule with options that builds its analyzer
// through one constructor taking a dispatcher: the registry's analyzer on this one, and a test's configured copy on a
// NewSyntax of its own, since a dispatcher names each rule once.
func SharedSyntax() *Syntax {
	return sharedSyntax
}

// Shared is the analyzer of the shared dispatcher listener's walk goes to, and whether that dispatcher holds it.
func Shared(listener *rule.Listener) (*analysis.Analyzer, bool) {
	if listener.IsSyntax() {
		return sharedSyntax.Analyzer(), sharedSyntax.Holds(listener)
	}
	return sharedIr.Analyzer(), sharedIr.Holds(listener)
}
