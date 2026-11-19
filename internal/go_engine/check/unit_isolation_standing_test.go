package check

import (
	"maps"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// isolationStanding is one package's standing bodies in the production shape of the unit isolation (#e4rd5dw): the
// units of the seeded half stand, every isolated rule is confined, and the host's findings of each rule on each file
// are the whole run's, as an extra build takes them from the host's run. It counts what the rules took.
type isolationStanding struct {
	stubbed []functionUnit
	whole   []Finding

	lock         sync.Mutex
	declarations map[string]int
	taken        int
}

// Stands is whether the function whose `func` is at offset of filename is one of the seeded half.
func (standing *isolationStanding) Stands(filename string, offset int) bool {
	return slices.ContainsFunc(standing.stubbed, func(unit functionUnit) bool {
		return unit.start == unitAt{file: filename, offset: offset}
	})
}

// Confines answers that every rule is confined: only the isolated rules run.
func (*isolationStanding) Confines(string) bool { return true }

// Findings is the whole run's findings of the rule named ruleName on filename, every one, as the host's entry holds
// them.
func (standing *isolationStanding) Findings(ruleName string, filename string) ([]rule.InheritedFinding, bool) {
	findings := []rule.InheritedFinding{}
	for _, finding := range standing.whole {
		if finding.Rule == ruleName && finding.File == filename {
			findings = append(findings, inheritedFinding(finding))
		}
	}
	return findings, true
}

// Took counts the declarations that stood in filename for a rule, the same for every rule, and the findings it took.
func (standing *isolationStanding) Took(_ string, filename string, declarations int, findings int) {
	standing.lock.Lock()
	defer standing.lock.Unlock()
	standing.declarations[filename] = declarations
	standing.taken += findings
}

// The unit isolation in the production shape (#e4rd5dw): every isolated rule, over the test's own fixture, finds what
// it finds over the whole package when a seeded random half of the package's function units stands, withheld through
// checker.Options.StandingBodies rather than stubbed, so every body is still in place and only the dispatcher's deaf
// walk and the swap keep a confined rule from hearing it, and the host's findings inside the standing declarations are
// the whole run's. The standing run sees the same findings everywhere, kept units and standing ones alike, and the
// rules took the host's findings in at least one standing declaration under each seed.
func TestEveryIsolatedRuleFindsTheWholeRunsFindingsWithHalfItsUnitsStanding(test *testing.T) {
	test.Parallel()
	isolated := isolatedRules(test)
	fixture := gopathFixture(test, "unit_isolation", filepath.Join("testdata", "unit_isolation"))
	whole, wholeError := load.Packages(fixture.directory, fixture.patterns, fixture.environment, nil)
	if wholeError != nil || len(whole.Roots) != 1 {
		test.Fatalf("loading the fixture: %v, %d roots", wholeError, len(whole.Roots))
	}
	wholeRun, runError := runIsolated(isolated, whole.Roots, nil)
	if runError != nil {
		test.Fatal(runError)
	}
	wholeFound := keptFindings(wholeRun.findings[whole.Roots[0]], nil)
	if len(wholeFound) == 0 {
		test.Fatal("the whole package has no findings, so the isolation compares nothing")
	}
	for _, seed := range unitIsolationSeeds {
		// A load of its own for each seed, since a run lets go of its dependencies' syntax, and a second run over them
		// could learn no fact there.
		fresh, freshError := load.Packages(fixture.directory, fixture.patterns, fixture.environment, nil)
		if freshError != nil || len(fresh.Roots) != 1 {
			test.Fatalf("seed %d: loading the fixture again: %v", seed, freshError)
		}
		analyzed := fresh.Roots[0]
		standing := &isolationStanding{stubbed: stubbedHalf(functionUnits(analyzed), analyzed.ID, seed),
			whole: wholeRun.checked[whole.Roots[0]], declarations: map[string]int{}}
		standingRun, standingError := runIsolatedWith(isolated, []*packages.Package{analyzed}, nil,
			func(options *checker.Options) {
				options.StandingBodies = func(packageId string, _ any) rule.StandingBodies {
					if packageId != analyzed.ID {
						return nil
					}
					return standing
				}
			})
		if standingError != nil {
			test.Fatalf("seed %d: %v", seed, standingError)
		}
		for _, ruleName := range slices.Sorted(maps.Keys(standingRun.crashed[analyzed])) {
			test.Errorf("seed %d: %s crashed with half the units standing: %s", seed, ruleName,
				standingRun.crashed[analyzed][ruleName])
		}
		standingFound := keptFindings(standingRun.findings[analyzed], nil)
		units := functionUnits(analyzed)
		for _, line := range slices.Sorted(maps.Keys(wholeFound)) {
			if standingFound[line] != wholeFound[line] {
				test.Errorf("seed %d, in %s: the whole package finds %s %d times, with half its units standing %d",
					seed, unitNamed(units, findingPlace(wholeRun.findings[whole.Roots[0]], line)), line,
					wholeFound[line], standingFound[line])
			}
		}
		for _, line := range slices.Sorted(maps.Keys(standingFound)) {
			if wholeFound[line] == 0 {
				test.Errorf("seed %d, in %s: only the package with half its units standing finds %s", seed,
					unitNamed(units, findingPlace(standingRun.findings[analyzed], line)), line)
			}
		}
		stood := 0
		for _, declarations := range standing.declarations {
			stood += declarations
		}
		if stood == 0 || standing.taken == 0 {
			test.Errorf("seed %d: the rules took %d findings in %d standing declarations, want some of each", seed,
				standing.taken, stood)
		}
	}
}
