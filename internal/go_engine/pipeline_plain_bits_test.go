package go_engine

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
