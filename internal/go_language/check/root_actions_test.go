package check

import (
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/check/checker"
)

// The gather reads each analyzer's action on each package from the last graph that ran one, since the second wave
// analyzes again a package of the first that it could not restore, and reads none for a package no graph ran it on.
// A root on a package not analyzed, as a first wave's on a package below them, is passed over, as is one of an analyzer
// not among the roots.
func TestTheGatherReadsTheLastWavesActionOnAPackage(test *testing.T) {
	test.Parallel()

	first, second := &analysis.Analyzer{Name: "first"}, &analysis.Analyzer{Name: "second"}
	stranger := &analysis.Analyzer{Name: "stranger"}
	analyzedPackage, untouched := &packages.Package{ID: "analyzed"}, &packages.Package{ID: "untouched"}
	below := &packages.Package{ID: "below"}
	firstWave := &checker.Action{Analyzer: first, Package: analyzedPackage}
	secondWave := &checker.Action{Analyzer: first, Package: analyzedPackage}
	graphs := []*checker.Graph{
		{Roots: []*checker.Action{firstWave, {Analyzer: second, Package: below}, {Analyzer: stranger,
			Package: analyzedPackage}}},
		{Roots: []*checker.Action{secondWave}},
	}
	actions := rootActions([]*analysis.Analyzer{first, second}, []*packages.Package{analyzedPackage, untouched}, graphs)
	if actions[0][0] != secondWave {
		test.Errorf("the gather read the first wave's action on %s, want the second's", analyzedPackage.ID)
	}
	for _, missing := range []struct {
		analyzerIndex int
		packageIndex  int
	}{{0, 1}, {1, 0}, {1, 1}} {
		if action := actions[missing.analyzerIndex][missing.packageIndex]; action != nil {
			test.Errorf("the gather read %s on %s, which no graph ran there", action.Analyzer.Name, action.Package.ID)
		}
	}
}
