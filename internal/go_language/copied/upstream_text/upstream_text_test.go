package upstream_text

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// TestEveryRecordedFileMatchesItsDigest holds testdata/upstream to SHA256SUMS: every file listed is there with the
// digest listed, every file there is listed, and every source holds its LICENSE.
func TestEveryRecordedFileMatchesItsDigest(test *testing.T) {
	test.Parallel()
	upstream, recordedError := ReadRecorded(os.DirFS(RecordedDirectory))
	if recordedError != nil {
		test.Fatal(recordedError)
	}
	for _, problem := range upstream.Verify() {
		test.Error(problem)
	}
}

// TestPlantedCopies holds the check to both answers on planted copies of x/tools' driverutil/url.go: a faithful
// copy with a marked change passes, and an unmarked edit, a header with no until and a module with no recorded text
// each fail, naming what failed.
func TestPlantedCopies(test *testing.T) {
	test.Parallel()
	upstream, recordedError := ReadRecorded(os.DirFS(RecordedDirectory))
	if recordedError != nil {
		test.Fatal(recordedError)
	}
	for _, planted := range []struct {
		file string

		// saying are what the problems say, one each, in order; none for a copy that passes.
		saying []string
	}{
		{file: "faithful/url.go"},
		{file: "unmarked/url.go", saying: []string{"unmarked/url.go:24: differs from golang.org/x/tools@v0.50.0 " +
			"internal/analysis/driverutil/url.go:20 with no cohere: comment among the lines that differ"}},
		{file: "no-until/url.go", saying: []string{"no-until/url.go: reading the copy header: no cohere:until line " +
			"right below the cohere:copied line"}},
		{file: "unrecorded/url.go", saying: []string{"unrecorded/url.go: no upstream text is recorded for " +
			"example.com/unrecorded@v1.0.0 url.go"}},
	} {
		test.Run(planted.file, func(test *testing.T) {
			test.Parallel()
			contents, readError := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(planted.file)))
			if readError != nil {
				test.Fatalf("reading the planted copy: %v", readError)
			}
			_, declared, problems := Check(upstream, planted.file, contents)
			if !declared {
				test.Fatalf("%s declares no copy, so it plants nothing", planted.file)
			}
			if len(problems) != len(planted.saying) {
				test.Fatalf("got %d problems, want %d:\n%s", len(problems), len(planted.saying),
					strings.Join(problems, "\n"))
			}
			for index, problem := range problems {
				if !strings.HasPrefix(problem, planted.saying[index]) {
					test.Errorf("problem %d says\n%s\nwant it to open with\n%s", index, problem, planted.saying[index])
				}
			}
		})
	}
}

