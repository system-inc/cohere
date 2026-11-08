package check

import (
	"path/filepath"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// runCounts is how many times each of a counted rule's analyzers ran.
type runCounts struct {
	rule         atomic.Int32
	prerequisite atomic.Int32
}

// countedRule is a rule of its own, for one test, whose analyzer reports every package clause and requires a
// prerequisite of its own, each counting its runs in counts: a restored package runs neither.
func countedRule(name string, counts *runCounts) rule.Rule {
	prerequisite := &analysis.Analyzer{
		Name: name + "Prerequisite",
		Doc:  "counts its runs",
		Run: func(pass *analysis.Pass) (any, error) {
			counts.prerequisite.Add(1)
			return nil, nil
		},
	}
	analyzer := &analysis.Analyzer{
		Name:     name,
		Doc:      "counts its runs and reports every package clause",
		Requires: []*analysis.Analyzer{prerequisite},
		Run: func(pass *analysis.Pass) (any, error) {
			counts.rule.Add(1)
			for _, file := range pass.Files {
				pass.Reportf(file.Name.Pos(), "counted package %s", file.Name.Name)
			}
			return nil, nil
		},
	}
	return houseRule(analyzer)
}

// testCaching is a cache in a directory of the test's own, keyed for rules.
func testCaching(test *testing.T, rules []rule.Rule) *Caching {
	test.Helper()
	return cachingAt(test.TempDir(), rules)
}

// cachingAt is a cache in directory, keyed for rules as testCaching keys one, so another process of the test binary
// can read and write the same entries.
func cachingAt(directory string, rules []rule.Rule) *Caching {
	names := make([]string, 0, len(rules))
	for _, keyed := range rules {
		names = append(names, keyed.Name)
	}
	slices.Sort(names)
	return &Caching{
		Store: cache.Open(directory),
		Context: cache.Context{
			FormatVersion:  CacheFormatVersion,
			EngineIdentity: "check tests",
			Build:          []string{"loader=source"},
			RuleSet:        names,
		},
	}
}

// runCached runs rules over analyzed with caching, failing the test if the run could not start.
func runCached(test *testing.T, analyzed []*packages.Package, caching *Caching, rules []rule.Rule) *Result {
	test.Helper()
	result, runError := Run(analyzed, rules, Options{Cache: NewCacheSession(caching, rules)})
	if runError != nil {
		test.Fatalf("running %d rules: %v", len(rules), runError)
	}
	return result
}

// withoutTimesOrCache is result with what may differ between a cold and a warm run left out: the time it took and
// what the cache did.
func withoutTimesOrCache(result *Result) Result {
	compared := *result
	compared.Elapsed, compared.Cache = 0, CacheCounts{}
	return compared
}

// ownLoad is a load of the fixtures' module, or of its generated module, that is the test's own. A run that restores
// a package it analyzes, while a package that missed imports it, lets go of the restored package's syntax, as of any
// package Analyze is not given, so a run with the cache never reads the load every other test shares.
func ownLoad(test *testing.T, module string) []*packages.Package {
	test.Helper()
	root := loadedFixtures(test)
	loaded, loadError := load.Packages(filepath.Join(root, module), []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixtures again: %v", loadError)
	}
	return loaded.Packages
}

// fixturePackage is the analyzed package of the fixtures at path.
func fixturePackage(test *testing.T, path string) *packages.Package {
	test.Helper()
	for _, analyzedPackage := range fixtures.loaded.Packages {
		if analyzedPackage.PkgPath == path {
			return analyzedPackage
		}
	}
	test.Fatalf("the fixtures have no package %s", path)
	return nil
}

// keyOf is the key caching gives the fixtures' package at path.
func keyOf(test *testing.T, caching *Caching, path string) cache.Key {
	test.Helper()
	keyContext := caching.Context
	keyContext.Build = append(append([]string{}, keyContext.Build...), "loader="+load.LoaderSource)
	keyed, keysError := cache.Keys(fixtures.loaded.Packages, keyContext, nil, cache.FileDigests{})
	if keysError != nil {
		test.Fatalf("computing the fixtures' keys: %v", keysError)
	}
	key, hasKey := keyed.Keys[fixturePackage(test, path)]
	if !hasKey {
		test.Fatalf("no key for %s", path)
	}
	return key
}

// A warm run over the same packages gives the same Result as the cold run that wrote the cache, findings, counts
// and withheld maps alike, and runs nothing: every package is a hit, and neither a rule nor what it requires runs.
func TestAWarmRunGivesTheColdRunsResultAndRunsNothing(test *testing.T) {
	test.Parallel()

	loadedFixtures(test)
	for _, warmCase := range []struct {
		name      string
		module    string
		coldWaves int
		rules     func(counts *runCounts) []rule.Rule
	}{
		{
			name:      "findings and facts",
			module:    ".",
			coldWaves: 2,
			rules: func(counts *runCounts) []rule.Rule {
				return []rule.Rule{countedRule("countedFindings", counts), houseRule(targetAnalyzer), packageClauseRule,
					houseRule(markedAnalyzer)}
			},
		},
		{
			name:      "withheld as generated",
			module:    "generated",
			coldWaves: 1,
			rules: func(counts *runCounts) []rule.Rule {
				return []rule.Rule{countedRule("countedGenerated", counts), houseRule(targetAnalyzer)}
			},
		},
	} {
		test.Run(warmCase.name, func(test *testing.T) {
			test.Parallel()

			counts := &runCounts{}
			rules := warmCase.rules(counts)
			caching := testCaching(test, rules)
			analyzed := ownLoad(test, warmCase.module)
			cold := runCached(test, analyzed, caching, rules)
			packageCount := len(analyzed)
			// Both modules' packages import nothing that misses but each other, so a cold run is one wave unless
			// one of them waits on another.
			wantCold := CacheCounts{On: true, Missed: packageCount, Written: packageCount, Waves: warmCase.coldWaves}
			if !reflect.DeepEqual(cold.Cache, wantCold) {
				test.Fatalf("want the cold run's cache %+v, got %+v", wantCold, cold.Cache)
			}
			coldRuns, coldPrerequisiteRuns := counts.rule.Load(), counts.prerequisite.Load()
			if coldRuns != int32(packageCount) || coldPrerequisiteRuns != int32(packageCount) {
				test.Fatalf("want the cold run to run the rule and its prerequisite once a package, got %d and %d",
					coldRuns, coldPrerequisiteRuns)
			}

			warm := runCached(test, analyzed, caching, rules)
			if wantWarm := (CacheCounts{On: true, Hit: packageCount}); !reflect.DeepEqual(warm.Cache, wantWarm) {
				test.Fatalf("want the warm run's cache %+v, got %+v", wantWarm, warm.Cache)
			}
			if counts.rule.Load() != coldRuns || counts.prerequisite.Load() != coldPrerequisiteRuns {
				test.Fatalf("want nothing run warm, got the rule %d more times and its prerequisite %d more",
					counts.rule.Load()-coldRuns, counts.prerequisite.Load()-coldPrerequisiteRuns)
			}
			if !reflect.DeepEqual(withoutTimesOrCache(warm), withoutTimesOrCache(cold)) {
				test.Fatalf("warm\n%+v\ncold\n%+v", withoutTimesOrCache(warm), withoutTimesOrCache(cold))
			}
			if len(cold.Findings) == 0 {
				test.Fatalf("want findings to compare, got none")
			}
		})
	}
}

// A fact base exported, restored from base's entry, reaches user's pass when user misses: marked's finding in user,
// which only that fact gives, comes back. user's entry is corrupt, so it is a miss and is analyzed again.
func TestARestoredFactReachesAnImporterThatMissed(test *testing.T) {
	test.Parallel()

	loadedFixtures(test)
	counts := &runCounts{}
	rules := []rule.Rule{houseRule(markedAnalyzer), countedRule("countedFacts", counts)}
	caching := testCaching(test, rules)
	analyzed := ownLoad(test, ".")
	cold := runCached(test, analyzed, caching, rules)
	userKey := keyOf(test, caching, "example.com/fixture/user")
	if putError := caching.Store.Put(userKey, []byte("corrupt")); putError != nil {
		test.Fatalf("corrupting user's entry: %v", putError)
	}
	coldRuns := counts.rule.Load()

	warm := runCached(test, analyzed, caching, rules)
	wantWarm := CacheCounts{On: true, Hit: 1, Missed: 1, Written: 1, Waves: 1}
	if !reflect.DeepEqual(warm.Cache, wantWarm) {
		test.Fatalf("want base hit and user missed and written again, got %+v", warm.Cache)
	}
	if ran := counts.rule.Load() - coldRuns; ran != 1 {
		test.Fatalf("want the rule run again on user alone, got %d runs", ran)
	}
	var marked []Finding
	for _, found := range warm.Findings {
		if found.Rule == rule.Namespace+"test-marked" {
			marked = append(marked, found)
		}
	}
	if len(marked) != 1 || marked[0].Message != "calls Marked, which base marked" {
		test.Fatalf("want user's call to Marked reported from base's restored fact, got %+v", marked)
	}
	if !reflect.DeepEqual(withoutTimesOrCache(warm), withoutTimesOrCache(cold)) {
		test.Fatalf("warm\n%+v\ncold\n%+v", withoutTimesOrCache(warm), withoutTimesOrCache(cold))
	}
}

// A corrupt entry is a miss, never a partial hit: the package is analyzed again, and its entry written again.
func TestACorruptEntryIsAMiss(test *testing.T) {
	test.Parallel()

	loadedFixtures(test)
	counts := &runCounts{}
	rules := []rule.Rule{countedRule("countedCorrupt", counts), houseRule(markedAnalyzer)}
	caching := testCaching(test, rules)
	analyzed := ownLoad(test, ".")
	cold := runCached(test, analyzed, caching, rules)
	baseKey := keyOf(test, caching, "example.com/fixture/base")
	written, stored := caching.Store.Get(baseKey)
	if !stored {
		test.Fatalf("want base's entry written by the cold run")
	}
	// Cut short, so the gob stream ends inside the entry: a reader that kept what it decoded before the end would
	// read a partial hit.
	if putError := caching.Store.Put(baseKey, written[:len(written)-1]); putError != nil {
		test.Fatalf("corrupting base's entry: %v", putError)
	}
	coldRuns := counts.rule.Load()

	// A load of its own: the cold run's second wave restored base, which its first had analyzed, and let go of its
	// syntax, which base, analyzed again, needs.
	warm := runCached(test, ownLoad(test, "."), caching, rules)
	wantWarm := CacheCounts{On: true, Hit: 1, Missed: 1, Written: 1, Waves: 1}
	if !reflect.DeepEqual(warm.Cache, wantWarm) {
		test.Fatalf("want user hit and base missed and written again, got %+v", warm.Cache)
	}
	if ran := counts.rule.Load() - coldRuns; ran != 1 {
		test.Fatalf("want the rule run again on base alone, got %d runs", ran)
	}
	if !reflect.DeepEqual(withoutTimesOrCache(warm), withoutTimesOrCache(cold)) {
		test.Fatalf("warm\n%+v\ncold\n%+v", withoutTimesOrCache(warm), withoutTimesOrCache(cold))
	}
	if rewritten, restored := caching.Store.Get(baseKey); !restored || len(rewritten) != len(written) {
		test.Fatalf("want base's entry written whole again")
	}
}

// A package on which an action did not finish gets no entry, so the next run analyzes it again and reports the
// same crash, rather than restoring a rule's silence.
func TestAnActionErrorLeavesNoEntry(test *testing.T) {
	test.Parallel()

	loadedFixtures(test)
	rules := []rule.Rule{houseRule(markedAnalyzer), houseRule(erroringAnalyzer)}
	caching := testCaching(test, rules)
	analyzed := ownLoad(test, ".")
	cold := runCached(test, analyzed, caching, rules)
	// base fails in the first wave, so the second analyzes it again with user, and reports the crash once.
	if wantCold := (CacheCounts{On: true, Missed: 2, Waves: 2}); !reflect.DeepEqual(cold.Cache, wantCold) {
		test.Fatalf("want both packages missed and none written, got %+v", cold.Cache)
	}
	for _, path := range []string{"example.com/fixture/base", "example.com/fixture/user"} {
		if _, stored := caching.Store.Get(keyOf(test, caching, path)); stored {
			test.Errorf("want no entry for %s, whose erroring action did not finish", path)
		}
	}
	warm := runCached(test, analyzed, caching, rules)
	if len(warm.Crashes) != 2 || !reflect.DeepEqual(withoutTimesOrCache(warm), withoutTimesOrCache(cold)) {
		test.Fatalf("want the same two crashes again, got\n%+v\ncold\n%+v", warm, cold)
	}
}

// A run without the cache counts nothing and reads as off.
func TestARunWithoutTheCacheCountsNothing(test *testing.T) {
	test.Parallel()

	result := runRules(test, Options{}, houseRule(markedAnalyzer))
	if !reflect.DeepEqual(result.Cache, CacheCounts{}) {
		test.Fatalf("want no cache counts, got %+v", result.Cache)
	}
}
