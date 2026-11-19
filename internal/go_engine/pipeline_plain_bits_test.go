package go_engine

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// censusFiles are every file of a census's directory, by name, with its contents.
func censusFiles(test *testing.T, directory string) map[string]string {
	test.Helper()
	entries, listError := os.ReadDir(directory)
	if listError != nil {
		test.Fatal(listError)
	}
	files := map[string]string{}
	for _, entry := range entries {
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
	_, backfilledStream := runOverConfigured(test, root, exitClean, func(engine *pipeline) {
		engine.mustCheckCensus = backfilledDirectory
		engine.declarationsSteps = func(step declarationsStep) { steps = append(steps, step) }
	}, "--no-system-cache")
	_, warmStream := runOverConfigured(test, root, exitClean,
		func(engine *pipeline) { engine.mustCheckCensus = warmDirectory }, "--no-system-cache")

	if want := []declarationsStep{declarationsWalksWhole, declarationsRecordsWriting}; !slices.Equal(steps, want) {
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
