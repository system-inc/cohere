// Package dispatch walks each package once for every dispatched rule, where each rule's analyzer would otherwise walk
// it on its own. A rule registers a rule.Listener through RuleAnalyzer, which returns an analyzer of the rule's own
// whose run reports what the dispatcher found under the rule's name, or returns the rule's crash. So the engine's
// handling of a rule, its settings, suppression, crash accounting, findings cache and rulesRun, reads a dispatched
// rule as any other.
//
// There are two dispatchers, one per walk: IntermediateRepresentation over the engine's own intermediate
// representation, and Syntax over the syntax tree. A listener hears one of them, never both, and the process shares
// one of each, so every rule's analyzer requires the one dispatcher that holds its listener.
package dispatch

import (
	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// sharedIntermediateRepresentation is the process's one dispatcher over the engine's own intermediate
// representation, which RuleAnalyzer registers every such rule on. One, built with the package, is what makes the
// checker walk a package once for all its rules, and keeps every rule's analyzer requiring the very dispatcher that
// holds its listener, however many times the registry is read.
var sharedIntermediateRepresentation = NewIntermediateRepresentation()

// sharedSyntax is the process's one dispatcher over the syntax, shared for sharedIntermediateRepresentation's
// reasons.
var sharedSyntax = NewSyntax()

// RuleAnalyzer registers listener on the shared dispatcher of the walk it hears, Syntax for a syntax listener and
// IntermediateRepresentation for any other, which refuses one that does not declare its walk, as the rule named
// ruleName, and returns the rule's own analyzer, named analyzerName and documented by doc,
// which requires that dispatcher. A house rule's name and its analyzer's differ, `cohere-go/consistency-no-label` and
// `consistency_no_label`, so both are given.
func RuleAnalyzer(ruleName string, analyzerName string, doc string, listener *rule.Listener) *analysis.Analyzer {
	if listener.IsSyntax() {
		return sharedSyntax.RuleAnalyzer(ruleName, analyzerName, doc, listener)
	}
	return sharedIntermediateRepresentation.RuleAnalyzer(ruleName, analyzerName, doc, listener)
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
	return sharedIntermediateRepresentation.Analyzer(), sharedIntermediateRepresentation.Holds(listener)
}

// HeldBy is the analyzer of each shared dispatcher that holds listener, in the order IntermediateRepresentation,
// Syntax, for the registry's test that every dispatched rule is held by exactly one: a listener registered on two
// dispatchers would be walked twice, by two walks, and only one outcome would be read.
func HeldBy(listener *rule.Listener) []*analysis.Analyzer {
	holders := []*analysis.Analyzer{}
	for _, dispatcher := range []*listeners{sharedIntermediateRepresentation.listeners, sharedSyntax.listeners} {
		if dispatcher.Holds(listener) {
			holders = append(holders, dispatcher.Analyzer())
		}
	}
	return holders
}