// TestPlantedRecordings holds the digest check to both answers on planted recorded trees: a faithful one passes,
// and text edited past its digest, a listed file missing, a file not listed, a source with no LICENSE and a
// malformed SHA256SUMS each fail, naming what failed. A copy held to edited text fails too, before any diff.
func TestPlantedRecordings(test *testing.T) {
	test.Parallel()
	const upstreamText = "package a\n"
	sum := sha256.Sum256([]byte(upstreamText))
	digest := hex.EncodeToString(sum[:])
	license := sha256.Sum256([]byte("BSD\n"))
	licensed := hex.EncodeToString(license[:]) + "  example.com/a@v1.0.0/LICENSE\n"
	for _, planted := range []struct {
		name  string
		files fstest.MapFS

		// saying are what the problems say, one each, in order; none for a tree that passes. readSaying is what
		// reading SHA256SUMS says, for a tree that cannot be read at all.
		saying     []string
		readSaying string
	}{
		{name: "faithful", files: fstest.MapFS{
			digestsFile:                    {Data: []byte(licensed + digest + "  example.com/a@v1.0.0/a.go\n")},
			readmeFile:                     {Data: []byte("# planted\n")},
			"example.com/a@v1.0.0/LICENSE": {Data: []byte("BSD\n")},
			"example.com/a@v1.0.0/a.go":    {Data: []byte(upstreamText)},
		}},
		{name: "edited past its digest", files: fstest.MapFS{
			digestsFile:                    {Data: []byte(licensed + digest + "  example.com/a@v1.0.0/a.go\n")},
			"example.com/a@v1.0.0/LICENSE": {Data: []byte("BSD\n")},
			"example.com/a@v1.0.0/a.go":    {Data: []byte("package b\n")},
		}, saying: []string{"testdata/upstream/example.com/a@v1.0.0/a.go: its sha256 is "}},
		{name: "a listed file missing", files: fstest.MapFS{
			digestsFile:                    {Data: []byte(licensed + digest + "  example.com/a@v1.0.0/a.go\n")},
			"example.com/a@v1.0.0/LICENSE": {Data: []byte("BSD\n")},
		}, saying: []string{"testdata/upstream/example.com/a@v1.0.0/a.go: reading the recorded text: "}},
		{name: "a file not listed", files: fstest.MapFS{
			digestsFile:                    {Data: []byte(licensed + digest + "  example.com/a@v1.0.0/a.go\n")},
			"example.com/a@v1.0.0/LICENSE": {Data: []byte("BSD\n")},
			"example.com/a@v1.0.0/a.go":    {Data: []byte(upstreamText)},
			"example.com/a@v1.0.0/b.go":    {Data: []byte(upstreamText)},
		}, saying: []string{"testdata/upstream/example.com/a@v1.0.0/b.go: no line of SHA256SUMS lists it"}},
		{name: "a source with no LICENSE", files: fstest.MapFS{
			digestsFile:                 {Data: []byte(digest + "  example.com/a@v1.0.0/a.go\n")},
			"example.com/a@v1.0.0/a.go": {Data: []byte(upstreamText)},
		}, saying: []string{"testdata/upstream/example.com/a@v1.0.0: no LICENSE is recorded beside its files"}},
		{name: "a malformed line", files: fstest.MapFS{
			digestsFile: {Data: []byte(digest + " example.com/a@v1.0.0/a.go\n")},
		}, readSaying: "testdata/upstream/SHA256SUMS:1: "},
		{name: "a path listed twice", files: fstest.MapFS{
			digestsFile: {Data: []byte(licensed + licensed)},
		}, readSaying: "testdata/upstream/SHA256SUMS:2: lists example.com/a@v1.0.0/LICENSE a second time"},
	} {
		test.Run(planted.name, func(test *testing.T) {
			test.Parallel()
			upstream, recordedError := ReadRecorded(planted.files)
			if planted.readSaying != "" {
				if recordedError == nil || !strings.HasPrefix(recordedError.Error(), planted.readSaying) {
					test.Fatalf("reading SHA256SUMS: got %v, want an error opening with %s", recordedError,
						planted.readSaying)
				}
				return
			}
			if recordedError != nil {
				test.Fatal(recordedError)
			}
			problems := upstream.Verify()
			if len(problems) != len(planted.saying) {
				test.Fatalf("got %d problems, want %d:\n%s", len(problems), len(planted.saying),
					strings.Join(problems, "\n"))
			}
			for index, problem := range problems {
				if !strings.HasPrefix(problem, planted.saying[index]) {
					test.Errorf("problem %d says\n%s\nwant it to open with\n%s", index, problem, planted.saying[index])
				}
			}
		})
	}

	// A copy is held to recorded text only once the text is its digest's, so an edit to both at once cannot pass.
	upstream, recordedError := ReadRecorded(fstest.MapFS{
		digestsFile:                 {Data: []byte(digest + "  example.com/a@v1.0.0/a.go\n")},
		"example.com/a@v1.0.0/a.go": {Data: []byte("package b\n")},
	})
	if recordedError != nil {
		test.Fatal(recordedError)
	}
	copyText := "// cohere:copied example.com/a@v1.0.0 a.go\n// cohere:until never: planted.\n\npackage b\n"
	_, declared, problems := Check(upstream, "planted/a.go", []byte(copyText))
	if !declared || len(problems) != 1 || !strings.Contains(problems[0], "its sha256 is ") {
		test.Errorf("a copy held to text edited past its digest: declared %t, problems %q, want one naming the "+
			"digest", declared, problems)
	}
}

// TestUnmarkedChangesFindsEachRunOnce diffs small line lists, so the hunks the copies rely on are pinned apart
// from any file: an insertion, a replacement and a drop each count as one run, marked by a cohere: comment
// anywhere in it, and the lines around a marked run are not swept into it.
func TestUnmarkedChangesFindsEachRunOnce(test *testing.T) {
	test.Parallel()
	upstream := []string{"a", "}", "", "b", "c", "}", "d"}
	for _, diffed := range []struct {
		name     string
		ours     []string
		expected []unmarkedChange
	}{
		{name: "identical", ours: upstream},
		{name: "a marked insertion with braces in it", ours: []string{"a", "}", "", "// cohere: x", "if x {", "}", "",
			"b", "c", "}", "d"}},
		{name: "an unmarked insertion", ours: []string{"a", "}", "", "x", "b", "c", "}", "d"},
			expected: []unmarkedChange{{oursLine: 4, upstreamLine: 4}}},
		{name: "a marked replacement", ours: []string{"a", "}", "", "\t// cohere: why", "B", "c", "}", "d"}},
		{name: "an unmarked replacement", ours: []string{"a", "}", "", "B", "c", "}", "d"},
			expected: []unmarkedChange{{oursLine: 4, upstreamLine: 4}}},
		{name: "a marked drop", ours: []string{"a", "}", "", "// cohere: b and c are dropped", "}", "d"}},
		{name: "an unmarked drop", ours: []string{"a", "}", "", "}", "d"},
			expected: []unmarkedChange{{oursLine: 4, upstreamLine: 4}}},
		{name: "an unmarked edit beside a marked one", ours: []string{"a", "// cohere: x", "}", "", "B", "c", "}", "d"},
			expected: []unmarkedChange{{oursLine: 5, upstreamLine: 4}}},
		{name: "a marker in code is no comment", ours: []string{"a", "}", "", `x := "cohere: no"`, "b", "c", "}", "d"},
			expected: []unmarkedChange{{oursLine: 4, upstreamLine: 4}}},
		{name: "an unmarked edit at the end", ours: []string{"a", "}", "", "b", "c", "}", "D"},
			expected: []unmarkedChange{{oursLine: 7, upstreamLine: 7}}},
	} {
		test.Run(diffed.name, func(test *testing.T) {
			test.Parallel()
			changes := unmarkedChanges(diffed.ours, upstream)
			if !slices.Equal(changes, diffed.expected) {
				test.Errorf("got %+v, want %+v", changes, diffed.expected)
			}
		})
	}
}
