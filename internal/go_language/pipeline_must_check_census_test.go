package go_language

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/build_dependence"
)

// censusLines are the lines of the census file at path, each without its newline, failing the test when it cannot be
// read.
func censusLines(test *testing.T, path string) []string {
	test.Helper()
	contents, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatal(readError)
	}
	// An empty file, as a wide file that answered nothing wide is, holds no line.
	text := strings.TrimSuffix(string(contents), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// Under `--build-dependence` with the must-check census, the build-dependence fixture's windows build writes its
// must-check set, its wide file and its judgment's stamp into the census's directory, named for the build, and the host
// writes none (#anfjtr3). The fixture's own package, whose mode file differs and which declares Mode, dependent on
// windows, is in the set for its files and its declarations, and so is os, whose PathSeparator windows declares in a
// file of its own. The whole set is the one the library's own judgment of the same two loads gives, every package of
// its difference and every package declaring a dependent object, less the host's alone, though cohere-go judged the
// standard library on a graph of its own. Nothing was answered wide. A run without the census keeps no set.
func TestEachBuildWritesItsMustCheckSet(test *testing.T) {
	test.Parallel()

	census := test.TempDir()
	root := copyFixture(test, filepath.Join("testdata", "build-dependence"))
	on, _ := runOverConfigured(test, root, exitClean, func(pipeline *pipeline) { pipeline.mustCheckCensus = census },
		"--no-cache", "--build-dependence")
	off, _ := runOver(test, copyFixture(test, filepath.Join("testdata", "build-dependence")), exitClean,
		"--build-dependence")

	if len(on.buildRuns) != 2 || on.buildRuns[1].build.OperatingSystem != "windows" {
		test.Fatalf("want the host's build and windows's, got %d runs", len(on.buildRuns))
	}
	stem := buildFileStem(on.buildRuns[1].build)
	written, listError := os.ReadDir(census)
	if listError != nil {
		test.Fatal(listError)
	}
	var names []string
	for _, entry := range written {
		names = append(names, entry.Name())
	}
	if want := []string{stem + ".judgment.txt", stem + ".txt", stem + ".wide.txt"}; !slices.Equal(names, want) {
		test.Fatalf("want the census to hold %q, the windows build's alone, got %q", want, names)
	}
	lines := censusLines(test, filepath.Join(census, stem+".txt"))
	for _, want := range []string{dependenceModule + "\tFiles,Declares", "os\tFiles,Declares"} {
		if !slices.Contains(lines, want) {
			test.Errorf("want the line %q in the windows build's set, got %q", want, lines)
		}
	}
	if !slices.IsSorted(lines) {
		test.Errorf("want the set's lines sorted, got %q", lines)
	}

	hostEnvironment := append(os.Environ(), on.described.workspace.Environment()...)
	buildEnvironment := append(slices.Clone(hostEnvironment), on.buildRuns[1].build.Environment()...)
	library := libraryJudgment(test, root, hostEnvironment, buildEnvironment)
	want := mustCheckLines([]build_dependence.Difference{library.difference},
		[]*build_dependence.Dependence{library.judged}, nil)
	if !slices.Equal(lines, want) {
		test.Errorf("the windows build's set is\n%s\nwant the library's\n%s", strings.Join(lines, "\n"),
			strings.Join(want, "\n"))
	}
	if wide := censusLines(test, filepath.Join(census, stem+".wide.txt")); len(wide) != 0 {
		test.Errorf("want nothing answered wide, got %q", wide)
	}

	for _, run := range off.buildRuns {
		if run.judged != nil && run.judged.mustCheck != nil {
			test.Errorf("a run without the census kept the must-check set of %s", run.build)
		}
	}
}

// The build-dependence-repeated fixture's reader reads position's Second, a const spec that repeats First's iota, which
// windows declares for itself in a file of position's own, so Second differs there though its own span names nothing:
// reader declares a dependent object on windows, though no file of its own differs, on the plain run's declarations
// graph and on `--build-dependence`'s whole one alike (#anfjtr3). Give a repeated spec only its own span's universe
// names, and reader is in neither set.
func TestARepeatedConstSpecsReaderIsInTheSet(test *testing.T) {
	test.Parallel()

	fixture := filepath.Join("testdata", "build-dependence-repeated")
	want := "example.com/repeated/reader\tDeclares"
	for _, path := range []struct {
		name  string
		flags []string
	}{
		{"the plain run", []string{"--no-cache", "--no-system-cache"}},
		{"build dependence", []string{"--no-cache", "--no-system-cache", "--build-dependence"}},
	} {
		census := test.TempDir()
		pipeline, _ := runOverConfigured(test, copyFixture(test, fixture), exitClean,
			func(pipeline *pipeline) { pipeline.mustCheckCensus = census }, path.flags...)
		if len(pipeline.buildRuns) != 2 || pipeline.buildRuns[1].build.OperatingSystem != "windows" {
			test.Fatalf("%s: want the host's build and windows's, got %d runs", path.name, len(pipeline.buildRuns))
		}
		lines := censusLines(test, filepath.Join(census, buildFileStem(pipeline.buildRuns[1].build)+".txt"))
		if !slices.Contains(lines, want) {
			test.Errorf("%s: want the line %q in the windows build's set, got %q", path.name, want, lines)
		}
	}
}
