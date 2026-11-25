package go_language

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/contract"
)

// censusFiles are every file of a census's directory, by name, with its contents, but each build's judgment stamp,
// whose milliseconds differ run to run (judgmentStamp).
func censusFiles(test *testing.T, directory string) map[string]string {
	test.Helper()
	entries, listError := os.ReadDir(directory)
	if listError != nil {
		test.Fatal(listError)
	}
	files := map[string]string{}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".judgment.txt") {
			continue
		}
		contents, readError := os.ReadFile(filepath.Join(directory, entry.Name()))
		if readError != nil {
			test.Fatal(readError)
		}
		files[entry.Name()] = string(contents)
	}
	return files
}

// withheldUnderATagFixture is the withheld-through-another-package fixture (withheldFixture) with a file of app's only
// the tag `purego` compiles, so a run checks a build under that tag beside the host's. The host fakes d, whose cgo file
// marks its reads never, and withholds d's Doubled and Size; the tag build compiles e's plain file in place of its cgo
// file, and d's files are the same in both builds.
func withheldUnderATagFixture(test *testing.T) string {
	test.Helper()
	root := withheldFixture(test)
	pure := "//go:build purego\n\npackage app\n\n// Pure says the build is purego's.\nconst Pure = true\n"
	if writeError := os.WriteFile(filepath.Join(root, "app", "app_purego.go"), []byte(pure), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	return root
}

// A plain run's declarations-only graph, built in its host's walks, judges each extra build to the must-check set
// `--build-dependence`'s whole graph does, file for file, on a run whose every package is checked from source, so no
// record is needed (#anfjtr3): beside its load, under `--no-cache`, where the pass reads what the load withheld once
// each package is final (load.Overlap.Withheld), and after it, with a cold store of its own, where it reads the load's
// (load.Loaded.Withheld). The build-dependence fixture's windows build is one; the withheld fixture's build under the
// tag `purego` is the other, where d, a dependency with a cgo file whose reads are never walked, is in the set only for
// what the host withheld of it. Nothing is answered wide. A run without the census builds no graph, and its findings
// are the census runs' line for line. Skip the packages whose reads are marked never, or hand on no withheld position,
// and the withheld fixture's sets differ; set the hook with the census off, and the run without it builds a graph.
func TestAPlainRunsBitsJudgeEachBuildAsTheWholeGraph(test *testing.T) {
	test.Parallel()

	for _, fixture := range []struct {
		name    string
		root    func(test *testing.T) string
		exit    int
		inSet   string
		builtIn func(engine *pipeline) bool
	}{
		{"build-dependence", func(test *testing.T) string {
			return copyFixture(test, filepath.Join("testdata", "build-dependence"))
		}, exitClean, dependenceModule + "\tFiles,Declares", func(engine *pipeline) bool {
			return engine.buildRuns[1].build.OperatingSystem == "windows"
		}},
		{"withheld under a tag", withheldUnderATagFixture, exitClean, "example.com/dependency/d\tDeclares",
			func(engine *pipeline) bool {
				return slices.Equal(engine.buildRuns[1].build.Tags, []string{"purego"})
			}},
	} {
		test.Run(fixture.name, func(test *testing.T) {
			test.Parallel()

			census := func(directory string) func(*pipeline) {
				return func(engine *pipeline) { engine.mustCheckCensus = directory }
			}
			reference := test.TempDir()
			whole, _ := runOverConfigured(test, fixture.root(test), fixture.exit, census(reference), "--no-cache",
				"--no-system-cache", "--build-dependence")
			if len(whole.buildRuns) != 2 || !fixture.builtIn(whole) {
				test.Fatalf("want the host's build and the fixture's other, got %d runs", len(whole.buildRuns))
			}
			want := censusFiles(test, reference)
			if len(want) != 2 || !slices.Contains(strings.Split(want[buildFileStem(whole.buildRuns[1].build)+".txt"],
				"\n"), fixture.inSet) {
				test.Fatalf("want the whole graph's set to hold %q, got %v", fixture.inSet, want)
			}
			for name, contents := range want {
				if strings.HasSuffix(name, ".wide.txt") && contents != "" {
					test.Errorf("want nothing answered wide by the whole graph, got %q", contents)
				}
			}

			besideDirectory, storedDirectory := test.TempDir(), test.TempDir()
			// Each run over a copy of its own, its stream's paths written from that copy's root, so the findings of
			// the three compare.
			normalized := func(configure func(*pipeline), extra ...string) (*pipeline, string) {
				root := fixture.root(test)
				engine, stream := runOverConfigured(test, root, fixture.exit, configure, extra...)
				return engine, normalize(stream, root)
			}
			beside, besideStream := normalized(census(besideDirectory), "--no-cache", "--no-system-cache")
			stored, storedStream := normalized(census(storedDirectory), "--no-system-cache")
			off, offStream := normalized(nil, "--no-cache", "--no-system-cache")
			for _, plain := range []struct {
				name      string
				engine    *pipeline
				directory string
			}{{"beside the load", beside, besideDirectory}, {"with a cold store", stored, storedDirectory}} {
				if plain.engine.plainBits == nil {
					test.Errorf("%s: want the plain run's bits, got none", plain.name)
				}
				if got := censusFiles(test, plain.directory); !maps.Equal(got, want) {
					test.Errorf("%s: the plain run's census is\n%v\nwant the whole graph's\n%v", plain.name, got, want)
				}
			}
			if off.plainBits != nil {
				test.Error("a run without the census built the plain run's bits")
			}
			for _, stream := range []string{besideStream, storedStream} {
				if !slices.Equal(findingLines(stream), findingLines(offStream)) {
					test.Errorf("the findings with the census are\n%q\nwant those without it\n%q",
						findingLines(stream), findingLines(offStream))
				}
			}
		})
	}
}

// A plain run's declarations-only records stand in its store for the packages the host restores (#anfjtr3). Over one
// copy of the build-dependence fixture, every package keyed into the project's store (`--no-system-cache`): a run
// without the census warms the store with no record; a census run then backfills every hit, checks it from source and
// writes each record once the host's walks have ended, never before; and a third reads every record back, backfilling
// nothing and writing nothing. Both census runs write the whole graph's census, and neither answers any package wide.
// Read no record, and the third run's wide file fills with every package its host restored.
func TestAPlainRunsDeclarationsRecordsStandInTheStore(test *testing.T) {
	test.Parallel()

	reference := test.TempDir()
	runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "build-dependence")), exitClean,
		func(engine *pipeline) { engine.mustCheckCensus = reference }, "--no-cache", "--no-system-cache",
		"--build-dependence")
	want := censusFiles(test, reference)

	root := copyFixture(test, filepath.Join("testdata", "build-dependence"))
	runOverConfigured(test, root, exitClean, nil, "--no-system-cache")
	backfilledDirectory, warmDirectory := test.TempDir(), test.TempDir()
	var steps []declarationsStep
	var stepsLock sync.Mutex
	_, backfilledStream := runOverConfigured(test, root, exitClean, func(engine *pipeline) {
		engine.mustCheckCensus = backfilledDirectory
		// The hook is called from the builds' goroutines too.
		engine.declarationsSteps = func(step declarationsStep) {
			stepsLock.Lock()
			defer stepsLock.Unlock()
			steps = append(steps, step)
		}
	}, "--no-system-cache")
	_, warmStream := runOverConfigured(test, root, exitClean,
		func(engine *pipeline) { engine.mustCheckCensus = warmDirectory }, "--no-system-cache")

	if want := []declarationsStep{declarationsWalksWhole, declarationsRecordsWriting}; len(steps) < len(want) ||
		!slices.Equal(steps[:len(want)], want) {
		test.Errorf("want the records written once the host's walks are whole, %q, got %q", want, steps)
	}
	backfilled, warm := recordCounts(test, backfilledStream), recordCounts(test, warmStream)
	if backfilled.Backfilled == 0 || backfilled.Written != backfilled.Backfilled {
		test.Errorf("want every hit backfilled and its record written, got %+v", backfilled)
	}
	if warm != (contract.DependenceRecords{Read: backfilled.Written}) {
		test.Errorf("want every record read back, %d, and nothing backfilled or written, got %+v", backfilled.Written,
			warm)
	}
	for _, census := range []struct {
		name      string
		directory string
	}{{"the backfilling run", backfilledDirectory}, {"the warm run", warmDirectory}} {
		if got := censusFiles(test, census.directory); !maps.Equal(got, want) {
			test.Errorf("%s: the census is\n%v\nwant the whole graph's\n%v", census.name, got, want)
		}
	}
}

