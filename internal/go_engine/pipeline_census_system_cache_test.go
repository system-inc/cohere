package go_engine

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// censusBuild is what a test reads of the windows build of a run under `--build-dependence` with the body reach
// census: its run, whose census and from-source packages the pipeline keeps, and its record.
type censusBuild struct {
	run    buildRun
	record contract.BuildRecord
}

// censusOverSystemCache is the windows build of a run under `--build-dependence`, with the body reach census, over a
// fresh copy of the std-records fixture, its project's store cold, reading and writing the system cache filled holds,
// with the host's record beside it. The run records the packages each build checks from source, so a test reads which
// the build checked.
func censusOverSystemCache(test *testing.T, filled standardLibrarySource) (censusBuild, contract.BuildRecord) {
	test.Helper()
	reading := readingSystemCache(filled, "")
	censusFile := filepath.Join(test.TempDir(), "reach.txt")
	fromSource := test.TempDir()
	engine, stream, _ := runOverNoted(test, copyFixture(test, standardLibraryRecordsFixture), exitClean,
		func(engine *pipeline) {
			reading(engine)
			engine.bodyReachCensus = censusFile
			engine.bodyReachFromSourceRecord = fromSource
		}, recordsSwitches...)
	records := lintRecordOf(test, stream).Builds
	if len(engine.buildRuns) != 2 || len(records) != 2 || engine.buildRuns[1].build.OperatingSystem != "windows" {
		test.Fatalf("want the host's build and windows's, got %d runs and the records %+v", len(engine.buildRuns),
			records)
	}
	if engine.buildRuns[1].bodyReach == nil {
		test.Fatal("windows's run has no census")
	}
	return censusBuild{run: engine.buildRuns[1], record: records[1]}, records[0]
}

// reachedByLoadMode are the bodies of census the load mode rule reached first, in a signature or a body, each as the
// census lists it with what reached it: a signature's reach is written Signature, so the rule is read from the driver.
func reachedByLoadMode(census *buildBodyReach) []string {
	var reached []string
	for _, body := range census.bodies {
		if strings.Contains(body.driver, " "+string(reachedLoadMode)+" load mode in the ") {
			reached = append(reached, body.String()+": "+body.driver)
		}
	}
	return reached
}

// factsCompared is how many facts census compared, every kind's.
func factsCompared(census *contract.DependenceFacts) int {
	compared := 0
	for _, kind := range census.Kinds {
		compared += kind.Compared
	}
	return compared
}

// A build of a run judging dependence reads the system cache as a plain run's build does (#wzre5eg): once a plain run
// has written windows's standard library to the system cache, a run under `--build-dependence` over a project new to
// the machine restores every one of those entries in its windows build, as its host restores the host's, checks none
// of the standard library from source and writes nothing there. The two sides then load every standard-library
// package alike, so the load mode rule reaches no body: Shout, which names four of them, is reached by its own
// constant alone.
func TestACensusBuildRestoresTheStandardLibraryAPlainRunWrote(test *testing.T) {
	test.Parallel()

	filled, _ := filledSystemCache(test, rules.All(), engineTestsStamp, nil)
	_, plainStream, _ := runOverNoted(test, copyFixture(test, standardLibraryRecordsFixture), exitClean,
		readingSystemCache(filled, ""))
	plain := lintRecordOf(test, plainStream).Builds
	if len(plain) != 2 || plain[1].SystemCacheWritten == 0 {
		test.Fatalf("want the plain run's windows build to write its standard library to the system cache, got the "+
			"records %+v", plain)
	}
	windows, host := censusOverSystemCache(test, filled)
	if host.SystemCacheRestored == 0 || host.SystemCacheWritten != 0 {
		test.Errorf("the host restored %d standard-library packages from the system cache and wrote %d, want it to "+
			"restore its own and write none", host.SystemCacheRestored, host.SystemCacheWritten)
	}
	if windows.record.SystemCacheRestored != plain[1].SystemCacheWritten || windows.record.SystemCacheWritten != 0 {
		test.Errorf("the windows build restored %d standard-library packages from the system cache and wrote %d, "+
			"want the %d the plain run's build wrote restored and none written", windows.record.SystemCacheRestored,
			windows.record.SystemCacheWritten, plain[1].SystemCacheWritten)
	}
	if want := []string{"example.com/stdrecords"}; !slices.Equal(windows.run.fromSourceIdentifiers, want) {
		test.Errorf("the windows build checked from source %v, want %v alone, none of the standard library",
			windows.run.fromSourceIdentifiers, want)
	}
	if reached := reachedByLoadMode(windows.run.bodyReach); len(reached) != 0 {
		test.Errorf("the load mode reaches %d bodies, want none where both sides restore the standard library:\n%s",
			len(reached), strings.Join(reached, "\n"))
	}
	for _, body := range windows.run.bodyReach.bodies {
		if body.name == "Shout" && body.reason != reachedConstant {
			test.Errorf("Shout is reached as %q by %q, want by its constant mode, which windows declares apart",
				body.reason, body.driver)
		}
	}
}

