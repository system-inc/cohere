package rules

import (
	"fmt"
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_line"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_shouting"
)

// syntaxOnlyWalks names the house rules whose own analyzer walks pass.Files itself, rather than listen on a dispatcher,
// and whose findings on a file are the file's bytes' and the settings' alone: All runs each through
// rule.SyntaxOnlyRun, so a build checking a package from source takes the host's findings on each file the host
// checked rather than hand the analyzer the file again (#y6q1x2j). A dispatched rule is marked on its listener instead
// (rule.Listener.SyntaxOnly). Each is marked only once its analyzer is read for everything it touches.
var syntaxOnlyWalks = map[string]bool{
	// consistency-no-long-line reads of a pass only its files, their line tables and each file's own bytes through
	// pass.ReadFile, and judges each file on its own: its exemptions come from the file's syntax tree and comments,
	// its limit and tab width are constants, and it has no settings.
	consistency_no_long_line.Name: true,
	// consistency-no-shouting reads of a pass only its files and their line tables, the file's own name among them to
	// find an example's output, and judges each file's comments on their own: the safe list and the two-letter words
	// are policy's embedded file, the same in every build, and it has no settings.
	consistency_no_shouting.Name: true,
}

// syntaxOnlyWalksMarked is whether syntaxOnlyWalks' rules are marked, which they are as the package initializes, before
// any pass and however many times All is read after, since a mark wraps the rule's analyzer in place
// (markedSyntaxOnlyWalks).
var syntaxOnlyWalksMarked = markedSyntaxOnlyWalks()

// markedSyntaxOnlyWalks runs the analyzer of each registered rule syntaxOnlyWalks names through rule.SyntaxOnlyRun, in
// place, as check's recover wraps an analyzer, since the checker keys every result by the analyzer's own pointer. Only
// the package's initialization calls it, so it runs once a process, before any pass could read an analyzer it wraps.
func markedSyntaxOnlyWalks() bool {
	walks, walksError := syntaxOnlyWalksOf(All(), syntaxOnlyWalks)
	if walksError != nil {
		// Invariant: syntaxOnlyWalks names registered rules the mark holds for, which
		// TestTheRegistrysSyntaxOnlyWalksHold keeps, so a refusal is a mistake in building the registry, never in a
		// package, and panicking as the package initializes fails every test before any rule runs.
		panic("marking the syntax-only walks: " + walksError.Error())
	}
	for _, walk := range walks {
		walk.Analyzer.Run = rule.SyntaxOnlyRun(walk.Name, walk.Analyzer.Run)
	}
	return true
}

// syntaxOnlyWalksOf is the rules of registered that marked names, in registered's order, refusing a name no rule of
// registered has and a rule the mark cannot hold for (rule.ValidateSyntaxOnlyWalk).
func syntaxOnlyWalksOf(registered []rule.Rule, marked map[string]bool) ([]rule.Rule, error) {
	walks := []rule.Rule{}
	for _, each := range registered {
		if !marked[each.Name] {
			continue
		}
		if validateError := rule.ValidateSyntaxOnlyWalk(each); validateError != nil {
			return nil, validateError
		}
		walks = append(walks, each)
	}
	for name := range marked {
		if !slices.ContainsFunc(walks, func(walk rule.Rule) bool { return walk.Name == name }) {
			return nil, fmt.Errorf("rule %q is marked a syntax-only walk and no rule is registered under it", name)
		}
	}
	return walks, nil
}