// A plain run's standard library stands in the system cache's store by its declarations-only records, under
// `--no-cache` too, where the system cache is the run's only store (#anfjtr3). Over the build-dependence fixture, whose
// package reads os, and so syscall: a census run over a cold system cache checks the standard library from source and
// writes each package's record beside its entry there, and the next reads every one back, syscall among them, side
// loading nothing. A system cache a run without the census filled holds no record: the census run side-loads its
// standard library, syscall among it, and writes the records, and the next reads them. A run under `--no-cache` writes
// no cache counts, so the records are told by the graph, which says which packages it took from one. Every census run
// writes the whole graph's census, nothing answered wide. Read no system record, and syscall is side-loaded again on
// every run.
func TestAPlainRunsStandardLibraryRecordsStandInTheSystemCache(test *testing.T) {
	test.Parallel()

	reference := test.TempDir()
	runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "build-dependence")), exitClean,
		func(engine *pipeline) { engine.mustCheckCensus = reference }, "--no-cache", "--no-system-cache",
		"--build-dependence")
	want := censusFiles(test, reference)
	censusRun := func(systemCache string) (*pipeline, map[string]string) {
		directory := test.TempDir()
		engine, _ := runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "build-dependence")),
			exitClean, func(engine *pipeline) {
				sharingSystemCache(systemCache)(engine)
				engine.mustCheckCensus = directory
			}, "--no-cache")
		return engine, censusFiles(test, directory)
	}

	checkedCache, filledCache := test.TempDir(), test.TempDir()
	runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "build-dependence")), exitClean,
		sharingSystemCache(filledCache), "--no-cache")
	for _, side := range []struct {
		name        string
		systemCache string
		sideLoads   bool
	}{{"a cold system cache", checkedCache, false}, {"one filled without the census", filledCache, true}} {
		first, firstCensus := censusRun(side.systemCache)
		second, secondCensus := censusRun(side.systemCache)
		for _, census := range []map[string]string{firstCensus, secondCensus} {
			if !maps.Equal(census, want) {
				test.Errorf("%s: the census is\n%v\nwant the whole graph's\n%v", side.name, census, want)
			}
		}
		recorded := 0
		second.plainBits.declarations.recorded.Range(func(any, any) bool {
			recorded++
			return true
		})
		if recorded == 0 {
			test.Errorf("%s: want the second run's standard library from its records, got no package from one",
				side.name)
		}
		if sideLoaded := slices.Contains(first.plainBits.sideLoaded, "syscall"); sideLoaded != side.sideLoads {
			test.Errorf("%s: want syscall side-loaded %t by the first run, got %t", side.name, side.sideLoads,
				sideLoaded)
		}
		if !second.plainBits.declarations.fromRecord("syscall") || len(second.plainBits.sideLoaded) != 0 {
			test.Errorf("%s: want syscall from its record on the second run and nothing side-loaded, got from a "+
				"record %t, side-loaded %q", side.name, second.plainBits.declarations.fromRecord("syscall"),
				second.plainBits.sideLoaded)
		}
	}
}