// A miss still fires the load mode rule (the Square's first condition on #wzre5eg): over a system cache holding the
// host's standard library and none of windows's, the windows build of a run under `--build-dependence` checks its
// standard library from source and writes each entry, as a plain run's build does, while the host restores its own
// and writes none. The two sides then load unicode/utf8 in different modes, and Shout, whose body calls
// utf8.ValidString first, is reached by the load mode. Reading the system cache turns the rule off nowhere.
func TestACensusBuildThatMissesTheSystemCacheIsReachedByTheLoadMode(test *testing.T) {
	test.Parallel()

	filled, _ := filledSystemCache(test, rules.All(), engineTestsStamp, nil)
	windows, host := censusOverSystemCache(test, filled)
	if host.SystemCacheRestored == 0 || host.SystemCacheWritten != 0 {
		test.Errorf("the host restored %d standard-library packages from the system cache and wrote %d, want it to "+
			"restore its own and write none", host.SystemCacheRestored, host.SystemCacheWritten)
	}
	if windows.record.SystemCacheRestored != 0 || windows.record.SystemCacheWritten == 0 {
		test.Errorf("the windows build restored %d standard-library packages from the system cache and wrote %d, "+
			"want none restored and its own written", windows.record.SystemCacheRestored,
			windows.record.SystemCacheWritten)
	}
	if !slices.Contains(windows.run.fromSourceIdentifiers, "unicode/utf8") {
		test.Errorf("the windows build checked from source %v, want unicode/utf8 among them",
			windows.run.fromSourceIdentifiers)
	}
	shout := ""
	for _, body := range windows.run.bodyReach.bodies {
		if body.name == "Shout" {
			shout = string(body.reason) + ": " + body.driver
		}
	}
	if !strings.HasPrefix(shout, string(reachedLoadMode)+": unicode/utf8.ValidString ") ||
		!strings.HasSuffix(shout, " "+string(reachedLoadMode)+" load mode in the body") {
		test.Errorf("Shout is reached as %q, want by the load mode of unicode/utf8.ValidString in its body", shout)
	}
	if reached := reachedByLoadMode(windows.run.bodyReach); len(reached) != 1 {
		test.Errorf("the load mode reaches %d bodies, want Shout alone:\n%s", len(reached),
			strings.Join(reached, "\n"))
	}
}

