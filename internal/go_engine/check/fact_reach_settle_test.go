package check

import (
	"encoding/hex"
	"errors"
	"io/fs"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/packages"
	"honnef.co/go/tools/analysis/facts/deprecated"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_facts"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/printf"
)

// settlingAnalyzer is an analyzer the facts' reach could settle by, for one test, as standing_facts would be: no rule,
// requiring inspect, so what it requires runs on the package too, its result there the package's path, and each run
// counted in runs. One that fails returns an error on every package instead.
func settlingAnalyzer(name string, runs *atomic.Int32, fails bool) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:       name,
		Doc:        "hands back its package's path, or fails",
		Requires:   []*analysis.Analyzer{inspect.Analyzer},
		ResultType: reflect.TypeFor[string](),
		Run: func(pass *analysis.Pass) (any, error) {
			runs.Add(1)
			if fails {
				return nil, errors.New("settling " + pass.Pkg.Path() + " failed")
			}
			return pass.Pkg.Path(), nil
		},
	}
}

// settledPaths is each of analyzed's path by its ID, the results settlingAnalyzer hands back on all of them.
func settledPaths(analyzed []*packages.Package) map[string]any {
	paths := map[string]any{}
	for _, analyzedPackage := range analyzed {
		paths[analyzedPackage.ID] = analyzedPackage.PkgPath
	}
	return paths
}

// withoutSettled is result with what only Options.FactReachSettle gives left out, and what withoutTimesOrCache leaves
// out, so a run with it compares with one without.
func withoutSettled(result *Result) Result {
	compared := withoutTimesOrCache(result)
	compared.FactReachSettled, compared.FactReachCrashes, compared.FactReachUnsettled = nil, nil, ""
	return compared
}

// storedKeys is the key of every file a store under directory holds under a key, each named for it.
func storedKeys(test *testing.T, directory string) map[cache.Key]bool {
	test.Helper()
	keys := map[cache.Key]bool{}
	walkError := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil || entry.IsDir() {
			return pathError
		}
		var key cache.Key
		decoded, decodeError := hex.DecodeString(entry.Name())
		if decodeError != nil || len(decoded) != len(key) {
			// The cache directory's own files, as its .gitignore, which no run's analysis writes.
			return nil
		}
		copy(key[:], decoded)
		keys[key] = true
		return nil
	})
	if walkError != nil {
		test.Fatalf("reading the store under %s: %v", directory, walkError)
	}
	return keys
}

// requireSameStores fails unless the store under settledDirectory, settledCaching's, holds the keys the one under
// plainDirectory, plainCaching's, holds, and under each the same entry, compared decoded, since two runs alike encode
// an entry's bytes in an order of their own.
func requireSameStores(
	test *testing.T,
	plainDirectory string,
	plainCaching *Caching,
	settledDirectory string,
	settledCaching *Caching,
) {
	test.Helper()
	plainKeys, settledKeys := storedKeys(test, plainDirectory), storedKeys(test, settledDirectory)
	if len(plainKeys) == 0 || !maps.Equal(settledKeys, plainKeys) {
		test.Errorf("with the analyzer the store holds the keys %v, want those of the run without it, %v",
			settledKeys, plainKeys)
	}
	for key := range plainKeys {
		plainEntry, _, plainHolds := storedEntryAt(plainCaching.Store, key)
		settledEntry, _, settledHolds := storedEntryAt(settledCaching.Store, key)
		if settledHolds != plainHolds || !reflect.DeepEqual(settledEntry, plainEntry) {
			test.Errorf("with the analyzer the store holds under %s\n%+v\nwant the run's without it\n%+v", key,
				settledEntry, plainEntry)
		}
	}
}

