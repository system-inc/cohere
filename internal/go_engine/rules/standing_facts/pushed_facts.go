package standing_facts

import (
	"fmt"
	"go/types"

	"honnef.co/go/tools/analysis/facts/deprecated"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/printf"
)

// pushedReasons are why a function was named for a pushed fact of the analyzer each is keyed by, which the build's
// fact differs from the host's in (#5vbs1np, must 2).
var pushedReasons = map[string]fact_reach.Reason{
	printf.Analyzer.Name:     "printf wrapper",
	deprecated.Analyzer.Name: "deprecation",
}

// pushedFacts are the build's own pushed facts the pass reads, beside ctrlflow's no-return, as the host's capture
// holds them (check.FactReachPackage.Pushed): printf's wrapper facts, its result on the package, which holds the facts
// of the package's own functions and those its imports exported that its pass looked up; and the deprecation facts,
// the deprecated facts analyzer's result on the package, which lists every deprecation of the package and its import
// closure. reads are the package's reads, where each deprecation asked of another package's function is recorded
// (deprecation), and home is the package.
//
// The deprecation facts were once left to the reach at body grain by a written argument: a deprecation is read from
// its declaration's doc comment, so it would differ between builds only where the declaration's text or file does,
// which the reach compares. It was false (#7v2bzvd): the analyzer carries a deprecation paragraph above an empty group,
// `var ()`, to the next declaration it meets, in that file or the next, so a function of a shared file is deprecated in
// exactly the builds that compile the paragraph's platform file, its text and file the same in each. So they are
// compared as the wrapper facts are, function by function.
type pushedFacts struct {
	wrappers     *printf.Wrappers
	deprecations deprecated.Result
	reads        *build_reads.Reads
	home         *types.Package
}

// words is the build's fact of function learned by the analyzer named analyzer, in the fact's words as the host's
// capture writes them (fmt.Sprint), empty for no fact; seen, whether the build's pass looked the fact up at all; and
// read, whether the pass reads the analyzer's facts for function. A function printf's pass never looked up is not
// seen: no call of the package names it, which holds of a callee only a stripped body calls once standing bodies are
// stripped, so its fact cannot be checked, and reads unknown (round 2's N2). A print function known by name is not
// read: its kind is its name's in every build, and no fact of it is. Every deprecation is seen, since the deprecated
// facts' result lists every one of the import closure.
func (pushed pushedFacts) words(analyzer string, function *types.Func) (words string, seen bool, read bool) {
	switch {
	case analyzer == printf.Analyzer.Name && !pushed.wrappers.ByName(function):
		words, seen = pushed.wrappers.Of(function)
		return words, seen, true
	case analyzer == deprecated.Analyzer.Name:
		return pushed.deprecation(function), true, true
	}
	return "", false, false
}

// deprecation is the build's deprecation fact of function, a fact about the generic declaration, in the fact's words,
// empty for none. Where function is another package's, the question and its answer are recorded in the package's
// reads, as deprecated_use's deprecationOf records each it asks: the result is a listing of every fact of the import
// closure, which the checker lets be read only one recorded question at a time (check's listingsRead). The package's
// own function's deprecation is read off its own syntax, no read of another package.
func (pushed pushedFacts) deprecation(function *types.Func) string {
	declared := function.Origin()
	deprecation, isDeprecated := pushed.deprecations.Objects[declared]
	if declared.Pkg() != nil && declared.Pkg() != pushed.home {
		lookup := build_reads.Lookup{Query: build_reads.QueryDeprecated, Path: declared.Pkg().Path(), Subject: declared,
			Found: isDeprecated}
		if isDeprecated {
			lookup.Text = deprecation.Msg
		}
		pushed.reads.AddLookup(lookup)
	}
	if !isDeprecated {
		return ""
	}
	return fmt.Sprint(deprecation)
}
