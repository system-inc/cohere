package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A run's output is read as its record keeps it: the three finding lines counted and hashed sorted, whatever order
// the engine wrote them in, the project record's files in scope, the lint record's files walked, standard library
// entries, width and builds as written, and the line that is not JSON counted rather than fatal. The md5 is the one
// `grep '"kind":"finding"' testdata/engine_output.jsonl | LC_ALL=C sort | md5` prints.
func TestReadEngineOutputKeepsWhatTheRecordReads(test *testing.T) {
	test.Parallel()
	fixture, openError := os.Open(filepath.Join("testdata", "engine_output.jsonl"))
	if openError != nil {
		test.Fatal(openError)
	}
	// Ignored: the fixture was only read.
	defer func() { _ = fixture.Close() }()
	read, readError := readEngineOutput(fixture)
	if readError != nil {
		test.Fatal(readError)
	}
	if read.Findings != 3 || read.FindingsMd5 != "b567eaa54a76d200bad4a918d6991a19" {
		test.Errorf("findings %d, md5 %s; want 3, b567eaa54a76d200bad4a918d6991a19", read.Findings, read.FindingsMd5)
	}
	for _, count := range []struct {
		name string
		read *int
		want int
	}{
		{"filesInScope", read.FilesInScope, 37},
		{"filesWalked", read.FilesWalked, 36},
		{"standardLibraryFromBinary", read.StandardLibraryFromBinary, 180},
	} {
		if count.read == nil || *count.read != count.want {
			test.Errorf("%s is %s, want %d", count.name, describeCount(count.read), count.want)
		}
	}
	if read.BuildsAtOnce != 2 || read.BuildsWidthLiveBytes != 436663784 {
		test.Errorf("buildsAtOnce %d, buildsWidthLiveBytes %d; want 2, 436663784", read.BuildsAtOnce,
			read.BuildsWidthLiveBytes)
	}
	wantBuilds := `[{"target":"linux/amd64 cgo","host":true,"compiledFiles":36},` +
		`{"target":"darwin/amd64","host":false,"restoredFromHost":271,"declinedAtLoad":90,"checkedFromSource":166}]`
	if string(read.Builds) != wantBuilds {
		test.Errorf("builds read as\n%s\nwant them as written\n%s", read.Builds, wantBuilds)
	}
	if read.LinesNotJson != 1 {
		test.Errorf("%d lines not JSON, want 1", read.LinesNotJson)
	}
}

// A stream with no project or lint record leaves their counts missing, not 0, so a refusal can say which, and a
// stream of no findings hashes to the md5 of nothing.
func TestReadEngineOutputLeavesWhatItNeverReadMissing(test *testing.T) {
	test.Parallel()
	stream := "{\"kind\":\"provenance\"}\nnot json\n{\"kind\":\"summary\"}"
	read, readError := readEngineOutput(strings.NewReader(stream))
	if readError != nil {
		test.Fatal(readError)
	}
	if read.FilesInScope != nil || read.FilesWalked != nil || read.StandardLibraryFromBinary != nil {
		test.Errorf("counts read from records the stream does not hold: %+v", read)
	}
	if read.Findings != 0 || read.FindingsMd5 != "d41d8cd98f00b204e9800998ecf8427e" || read.LinesNotJson != 1 {
		test.Errorf("findings %d, md5 %s, not JSON %d; want 0, the empty md5, 1", read.Findings, read.FindingsMd5,
			read.LinesNotJson)
	}
}

// The findings md5 is of the lines sorted bytewise, so two runs that wrote the same findings in another order agree,
// and one that wrote another finding does not.
func TestSortedLinesMd5IsOfTheSortedLines(test *testing.T) {
	test.Parallel()
	forward := sortedLinesMd5([][]byte{[]byte("a"), []byte("b"), []byte("c")})
	backward := sortedLinesMd5([][]byte{[]byte("c"), []byte("a"), []byte("b")})
	if forward != backward {
		test.Errorf("one set of lines in two orders hashes to %s and %s", forward, backward)
	}
	// What `printf 'a\nb\nc\n' | md5` prints: each line with its newline, the last one's included.
	if forward != "40c53c58fdafacc83cfff6ee3d2f6d69" {
		test.Errorf("the md5 of a, b, c is %s, not that of the lines each with its newline", forward)
	}
	if other := sortedLinesMd5([][]byte{[]byte("a"), []byte("b"), []byte("d")}); other == forward {
		test.Error("another finding hashes the same")
	}
}

// A run's record in runs.jsonl carries what its output said beside the run's own fields, one flat object a line, so
// a reader of the file finds findingsMd5 where it finds wallSeconds.
func TestRunRecordKeepsTheEngineOutputFlat(test *testing.T) {
	test.Parallel()
	encoded, encodeError := json.Marshal(fixtureRecords()[0])
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	decoded := map[string]any{}
	if decodeError := json.Unmarshal(encoded, &decoded); decodeError != nil {
		test.Fatal(decodeError)
	}
	for _, key := range []string{"corpus", "wallSeconds", "findings", "findingsMd5", "filesInScope", "filesWalked",
		"standardLibraryFromBinary", "buildsAtOnce", "buildsWidthLiveBytes", "builds", "linesNotJson",
		"standardErrorBytes", "peakResidentBytes", "loadBefore"} {
		if _, present := decoded[key]; !present {
			test.Errorf("the record has no %s: %s", key, encoded)
		}
	}
}