// Run roots Options.FactReachSettle beside the rules on every package it analyzes and hands back its result on each,
// by package ID (#aswfm7a): the findings, their counts by rule and everything else of the run are the same as a run's
// without it, which hands back none.
func TestRunRootsTheFactReachAnalyzerBesideTheRulesAndHandsBackItsResults(test *testing.T) {
	test.Parallel()

	// reachProbe's rule requires ctrlflow, without which a run settles nothing (settlingIn).
	rules := []rule.Rule{houseRule(targetAnalyzer), packageClauseRule, houseRule(reachProbeAnalyzer)}
	var runs atomic.Int32
	without := runRules(test, Options{}, rules...)
	with := runRules(test, Options{FactReachSettle: settlingAnalyzer("settling_live", &runs, false)}, rules...)
	if want := settledPaths(fixtures.loaded.Packages); !maps.Equal(with.FactReachSettled, want) {
		test.Errorf("the run settled %v, want every analyzed package's path %v", with.FactReachSettled, want)
	}
	if len(with.FactReachCrashes) > 0 || len(with.Crashes) > 0 {
		test.Errorf("the run crashed: %+v, %+v", with.FactReachCrashes, with.Crashes)
	}
	if without.FactReachSettled != nil {
		test.Errorf("a run without the analyzer settled %v", without.FactReachSettled)
	}
	if len(without.Findings) == 0 || !reflect.DeepEqual(withoutSettled(with), withoutSettled(without)) {
		test.Errorf("with the analyzer the run is\n%+v\nwant the run without it\n%+v", withoutSettled(with),
			withoutSettled(without))
	}
	if _, runError := Run(fixtures.loaded.Packages, rules, Options{FactReachSettle: targetAnalyzer}); runError == nil {
		test.Error("a rule's analyzer was taken as the one the facts' reach settles by")
	}
}

// With the findings cache, a cold run that roots Options.FactReachSettle writes the same entries under the same keys
// as a cold run without it, in the two waves both take, the analyzer running once a package (#aswfm7a). A warm run
// restores every package, so the analyzer runs on none and hands back nothing.
func TestTheFactReachAnalyzerChangesNoEntryAndNeverRunsOnARestoredPackage(test *testing.T) {
	test.Parallel()

	loadedFixtures(test)
	counts := &runCounts{}
	rules := []rule.Rule{countedRule("countedSettled", counts), houseRule(targetAnalyzer), packageClauseRule,
		houseRule(markedAnalyzer), houseRule(reachProbeAnalyzer)}
	plainDirectory, settledDirectory := test.TempDir(), test.TempDir()
	plainCaching, settledCaching := cachingAt(plainDirectory, rules), cachingAt(settledDirectory, rules)
	var runs atomic.Int32
	settling := settlingAnalyzer("settling_cached", &runs, false)
	run := func(analyzed []*packages.Package, caching *Caching, settle *analysis.Analyzer) *Result {
		test.Helper()
		result, runError := Run(analyzed, rules, Options{Cache: NewCacheSession(caching, rules),
			FactReachSettle: settle})
		if runError != nil {
			test.Fatalf("running %d rules: %v", len(rules), runError)
		}
		return result
	}
	plain := run(ownLoad(test, "."), plainCaching, nil)
	analyzed := ownLoad(test, ".")
	settled := run(analyzed, settledCaching, settling)
	packageCount := len(analyzed)

	if settled.Cache.Waves != 2 || !reflect.DeepEqual(settled.Cache, plain.Cache) {
		test.Errorf("the cold run with the analyzer cached %+v, want two waves, as the run without it, %+v",
			settled.Cache, plain.Cache)
	}
	if !reflect.DeepEqual(withoutSettled(settled), withoutSettled(plain)) {
		test.Errorf("with the analyzer the cold run is\n%+v\nwant the run without it\n%+v", withoutSettled(settled),
			withoutSettled(plain))
	}
	requireSameStores(test, plainDirectory, plainCaching, settledDirectory, settledCaching)
	if want := settledPaths(analyzed); !maps.Equal(settled.FactReachSettled, want) ||
		runs.Load() != int32(packageCount) {
		test.Errorf("the cold run settled %v in %d runs, want %v, once a package", settled.FactReachSettled,
			runs.Load(), want)
	}

	warm := run(analyzed, settledCaching, settling)
	if warm.Cache.Hit != packageCount || runs.Load() != int32(packageCount) || len(warm.FactReachSettled) > 0 ||
		len(warm.FactReachCrashes) > 0 {
		test.Errorf("the warm run hit %d of %d packages, ran the analyzer %d more times, settled %v and crashed %+v, "+
			"want every package restored and nothing settled", warm.Cache.Hit, packageCount,
			runs.Load()-int32(packageCount), warm.FactReachSettled, warm.FactReachCrashes)
	}
}