// The facts census's record says what it left out for want of syntax (the Square's second condition on #wzre5eg).
// Over a system cache holding none of windows's standard library the build checks it from source, and the census
// compares its facts with the host's and leaves none out. Once the system cache holds it, the build of a project new
// to the machine restores it, with no syntax, and the census compares fewer facts: its record counts those it left
// out, so its 0 that covers less says so. Which facts it counts is pinned one by one beside this
// (TestTheFactsCensusCountsEachFactOfAPackageWithoutSyntax); how many the standard library has is the toolchain's.
func TestTheFactsCensusOfABuildThatRestoredSaysWhatItLeftOut(test *testing.T) {
	test.Parallel()

	filled, _ := filledSystemCache(test, rules.All(), engineTestsStamp, nil)
	fromSource, _ := censusOverSystemCache(test, filled)
	restored, _ := censusOverSystemCache(test, filled)
	if fromSource.record.Dependence == nil || fromSource.record.Dependence.Facts == nil ||
		restored.record.Dependence == nil || restored.record.Dependence.Facts == nil {
		test.Fatalf("a windows build has no facts census: %+v and %+v", fromSource.record.Dependence,
			restored.record.Dependence)
	}
	if restored.record.SystemCacheRestored == 0 {
		test.Fatalf("the second run's windows build restored nothing from the system cache: %+v", restored.record)
	}
	whole, part := fromSource.record.Dependence.Facts, restored.record.Dependence.Facts
	fewer := factsCompared(whole) - factsCompared(part)
	test.Logf("checked from source the census compares %d facts and leaves out %d for want of syntax; restored, %d "+
		"and %d", factsCompared(whole), whole.WithoutSyntax, factsCompared(part), part.WithoutSyntax)
	if whole.WithoutSyntax != 0 {
		test.Errorf("the census of a build that restored nothing counts %d facts left out for want of syntax, want 0",
			whole.WithoutSyntax)
	}
	if fewer <= 0 {
		test.Fatalf("the census of the build that restored its standard library compares %d facts, and the one that "+
			"checked it from source %d, want fewer", factsCompared(part), factsCompared(whole))
	}
	if part.WithoutSyntax == 0 {
		test.Errorf("the census compares %d facts fewer of a build that restored its standard library and counts none "+
			"left out for want of syntax, want its record to say so", fewer)
	}
}

// The facts census counts each fact it leaves out for want of syntax, on the side that holds it (#wzre5eg). The
// census-split fixture's package is the darwin host's and the windows build's by one ID, split.go compiled by both.
// With the build's package checked from source, the census compares the fact both hold about Shared, the host's about
// the package and the build's about Serve, and leaves out nothing. With it restored, the build having no syntax of
// it, the census compares none and counts five: the host's about Shared and about the package, the build's about
// Shared and about Serve, and one the build's entry holds by a path alone. Each side's fact about its own Split, in a
// file the other does not compile, is no comparison's either way, and is never counted.
func TestTheFactsCensusCountsEachFactOfAPackageWithoutSyntax(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "census-split"))
	host, build := loadedSplit(test, root, "darwin"), loadedSplit(test, root, "windows")
	graph, dependence := splitGraph(host)
	about := func(owner *packages.Package, name string) check.ExportedFact {
		return check.ExportedFact{Package: owner, Analyzer: "ctrlflow", FactType: "noReturn",
			Object: splitFunction(test, owner, name)}
	}
	hosted := map[factKey]check.ExportedFact{}
	for _, fact := range []check.ExportedFact{about(host, "Shared"), about(host, "Split"),
		{Package: host, Analyzer: "ctrlflow", FactType: "noReturn"}} {
		key, isKeyed := factKeyOf(fact)
		if !isKeyed {
			test.Fatalf("the host's fact about %v has no key", fact.Object)
		}
		hosted[key] = fact
	}
	built := []check.ExportedFact{about(build, "Shared"), about(build, "Serve"), about(build, "Split")}
	shared := map[sharedFile]bool{{packageIdentifier: host.ID, file: filepath.Join(root, "split.go")}: true}
	census := func(judged *buildJudgment, facts []check.ExportedFact) (int, int) {
		judged.dependence, judged.shared = dependence, shared
		engine := &pipeline{dependence: &hostDependence{graph: graph, facts: hosted}}
		counted := engine.compareFacts(judged, facts)
		return factsCompared(counted), counted.WithoutSyntax
	}
	if compared, withoutSyntax := census(&buildJudgment{sharedPackages: map[string]bool{host.ID: true}},
		built); compared != 3 || withoutSyntax != 0 {
		test.Errorf("checked from source, the census compares %d facts and counts %d left out for want of syntax, "+
			"want 3 and 0", compared, withoutSyntax)
	}
	pathOnly := check.ExportedFact{Package: build, Analyzer: "ctrlflow", FactType: "noReturn", ObjectPath: "T.M0"}
	if compared, withoutSyntax := census(&buildJudgment{withoutSyntax: map[string]bool{host.ID: true}},
		append(built, pathOnly)); compared != 0 || withoutSyntax != 5 {
		test.Errorf("restored, the census compares %d facts and counts %d left out for want of syntax, want 0 and 5",
			compared, withoutSyntax)
	}
}
