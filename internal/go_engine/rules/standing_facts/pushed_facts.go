package standing_facts

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/printf"
)

// pushedReasons are why a function was named for a pushed fact of the analyzer each is keyed by, which the build's
// fact differs from the host's in (#5vbs1np, must 2).
var pushedReasons = map[string]fact_reach.Reason{
	printf.Analyzer.Name: "printf wrapper",
}

// pushedFacts are the build's own pushed facts the pass reads, beside ctrlflow's no-return: printf's wrapper facts,
// its result on the package, which holds the facts of the package's own functions and those its imports exported
// that its pass looked up, as the host's capture holds them (check.FactReachPackage.Pushed).
//
// The deprecation facts are pushed too, and are never compared here, by an argument rather than a comparison (agreed
// in B's design, #5vbs1np). A deprecation fact is read from its object's declaration's doc comment and nothing else
// (honnef.co/go/tools v0.8.1, analysis/facts/deprecated/deprecated.go), so it differs between two builds only where the
// declaration's text does, or where the declaration sits in a file only one build compiles. Either way the object's
// identity, which holds its file and is compared by the reach at body grain (#w72zxtq), differs, or its file's bytes
// do, and the reach lints every body that uses the object before any fact is compared: no standing body reads a
// deprecation that differs. TestADeprecationThatDiffersIsTakenOutByTheReach shows it for a function deprecated in one
// platform's file alone, and the body differential's second leg compares each standing function's own deprecation
// with the host's on every census, a difference failing it.
type pushedFacts struct {
	wrappers *printf.Wrappers
}

// words is the build's fact of function learned by the analyzer named analyzer, in the fact's words as the host's
// capture writes them (fmt.Sprint), empty for no fact; seen, whether the build's pass looked the fact up at all; and
// read, whether the pass reads the analyzer's facts for function. A function printf's pass never looked up is not
// seen: no call of the package names it, which holds of a callee only a stripped body calls once standing bodies are
// stripped, so its fact cannot be checked, and reads unknown (round 2's N2). A print function known by name is not
// read: its kind is its name's in every build, and no fact of it is.
func (pushed pushedFacts) words(analyzer string, function *types.Func) (words string, seen bool, read bool) {
	if analyzer == printf.Analyzer.Name && !pushed.wrappers.ByName(function) {
		words, seen = pushed.wrappers.Of(function)
		return words, seen, true
	}
	return "", false, false
}