// The build-dependence-embedded fixture's module, and its packages' IDs.
const (
	embeddedModule = "example.com/embedded"
	embeddedLaunch = embeddedModule + "/launch"
	embeddedSignal = embeddedModule + "/signal"
	embeddedSteady = embeddedModule + "/steady"
)

// A plain run judges each extra build at its restore, from its listing and its build-only files' syntax alone, and its
// must-check set is the whole graph's in every state of its stores (#anfjtr3). The build-dependence-embedded fixture's
// launch embeds syscall's SysProcAttr, whose fields windows declares in a file of its own, so launch declares a
// dependent object with no file of its own differing; signal embeds marker's Marker, whose method set only a file
// windows alone compiles changes, which only that file's syntax says; steady embeds nothing a platform shapes. The
// whole graph's census under `--build-dependence`, every package checked from source, holds launch and signal for
// their declarations and not steady, and answers nothing wide. Each plain state writes the same: every package from
// source beside the load; a system cache cold, then warm, under `--no-cache`; a store cold, then warm. Each build's
// judgment stamp says it was judged at its listing. A run without the census builds no graph and finds what the
// census runs find, line for line.
func TestEachBuildIsJudgedAtItsRestore(test *testing.T) {
	test.Parallel()

	fixture := filepath.Join("testdata", "build-dependence-embedded")
	reference := test.TempDir()
	whole, _ := runOverConfigured(test, copyFixture(test, fixture), exitClean,
		func(engine *pipeline) { engine.mustCheckCensus = reference }, "--no-cache", "--no-system-cache",
		"--build-dependence")
	if len(whole.buildRuns) != 2 || whole.buildRuns[1].build.OperatingSystem != "windows" {
		test.Fatalf("want the host's build and windows's, got %d runs", len(whole.buildRuns))
	}
	stem := buildFileStem(whole.buildRuns[1].build)
	want := censusFiles(test, reference)
	set := strings.Split(want[stem+".txt"], "\n")
	for _, line := range []string{embeddedLaunch + "\tDeclares", embeddedSignal + "\tDeclares"} {
		if !slices.Contains(set, line) {
			test.Errorf("want the whole graph's set to hold %q, got %q", line, set)
		}
	}
	for _, line := range set {
		if strings.HasPrefix(line, embeddedSteady+"\t") {
			test.Errorf("want steady in no set, got %q", line)
		}
	}
	if wide := want[stem+".wide.txt"]; wide != "" {
		test.Errorf("want nothing answered wide by the whole graph, got %q", wide)
	}

	census := func(directory string, configure func(*pipeline)) func(*pipeline) {
		return func(engine *pipeline) {
			if configure != nil {
				configure(engine)
			}
			engine.mustCheckCensus = directory
		}
	}
	systemCache, storedRoot := test.TempDir(), copyFixture(test, fixture)
	var findings [][]string
	for _, state := range []struct {
		name      string
		root      string
		configure func(*pipeline)
		flags     []string
	}{
		{"every package from source", "", nil, []string{"--no-cache", "--no-system-cache"}},
		{"a cold system cache", "", sharingSystemCache(systemCache), []string{"--no-cache"}},
		{"a warm system cache", "", sharingSystemCache(systemCache), []string{"--no-cache"}},
		{"a cold store", storedRoot, nil, []string{"--no-system-cache"}},
		{"a warm store", storedRoot, nil, []string{"--no-system-cache"}},
	} {
		root := state.root
		if root == "" {
			root = copyFixture(test, fixture)
		}
		directory := test.TempDir()
		plain, stream := runOverConfigured(test, root, exitClean, census(directory, state.configure), state.flags...)
		findings = append(findings, findingLines(normalize(stream, root)))
		if got := censusFiles(test, directory); !maps.Equal(got, want) {
			test.Errorf("%s: the census is\n%v\nwant the whole graph's\n%v", state.name, got, want)
		}
		stamp, stampError := os.ReadFile(filepath.Join(directory, stem+".judgment.txt"))
		if stampError != nil || !strings.Contains(string(stamp), "at\tListing\n") {
			test.Errorf("%s: want the build's judgment stamped at its listing, got %q, %v", state.name, stamp,
				stampError)
		}
		if plain.plainBits == nil {
			test.Errorf("%s: want the plain run's bits, got none", state.name)
		}
	}
	offRoot := copyFixture(test, fixture)
	off, offStream := runOverConfigured(test, offRoot, exitClean, nil, "--no-cache", "--no-system-cache")
	if off.plainBits != nil {
		test.Error("a run without the census built the plain run's bits")
	}
	for index, found := range findings {
		if !slices.Equal(found, findingLines(normalize(offStream, offRoot))) {
			test.Errorf("state %d: the findings with the census are\n%q\nwant those without it\n%q", index, found,
				findingLines(normalize(offStream, offRoot)))
		}
	}
}