// standing_facts requires printf and so runs it, and every analyzer printf requires, on every package it is rooted on,
// though no enabled rule requires printf (#5vbs1np, must 2): their facts and results reach no entry and no key, so a
// cold run rooting it writes the same entries under the same keys as one without it, with rules that leave printf
// and SA1019 out. With no host's side handed, it settles nothing, and hands back an empty result on each package.
//
// It requires ctrlflow too, whose run on a package has the resolver learn it, and the save then writes the package's
// answers, each kind complete and empty, into an entry a run without it writes none of. So a run whose rules require
// no ctrlflow roots it nowhere and says why, and its entries are the plain run's too; with reachProbe's rule, which
// requires ctrlflow, as every real run's rules do, it is rooted on every package.
func TestStandingFactsRunsPrintfForItselfAndChangesNoEntry(test *testing.T) {
	test.Parallel()

	loadedFixtures(test)
	for _, each := range []struct {
		name     string
		ctrlflow bool
	}{{"without ctrlflow", false}, {"with ctrlflow", true}} {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()
			counts := &runCounts{}
			rules := []rule.Rule{countedRule("countedStanding", counts), houseRule(targetAnalyzer),
				packageClauseRule, houseRule(markedAnalyzer)}
			if each.ctrlflow {
				rules = append(rules, houseRule(reachProbeAnalyzer))
			}
			ranCtrlflow := false
			for _, enabled := range rules {
				if requiresAnalyzer(enabled.Analyzer, printf.Analyzer) ||
					requiresAnalyzer(enabled.Analyzer, deprecated.Analyzer) {
					test.Fatalf("the rule %s is or requires printf or the deprecation facts", enabled.Name)
				}
				ranCtrlflow = ranCtrlflow || requiresAnalyzer(enabled.Analyzer, ctrlflow.Analyzer)
			}
			if ranCtrlflow != each.ctrlflow {
				test.Fatalf("the rules require ctrlflow %t, want %t", ranCtrlflow, each.ctrlflow)
			}
			plainDirectory, settledDirectory := test.TempDir(), test.TempDir()
			plainCaching, settledCaching := cachingAt(plainDirectory, rules), cachingAt(settledDirectory, rules)
			run := func(analyzed []*packages.Package, caching *Caching, settle *analysis.Analyzer) *Result {
				test.Helper()
				result, runError := Run(analyzed, rules, Options{Cache: NewCacheSession(caching, rules),
					FactReachSettle: settle})
				if runError != nil {
					test.Fatalf("running %d rules: %v", len(rules), runError)
				}
				return result
			}
			plain := run(ownLoad(test, "."), plainCaching, nil)
			analyzed := ownLoad(test, ".")
			settled := run(analyzed, settledCaching, standing_facts.Analyzer)
			if each.ctrlflow {
				// A package the second wave analyzes over an import the first wave's entry restores gets no printf
				// facts of it, the rule being off, so its printf fails, and standing_facts with it: a crash, which
				// lints the package whole, never a finding.
				if len(settled.FactReachSettled) == 0 ||
					len(settled.FactReachSettled)+len(settled.FactReachCrashes) != len(analyzed) ||
					settled.FactReachUnsettled != "" {
					test.Errorf("standing_facts settled %d of %d packages, crashing on %+v, unsettled %q, want each "+
						"settled or crashed, one settled at least", len(settled.FactReachSettled), len(analyzed),
						settled.FactReachCrashes, settled.FactReachUnsettled)
				}
			} else if settled.FactReachSettled != nil || settled.FactReachUnsettled != withoutCtrlflow {
				test.Errorf("with no rule requiring ctrlflow standing_facts settled %v, unsettled %q, want nothing, "+
					"for %q", settled.FactReachSettled, settled.FactReachUnsettled, withoutCtrlflow)
			}
			if !reflect.DeepEqual(settled.Cache, plain.Cache) || !reflect.DeepEqual(withoutSettled(settled),
				withoutSettled(plain)) {
				test.Errorf("with standing_facts the cold run is\n%+v\nwant the run without it\n%+v",
					withoutSettled(settled), withoutSettled(plain))
			}
			requireSameStores(test, plainDirectory, plainCaching, settledDirectory, settledCaching)
		})
	}
}

