package build

import (
	"bytes"
	"fmt"
	"go/build"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
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
	// Each import sits where no other does, so one import's position taken for all would move ImportPos. The fixture
	// is read as darwin's, never the host's: positions_darwin.go holds two of its imports, and linux excludes the file.
	darwin := build.Default
	darwin.GOOS, darwin.GOARCH, darwin.CgoEnabled = "darwin", "arm64", false
	expected, expectedError := darwin.ImportDir(directory, 0)
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

// heldSource is a file whose imports carry doc comments of their own, which compacted cuts, and sit on lines of their
// own, which resolved reads.
const heldSource = "// Package held is read.\npackage held\n\nimport (\n\t// The doc of fmt's import.\n\t\"fmt\"\n" +
	"\t\"strings\"\n)\n\n// The lone import's doc.\nimport \"os\"\n\nvar _, _, _ = fmt.Sprint, strings.Cut, os.Exit\n"

// The one read readGoInfoOnce shares is the one every build holds, and compacted and resolved cut and resolve only a
// read their caller gave up. A build that finds the file unread reads it, and before it shares its read another build
// reads and shares the file first, through the hook: the second compacts its own read, drops it, and takes the shared
// one, which the build already holding it still reads as it was handed out. Without headers the read is the caller's
// whole, as upstream's matchFile reads it: the syntax tree and each import's doc kept, each position resolved.
func TestACompactedReadIsNoReadACallerStillHolds(test *testing.T) {
	test.Parallel()
	file := filepath.Join(test.TempDir(), "held.go")
	if writeError := os.WriteFile(file, []byte(heldSource), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	// What a build holding a read reads of it, spelled out, so a later cut of what it holds shows against it.
	holding := func(read fileInfo) string {
		var described strings.Builder
		fmt.Fprintf(&described, "header %q, file set kept %t", read.header, read.fset != nil)
		if read.parsed != nil {
			fmt.Fprintf(&described, ", package %s, doc %q, %d declarations, %d imports parsed", read.parsed.Name.Name,
				read.parsed.Doc.Text(), len(read.parsed.Decls), len(read.parsed.Imports))
		}
		for _, each := range read.imports {
			fmt.Fprintf(&described, "; import %s at %s, doc %q", each.path, each.position, each.doc.Text())
		}
		return described.String()
	}
	wantPositions := []string{file + ":6:2", file + ":7:2", file + ":11:8"}
	positions := func(read fileInfo) []string {
		var resolvedPositions []string
		for _, each := range read.imports {
			resolvedPositions = append(resolvedPositions, each.position.String())
		}
		return resolvedPositions
	}

	headers := NewHeaders()
	context := &Context{Context: build.Default, headers: headers}
	var first fileInfo
	var firstHeld string
	calls := 0
	headers.reading = func(string) {
		calls++
		// Only the outer read lets another build in, and that build's own call to the hook passes through.
		if calls != 1 {
			return
		}
		read, openError, readError := context.readGoInfoOnce(file, token.NewFileSet())
		if openError != nil || readError != nil {
			test.Fatalf("the first build to share the read opened with %v, read with %v", openError, readError)
		}
		first, firstHeld = read, holding(read)
	}
	second, openError, readError := context.readGoInfoOnce(file, token.NewFileSet())
	if openError != nil || readError != nil {
		test.Fatalf("the build that lost the race opened with %v, read with %v", openError, readError)
	}
	if parsed := headers.Parsed(); parsed != 2 {
		test.Fatalf("the builds read %d headers, want two: the shared one and the one that lost the race", parsed)
	}
	if second.parsed != first.parsed || len(second.imports) != len(first.imports) || len(first.imports) == 0 ||
		&second.imports[0] != &first.imports[0] {
		// Alike in every value, so only the tree's and the imports' addresses tell the two reads apart.
		test.Error("the build that lost the race kept a read of its own, not the shared one")
	}
	if held := holding(first); held != firstHeld {
		test.Errorf("the shared read moved under the build holding it, from\n%s\nto\n%s", firstHeld, held)
	}
	// Compacted through readGoInfoOnce: the tree's name and doc kept and nothing else, no import's doc, no file set.
	if first.fset != nil || first.parsed.Name.Name != "held" || first.parsed.Doc.Text() != "Package held is read.\n" ||
		first.parsed.Decls != nil || first.parsed.Imports != nil || first.imports[0].doc != nil {
		test.Errorf("the shared read is not compacted: %s", firstHeld)
	}
	if !slices.Equal(positions(first), wantPositions) {
		test.Errorf("the shared read's imports sit at %q, want %q", positions(first), wantPositions)
	}
	if third, _, _ := context.readGoInfoOnce(file, token.NewFileSet()); third.parsed != first.parsed {
		test.Errorf("a build after both read %s, not the shared read", holding(third))
	}
	if held := holding(first); held != firstHeld {
		test.Errorf("the shared read moved under the build holding it once a third build took it, to\n%s", held)
	}

	fileSet := token.NewFileSet()
	whole, wholeOpenError, wholeReadError := (&Context{Context: build.Default}).readGoInfoOnce(file, fileSet)
	if wholeOpenError != nil || wholeReadError != nil {
		test.Fatalf("the read without headers opened with %v, read with %v", wholeOpenError, wholeReadError)
	}
	if whole.fset != fileSet || len(whole.parsed.Decls) != 2 || len(whole.parsed.Imports) != 3 ||
		whole.imports[0].doc.Text() != "The doc of fmt's import.\n" ||
		whole.imports[2].doc.Text() != "The lone import's doc.\n" {
		test.Errorf("the read without headers is not the caller's whole: %s", holding(whole))
	}
	if !slices.Equal(positions(whole), wantPositions) {
		test.Errorf("the read without headers has its imports at %q, want %q", positions(whole), wantPositions)
	}
}
