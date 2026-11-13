package build

import (
	"bytes"
	"go/build"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// positionFiles are a package whose imports, embeds and directives sit on lines and columns of their own, in a file of
// the package, its test and its external test, so a position read wrong, or one import's taken for another's, moves a
// Package field.
var positionFiles = map[string]string{
	"positions.go": "//go:build darwin || linux\n\n// Package positions is read.\npackage positions\n\nimport (\n" +
		"\t\"os\"\n\t  _ \"strings\"\n\n\tsorting \"sort\"\n)\nimport \"fmt\"; import _ \"embed\"\n\n" +
		"//go:embed a.txt\n//go:embed   b.txt\nvar files string\n\nvar _, _, _ = os.Args, sorting.Ints, fmt.Sprint\n",
	"positions_darwin.go": "//go:debug panicnil=1\n\npackage positions\n\nimport (\n\t\"bytes\"\n\t\"io\"\n)\n\n" +
		"var _, _ = bytes.Clone, io.EOF\n",
	"positions_test.go": "package positions\n\nimport \"testing\"; import \"slices\"\n\n" +
		"func TestPositions(test *testing.T) { _ = slices.Max[[]int] }\n",
	"external_test.go": "package positions_test\n\nimport (\n\t\"testing\"\n\n\t\"strconv\"\n)\n\n" +
		"func TestExternal(test *testing.T) { _ = strconv.Itoa }\n",
	"a.txt": "a\n",
	"b.txt": "b\n",
}

// Every position Import reads of a file read once for the run is the one go/build reads, under each build that reads
// the file: each import's, the test's and the external test's, the embeds' and the directives', field by field. The
// second build takes the first's read, positions resolved by it.
func TestPositionsReadOnceAreGoBuildsOwn(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	for name, contents := range positionFiles {
		if writeError := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	headers := NewHeaders()
	for _, target := range []string{"darwin", "linux", "darwin"} {
		context := build.Default
		context.GOOS, context.GOARCH, context.CgoEnabled = target, "arm64", false
		built, importError := ImportDirectory(context, directory, nil, headers)
		expected, expectedError := context.ImportDir(directory, 0)
		if importError != nil || expectedError != nil {
			test.Fatalf("%s: read with %v, go/build with %v", target, importError, expectedError)
		}
		if !reflect.DeepEqual(built, expected) {
			test.Errorf("%s: read\n%+v\nwhere go/build reads\n%+v", target, built, expected)
		}
	}
	// Each import sits where no other does, so one import's position taken for all would move ImportPos.
	expected, expectedError := build.Default.ImportDir(directory, 0)
	if expectedError != nil {
		test.Fatal(expectedError)
	}
	var seen []string
	for _, positions := range expected.ImportPos {
		for _, position := range positions {
			if slices.Contains(seen, position.String()) {
				test.Fatalf("two imports at %s, so the fixture cannot tell them apart", position)
			}
			seen = append(seen, position.String())
		}
	}
	if len(seen) < 6 || len(expected.EmbedPatternPos) != 2 {
		test.Fatalf("the fixture has %d import positions and %d embed patterns, too few to read", len(seen),
			len(expected.EmbedPatternPos))
	}
	if parsed := headers.Parsed(); parsed != 4 {
		test.Errorf("the reads read %d headers, want each of the four Go files a build admits once", parsed)
	}
}

// A build that finds a file being read by another reads it itself, never waiting, and every build's answer is the one
// its own held bytes give: a build holding other bytes reads them, and a build that finished second takes the first
// read, of the same bytes. The reads are driven by a hook that holds the first one, never by timing.
func TestABuildReadsAFileBeingReadItself(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	file := filepath.Join(directory, "raced.go")
	if writeError := os.WriteFile(file, []byte("package raced\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	first := []byte("package raced\n\nimport _ \"container/list\"\n")
	second := []byte("package raced\n\nimport _ \"container/ring\"\n")
	headers := NewHeaders()
	entered, release := make(chan struct{}), make(chan struct{})
	var calls int
	headers.reading = func(string) {
		calls++
		// Only the first read is held, until every other has finished.
		if calls == 1 {
			close(entered)
			<-release
		}
	}
	read := func(contents []byte) []string {
		held := func(path string) ([]byte, bool) { return contents, path == file }
		// The context opens a held file from its held bytes, as the lister's does (load's Held).
		context := build.Default
		context.OpenFile = func(path string) (io.ReadCloser, error) {
			if path == file {
				return io.NopCloser(bytes.NewReader(contents)), nil
			}
			return os.Open(path)
		}
		built, importError := ImportDirectory(context, directory, held, headers)
		if importError != nil {
			test.Errorf("reading %q: %v", contents, importError)
			return nil
		}
		return built.Imports
	}
	held := make(chan []string)
	go func() { held <- read(first) }()
	<-entered
	// The same bytes while the first read is held: read here, never waiting on it.
	if imports := read(first); !slices.Equal(imports, []string{"container/list"}) {
		test.Errorf("a second build of the same bytes reads imports %v", imports)
	}
	// Other bytes, as after the long-line wrap: read as written, never the first read.
	if imports := read(second); !slices.Equal(imports, []string{"container/ring"}) {
		test.Errorf("a build of other bytes reads imports %v", imports)
	}
	close(release)
	if imports := <-held; !slices.Equal(imports, []string{"container/list"}) {
		test.Errorf("the held first build reads imports %v", imports)
	}
	if parsed := headers.Parsed(); parsed != 3 {
		test.Errorf("the builds read %d headers, want three: the held one, the one beside it and the other bytes",
			parsed)
	}
}