// Options.FactReachSettle failing on every package fails no run and is no finding (#aswfm7a): each package is in
// Result.FactReachCrashes, never in Crashes, and its entry is written as a run's without the analyzer.
func TestTheFactReachAnalyzerFailingIsNoCrashOfTheRun(test *testing.T) {
	test.Parallel()

	loadedFixtures(test)
	rules := []rule.Rule{houseRule(targetAnalyzer), packageClauseRule, houseRule(reachProbeAnalyzer)}
	var runs atomic.Int32
	analyzed := ownLoad(test, ".")
	failed, runError := Run(analyzed, rules, Options{Cache: NewCacheSession(testCaching(test, rules), rules),
		FactReachSettle: settlingAnalyzer("settling_failed", &runs, true)})
	if runError != nil {
		test.Fatalf("a failing analyzer failed the run: %v", runError)
	}
	plain := runCached(test, ownLoad(test, "."), testCaching(test, rules), rules)
	if len(failed.FactReachCrashes) != len(analyzed) || len(failed.FactReachSettled) > 0 || len(failed.Crashes) > 0 {
		test.Errorf("the failing analyzer left the crashes %+v, settled %v and the run's crashes %+v, want one of "+
			"its own a package and nothing else", failed.FactReachCrashes, failed.FactReachSettled, failed.Crashes)
	}
	if failed.Cache.Written != len(analyzed) || !reflect.DeepEqual(withoutSettled(failed), withoutSettled(plain)) {
		test.Errorf("with the failing analyzer the run is\n%+v\nwant the run without it\n%+v", withoutSettled(failed),
			withoutSettled(plain))
	}
}

// An extra build restoring the write-through fixture's packages from the host's run analyzes them by restoring them,
// so it roots Options.FactReachSettle on none of them, and no restore is asked of it (#aswfm7a): nothing it restored is
// settled, and nothing crashed.
func TestTheFactReachAnalyzerIsRootedOnNoPackageRestoredFromTheHost(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, throughFiles)
	stores := test.TempDir()
	host := throughHost(test, root, filepath.Join(stores, "host"))
	rules := throughRules()
	session := NewCacheSession(windowsCachingAt(filepath.Join(stores, "build"), rules), rules)
	session.RestoreFromHost(host)
	loaded, loadError := load.PackagesCached(root, []string{"./..."}, []string{"GOWORK=off", "GOOS=windows"}, nil,
		load.Caching{Restorer: session})
	if loadError != nil || len(loaded.Diagnostics) > 0 {
		test.Fatalf("loading windows's build: %v %+v", loadError, loaded.Diagnostics)
	}
	var runs atomic.Int32
	build, runError := Run(loaded.Packages, rules, Options{Cache: session, Withheld: loaded.Withheld,
		Bodiless: loaded.Bodiless, FactReachSettle: settlingAnalyzer("settling_hosted", &runs, false)})
	if runError != nil {
		test.Fatalf("running windows's build: %v", runError)
	}
	if !slices.Contains(build.FromHost.Restored, throughBasePath) {
		test.Fatalf("windows restored %q from the host, want %s among them", build.FromHost.Restored, throughBasePath)
	}
	for _, settled := range build.FactReachSettled {
		settledPath, isPath := settled.(string)
		if !isPath {
			test.Fatalf("windows settled %v, a %T, want the package's path settlingAnalyzer gives", settled, settled)
		}
		if slices.Contains(build.FromHost.Restored, settledPath) {
			test.Errorf("windows settled %s, which it restored from the host", settledPath)
		}
	}
	if len(build.FactReachCrashes) > 0 || len(build.Crashes) > 0 || runs.Load() != int32(len(build.FactReachSettled)) {
		test.Errorf("windows's build crashed %+v and %+v, and ran the analyzer %d times for %d settled, want neither "+
			"crash and a run for each", build.FactReachCrashes, build.Crashes, runs.Load(), len(build.FactReachSettled))
	}
}
