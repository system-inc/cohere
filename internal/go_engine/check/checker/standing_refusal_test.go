package checker

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// refusalAnalyzer is an analyzer named name that requires required and reports nothing.
func refusalAnalyzer(name string, required ...*analysis.Analyzer) *analysis.Analyzer {
	return &analysis.Analyzer{Name: name, Doc: "requires what it is given and reports nothing", Requires: required,
		Run: func(*analysis.Pass) (any, error) { return nil, nil }}
}

// Analyze refuses an Options.StandingAfter that is a follower, an analyzer Options.FollowsStanding names, or that
// requires one directly or through what it requires, with an error naming it and the follower, since the edge it adds
// would have that follower's action wait on itself (#e4rd5dw). A follower need be no dispatcher, as the build of the
// representation is none: one Options.Tolerant does not name is refused as a dispatcher is. An analyzer that requires
// no follower is taken, and the graph analyzed.
func TestAnalyzeRefusesAStandingAfterThatRequiresAFollower(test *testing.T) {
	test.Parallel()
	roots := timedModule(test)
	dispatcher := refusalAnalyzer("dispatcher")
	representation := refusalAnalyzer("representation")
	tolerant := func(analyzer *analysis.Analyzer) bool { return analyzer == dispatcher }
	follows := func(analyzer *analysis.Analyzer) bool { return analyzer == dispatcher || analyzer == representation }
	options := func(after *analysis.Analyzer) *Options {
		return &Options{Standing: standingKey, Tolerant: tolerant, FollowsStanding: follows, StandingAfter: after,
			StandingBodies: func(string, any) rule.StandingBodies { return nil }}
	}
	for _, follower := range []*analysis.Analyzer{dispatcher, representation} {
		middle := refusalAnalyzer("middle_"+follower.Name, follower)
		for _, refused := range []*analysis.Analyzer{
			follower,
			refusalAnalyzer("direct_"+follower.Name, follower),
			refusalAnalyzer("through_"+follower.Name, middle),
		} {
			_, analyzeError := Analyze([]*analysis.Analyzer{dispatcher, representation}, roots, options(refused))
			if analyzeError == nil || !strings.Contains(analyzeError.Error(), refused.Name+", requires the follower "+
				follower.Name) {
				test.Errorf("a StandingAfter %s was analyzed with the error %v, want it refused, naming it and the "+
					"follower %s", refused.Name, analyzeError, follower.Name)
			}
		}
	}
	if _, analyzeError := Analyze([]*analysis.Analyzer{dispatcher, representation}, roots,
		options(refusalAnalyzer("free"))); analyzeError != nil {
		test.Errorf("a StandingAfter that requires no follower was refused: %v", analyzeError)
	}
}