// A build's judgment at its restore waits for the plain run's graph to be whole, its side load run, before it reads it
// (#anfjtr3). The graph's completion is held just before it lets the judgments run until the windows build has listed,
// so a judgment that did not wait would judge first; the build's listing waits, and judges only once the graph is
// whole. Judge without waiting, and the judgment comes before the graph is whole.
func TestABuildsJudgmentWaitsForTheWholeGraph(test *testing.T) {
	test.Parallel()

	var steps []declarationsStep
	var stepsLock sync.Mutex
	listed := make(chan struct{})
	var listedOnce sync.Once
	runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "build-dependence-embedded")), exitClean,
		func(engine *pipeline) {
			engine.mustCheckCensus = test.TempDir()
			engine.declarationsSteps = func(step declarationsStep) {
				if step == declarationsReady {
					// Held, with no lock, until the build has listed: waiting there, or judging already.
					<-listed
				}
				stepsLock.Lock()
				defer stepsLock.Unlock()
				steps = append(steps, step)
				if step == declarationsListedWaiting || step == declarationsJudging {
					listedOnce.Do(func() { close(listed) })
				}
			}
		}, "--no-cache", "--no-system-cache")
	ready, judging := slices.Index(steps, declarationsReady), slices.Index(steps, declarationsJudging)
	if ready < 0 || judging < 0 || judging < ready || slices.Index(steps, declarationsListedWaiting) > ready {
		test.Errorf("want the build's listing to wait, then the graph whole, then the judgment, got %q", steps)
	}
}

