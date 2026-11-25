package go_language

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/contract"
	"github.com/system-inc/cohere/internal/go_language/rules"
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

// misusedPrintf is a file of the std-records fixture's package whose call into fmt is printf's finding, one a build
// makes of a package it restored from the system cache.
const misusedPrintf = "package library\n\nimport \"fmt\"\n\n// Misused formats a string as a number.\n" +
	"func Misused() {\n\tfmt.Printf(\"%d\\n\", \"text\")\n}\n"

// The other direction (#wzre5eg's review): a census build's entries serve a plain run. A build of a run judging
// dependence writes the standard library it checks from source to the system cache, which every project on the
// machine then restores, so what it writes there must be what a plain run's build writes. Over a system cache
// holding the host's standard library alone, a run under `--build-dependence` with the body reach census, the body
// differential and the standing bodies' switch on writes windows's entries, its host writing none; a plain run over
// another copy of the project then restores every one of them in its windows build, writes none, and finds what a
// plain run finds over entries a plain run wrote, in its windows build's own findings and in its stream's, printf's
// finding of a call into fmt, which the build restored, among them. An entry short of what a plain run reads of it is
// one that run checks from source and writes again, so the counts tell it first. The entries' bytes cannot pin this,
// two plain runs alike writing some of them with other bytes, so the findings are held alike.
func TestAPlainRunRestoresTheStandardLibraryACensusBuildWrote(test *testing.T) {
	test.Parallel()

	fixture := func() string {
		root := copyFixture(test, standardLibraryRecordsFixture)
		if writeError := os.WriteFile(filepath.Join(root, "misused.go"), []byte(misusedPrintf),
			0o644); writeError != nil {
			test.Fatal(writeError)
		}
		return root
	}
	// served are a plain run's windows build's own findings and its stream's, over a system cache whose windows
	// entries the run named writer wrote, under switches and configured by configure when it is not nil.
	served := func(writer string, configure func(*pipeline), switches ...string) ([]string, []string) {
		filled, _ := filledSystemCache(test, rules.All(), engineTestsStamp, nil)
		writerReading := readingSystemCache(filled, "")
		_, writerStream, _ := runOverNoted(test, fixture(), exitFindings, func(engine *pipeline) {
			writerReading(engine)
			if configure != nil {
				configure(engine)
			}
		}, switches...)
		wrote := lintRecordOf(test, writerStream).Builds
		if len(wrote) != 2 || wrote[0].SystemCacheWritten != 0 || wrote[1].SystemCacheWritten == 0 {
			test.Fatalf("want the %s run's host to write nothing to the system cache and its windows build its own "+
				"standard library, got the records %+v", writer, wrote)
		}
		root := fixture()
		engine, stream, _ := runOverNoted(test, root, exitFindings, readingSystemCache(filled, ""), "--no-cache")
		restored := lintRecordOf(test, stream).Builds
		if len(engine.buildRuns) != 2 || len(restored) != 2 ||
			engine.buildRuns[1].build.OperatingSystem != "windows" {
			test.Fatalf("after the %s run, want the host's build and windows's, got %d runs and the records %+v",
				writer, len(engine.buildRuns), restored)
		}
		if restored[1].SystemCacheRestored != wrote[1].SystemCacheWritten || restored[1].SystemCacheWritten != 0 {
			test.Errorf("after the %s run, the plain run's windows build restored %d standard-library packages from "+
				"the system cache and wrote %d, want the %d that run's build wrote restored and none written", writer,
				restored[1].SystemCacheRestored, restored[1].SystemCacheWritten, wrote[1].SystemCacheWritten)
		}
		return rawFindings(engine.buildRuns[1], root), findingLines(normalize(stream, root))
	}
	plainOwn, plainStream := served("plain", nil, "--no-cache")
	censusOwn, censusStream := served("census", func(engine *pipeline) {
		engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
		engine.bodyDifferential = true
		engine.standingBodies = true
	}, "--no-cache", "--no-fix", "--build-dependence")
	if !slices.ContainsFunc(plainStream, func(line string) bool {
		return strings.Contains(line, `"rule":"cohere-go/printf"`)
	}) || len(plainOwn) == 0 {
		test.Fatalf("over a plain run's entries the plain run's windows build finds %d findings of its own and its "+
			"stream holds\n%s\nwant findings of its own and printf's in the stream", len(plainOwn),
			strings.Join(plainStream, ""))
	}
	if !slices.Equal(censusOwn, plainOwn) {
		test.Errorf("over a census build's entries the plain run's windows build finds\n%s\nwant what it finds over "+
			"a plain run's\n%s", strings.Join(censusOwn, "\n"), strings.Join(plainOwn, "\n"))
	}
	if !slices.Equal(censusStream, plainStream) {
		test.Errorf("over a census build's entries the plain run's stream holds the findings\n%s\nwant those it "+
			"holds over a plain run's\n%s", strings.Join(censusStream, ""), strings.Join(plainStream, ""))
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
//
// Either census leaves out the facts about an object in a file one build alone compiles, of which the standard library
// has some on every host, windows's own files and those windows does not compile, and its record counts them too
// (contract.DependenceFacts.CompiledInOne, #w5dbvdm). How many is the toolchain's and the host's platform's, so the
// numbers are logged, never pinned: on darwin/arm64 under go1.27 the census of the build that restored counts 41, 28
// of the host's and 13 of windows's, and the one of the build that checked from source 142, the same 28 and 114.
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
	test.Logf("checked from source the census compares %d facts, leaves out %d for want of syntax and %d in a file "+
		"one build alone compiles; restored, %d, %d and %d", factsCompared(whole), whole.WithoutSyntax,
		whole.CompiledInOne, factsCompared(part), part.WithoutSyntax, part.CompiledInOne)
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
	if whole.CompiledInOne == 0 || part.CompiledInOne == 0 {
		test.Errorf("the censuses count %d and %d facts in a file one build alone compiles, want some in each, the "+
			"standard library having files only windows compiles and files it does not", whole.CompiledInOne,
			part.CompiledInOne)
	}
}

// The facts census counts each fact it leaves out for want of syntax, on the side that holds it (#wzre5eg). The
// census-split fixture's package is the darwin host's and the windows build's by one ID, split.go compiled by both.
// With the build's package checked from source, the census compares the fact both hold about Shared, the host's about
// the package and the build's about Serve, and leaves out nothing. With it restored, the build having no syntax of
// it, the census compares none and counts five: the host's about Shared and about the package, the build's about
// Shared and about Serve, and one the build's entry holds by a path alone. Each side's fact about its own Split, in a
// file the other does not compile, is no comparison's either way, and is counted apart, on the side that holds it
// (contract.DependenceFacts.CompiledInOne, #w5dbvdm): two either way, one with the host's left out, one with the
// build's, and none of a package the judgment holds as neither's, which no census reads.
func TestTheFactsCensusCountsEachFactOfAPackageWithoutSyntax(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "census-split"))
	host, build := loadedSplit(test, root, "darwin"), loadedSplit(test, root, "windows")
	graph, dependence := splitGraph(host)
	about := func(owner *packages.Package, name string) check.ExportedFact {
		return check.ExportedFact{Package: owner, Analyzer: "ctrlflow", FactType: "noReturn",
			Object: splitFunction(test, owner, name)}
	}
	keyed := func(facts ...check.ExportedFact) map[factKey]check.ExportedFact {
		hosted := map[factKey]check.ExportedFact{}
		for _, fact := range facts {
			key, isKeyed := factKeyOf(fact)
			if !isKeyed {
				test.Fatalf("the host's fact about %v has no key", fact.Object)
			}
			hosted[key] = fact
		}
		return hosted
	}
	hostsShared := []check.ExportedFact{about(host, "Shared"), {Package: host, Analyzer: "ctrlflow",
		FactType: "noReturn"}}
	hosted := keyed(append(hostsShared, about(host, "Split"))...)
	buildsShared := []check.ExportedFact{about(build, "Shared"), about(build, "Serve")}
	built := append(buildsShared, about(build, "Split"))
	shared := map[sharedFile]bool{{packageIdentifier: host.ID, file: filepath.Join(root, "split.go")}: true}
	census := func(judged *buildJudgment, hosted map[factKey]check.ExportedFact,
		facts []check.ExportedFact) [3]int {
		judged.dependence, judged.shared = dependence, shared
		engine := &pipeline{dependence: &hostDependence{graph: graph, facts: hosted}}
		counted := engine.compareFacts(judged, facts)
		return [3]int{factsCompared(counted), counted.WithoutSyntax, counted.CompiledInOne}
	}
	fromSource := func() *buildJudgment { return &buildJudgment{sharedPackages: map[string]bool{host.ID: true}} }
	restored := func() *buildJudgment { return &buildJudgment{withoutSyntax: map[string]bool{host.ID: true}} }
	if got := census(fromSource(), hosted, built); got != [3]int{3, 0, 2} {
		test.Errorf("checked from source, the census compares, counts left out for want of syntax and counts in a "+
			"file one build alone compiles %v, want 3, 0 and 2", got)
	}
	pathOnly := check.ExportedFact{Package: build, Analyzer: "ctrlflow", FactType: "noReturn", ObjectPath: "T.M0"}
	if got := census(restored(), hosted, append(built, pathOnly)); got != [3]int{0, 5, 2} {
		test.Errorf("restored, the census compares, counts left out for want of syntax and counts in a file one "+
			"build alone compiles %v, want 0, 5 and 2", got)
	}
	// Each side's Split is counted on its own side: with one side's left out, the other's alone is counted.
	for _, each := range []struct {
		name   string
		judged func() *buildJudgment
		want   [3]int
	}{{"checked from source", fromSource, [3]int{3, 0, 1}}, {"restored", restored, [3]int{0, 4, 1}}} {
		if got := census(each.judged(), keyed(hostsShared...), built); got != each.want {
			test.Errorf("%s, without the host's fact about its Split, the census counts %v, want %v", each.name, got,
				each.want)
		}
		if got := census(each.judged(), hosted, buildsShared); got != each.want {
			test.Errorf("%s, without the build's fact about its Split, the census counts %v, want %v", each.name, got,
				each.want)
		}
	}
	// A package the judgment holds as neither both builds' checked from source nor both builds' restored is one
	// build's alone, or a root held against its test variant, and no fact of it is counted.
	if got := census(&buildJudgment{}, hosted, built); got != [3]int{0, 0, 0} {
		test.Errorf("of a package only one build has by its ID, the census counts %v, want nothing", got)
	}
}
