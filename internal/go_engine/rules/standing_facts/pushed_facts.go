package standing_facts

import (
	"fmt"
	"maps"
	"slices"

	"github.com/system-inc/go-types/types"
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

// localWrapperReason is why a variable or a field of another package was named whatever its facts read: printf may
// hold a kind of it found in the package alone, which no fact carries (fact_reach.PushedObject.Local).
const localWrapperReason fact_reach.Reason = "local printf wrapper"

// pushedFacts are the build's own pushed facts the pass reads, beside ctrlflow's no-return, as the host's capture
// holds them (check.FactReachPackage.Pushed): printf's wrapper facts, its result on the package, which holds the facts
// of the package's own functions, interface methods and variables and those its imports exported that its pass looked
// up; and the deprecation facts, the deprecated facts analyzer's result on the package, which lists every deprecation
// of the package and its import closure. reads are the package's reads, where each deprecation asked of another
// package's function is recorded (deprecation), and home is the package. A wrapper fact needs no such record: printf's
// result is no listing, holding only the facts its pass imported, each read the checker records already.
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

// differing compares hosted, the host's pushed facts of the object under key by the name of the analyzer that learns
// them, with the build's own of own, the build's object for it, nil where the build has none, whose every fact then
// reads unknown. It is how many facts it compared, and the namings of key for each that differs from the host's or
// that the build cannot read, under its analyzer's reason, in the analyzers' order.
//
// ownUnlookedEmpty is whether an object of the package's own that the build's printf never looked up reads as holding
// no fact, rather than unknown, as an interface method, a variable or a field does (fact_reach.PushedObject). The
// pass's search is the one place a wrapper fact of its own package's object is made (wrapper_search.go's propagate),
// and every object it makes one of is among those Wrappers holds, so one it never looked up holds no fact in the
// build: a body that only takes it as a value, `_ = logger.Logf`, stands where the host's fact is empty too (seats'
// review-1, a should). A function's is left unknown, as #9y28qqr weighs.
func (pushed pushedFacts) differing(
	key fact_reach.Object,
	hosted map[string]string,
	own types.Object,
	ownUnlookedEmpty bool,
) (int, []fact_reach.Named) {
	compared := 0
	var named []fact_reach.Named
	for _, analyzer := range slices.Sorted(maps.Keys(hosted)) {
		words, isSeen, isRead := "", false, true
		if own != nil {
			words, isSeen, isRead = pushed.words(analyzer, own)
			isSeen = isSeen || ownUnlookedEmpty && own.Pkg() == pushed.home
		}
		if !isRead {
			continue
		}
		compared++
		switch {
		case !isSeen:
			named = append(named, fact_reach.Named{Object: key, Reason: "unknown " + pushedReasons[analyzer]})
		case words != hosted[analyzer]:
			named = append(named, fact_reach.Named{Object: key, Reason: pushedReasons[analyzer]})
		}
	}
	return compared, named
}

// words is the build's fact of object learned by the analyzer named analyzer, in the fact's words as the host's
// capture writes them (fmt.Sprint), empty for no fact; seen, whether the build's pass looked the fact up at all; and
// read, whether the pass reads the analyzer's facts for object. An object printf's pass never looked up is not seen:
// no call of the package names it, which holds of a callee only a stripped body calls once standing bodies are
// stripped, so its fact cannot be checked, and reads unknown (round 2's N2). A print function known by name is not
// read: its kind is its name's in every build, and no fact of it is. Every deprecation of a function is seen, since the
// deprecated facts' result lists every one of the import closure; the host's side compares no other object's.
func (pushed pushedFacts) words(analyzer string, object types.Object) (words string, seen bool, read bool) {
	function, isFunction := object.(*types.Func)
	switch {
	case analyzer == printf.Analyzer.Name && !pushed.wrappers.ByName(object):
		words, seen = pushed.wrappers.Of(object)
		return words, seen, true
	case analyzer == deprecated.Analyzer.Name && isFunction:
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