// bufferedFixture is the build-dependence-embedded fixture with a package of its own, buffered, that embeds bytes'
// Buffer, alike in every build, so buffered declares a dependent object in no build.
func bufferedFixture(test *testing.T) string {
	test.Helper()
	root := copyFixture(test, filepath.Join("testdata", "build-dependence-embedded"))
	buffered := "// Package buffered embeds bytes' Buffer, alike in every build.\npackage buffered\n\n" +
		"import \"bytes\"\n\n// Buffered is a buffer.\ntype Buffered struct {\n\tbytes.Buffer\n}\n"
	if makeError := os.Mkdir(filepath.Join(root, "buffered"), 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	if writeError := os.WriteFile(filepath.Join(root, "buffered", "buffered.go"), []byte(buffered),
		0o644); writeError != nil {
		test.Fatal(writeError)
	}
	return root
}

// A package a plain run's judgment holds in no graph is in the build's set as NoGraph, and named in its wide file, and
// a package that reads it is in the set for its declarations, each object it reads answered dependent (#anfjtr3). Over
// the buffered fixture, whose system cache a run without the census filled, so it holds no declarations record, a
// census run under `--no-cache` side-loads its standard library, and the side load fails: every standard-library
// package of the host's listing is in no graph. Of the windows build's, bytes, whose files are alike, is in the set
// as NoGraph alone, and syscall, whose files differ, as Files,NoGraph; buffered, which embeds bytes' Buffer, is in the
// set for its declarations, though the whole graph's set, from a run that loads every package from source, holds no
// buffered: only bytes answered wide puts it there. Answer what no graph holds narrow and buffered leaves the set;
// write no package in the wide file, or find none in no graph, and their lines go.
func TestAPackageInNoGraphIsInTheSet(test *testing.T) {
	test.Parallel()

	reference := test.TempDir()
	whole, _ := runOverConfigured(test, bufferedFixture(test), exitClean,
		func(engine *pipeline) { engine.mustCheckCensus = reference }, "--no-cache", "--no-system-cache",
		"--build-dependence")
	if len(whole.buildRuns) != 2 || whole.buildRuns[1].build.OperatingSystem != "windows" {
		test.Fatalf("want the host's build and windows's, got %d runs", len(whole.buildRuns))
	}
	stem := buildFileStem(whole.buildRuns[1].build)
	buffered := embeddedModule + "/buffered"
	for _, line := range censusLines(test, filepath.Join(reference, stem+".txt")) {
		if strings.HasPrefix(line, buffered+"\t") {
			test.Errorf("want buffered in no set of the whole graph's, got %q", line)
		}
	}

	filledCache, census := test.TempDir(), test.TempDir()
	runOverConfigured(test, bufferedFixture(test), exitClean, sharingSystemCache(filledCache), "--no-cache")
	refused := errors.New("the side load is refused")
	plain, _ := runOverConfigured(test, bufferedFixture(test), exitClean, func(engine *pipeline) {
		sharingSystemCache(filledCache)(engine)
		engine.mustCheckCensus = census
		engine.sideLoadRefused = refused
	}, "--no-cache")
	if plain.plainBits == nil || !errors.Is(plain.plainBits.sideLoadProblem, refused) {
		test.Fatal("want the plain run's side load refused")
	}
	lines := censusLines(test, filepath.Join(census, stem+".txt"))
	for _, want := range []string{"bytes\tNoGraph", "syscall\tFiles,NoGraph", buffered + "\tDeclares"} {
		if !slices.Contains(lines, want) {
			test.Errorf("want the line %q in the windows build's set, got %q", want, lines)
		}
	}
	wide := censusLines(test, filepath.Join(census, stem+".wide.txt"))
	for _, want := range []string{"bytes\tNoGraph", "syscall\tNoGraph"} {
		if !slices.Contains(wide, want) {
			test.Errorf("want the line %q in the windows build's wide file, got %q", want, wide)
		}
	}
}

// A plain run without the census keeps no declarations record (#anfjtr3). Over one copy of the
// build-dependence-embedded fixture, whose windows build runs beside the host's, every package keyed into the
// project's store, a run fills the store and a warm run after it restores every package the host checks from there:
// the warm run counts no dependence record of any kind, reading, backfilling and writing none, and the store holds no
// declarations record beside any entry. Keep declarations records on a plain run without the census, and the warm run
// backfills every hit.
func TestAPlainRunWithoutTheCensusKeepsNoDeclarationsRecord(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "build-dependence-embedded"))
	runOverConfigured(test, root, exitClean, nil, "--no-system-cache")
	warm, warmStream := runOverConfigured(test, root, exitClean, nil, "--no-system-cache")
	if len(warm.buildRuns) != 2 || warm.plainBits != nil {
		test.Fatalf("want the host's build and windows's and no plain bits, got %d runs", len(warm.buildRuns))
	}
	cached := lintRecordOf(test, warmStream).Cache
	if cached.Missed != 0 {
		test.Fatalf("want the warm run to restore every package, got %d missed", cached.Missed)
	}
	if cached.DependenceRecords != nil {
		test.Errorf("want no dependence record counted, got %+v", *cached.DependenceRecords)
	}
	if stored := storedDeclarationsRecords(test, root); len(stored) != 0 {
		test.Errorf("want no declarations record in the store, got %d", len(stored))
	}
}
