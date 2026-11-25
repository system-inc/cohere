package load

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// sharedListingTargets are the environments a run of targetedModuleFiles might list under, the host's first as the
// engine lists it: three operating systems, a custom tag, and cgo both ways, each selecting other files of one
// directory.
var sharedListingTargets = [][]string{
	{"GOOS=darwin", "GOARCH=arm64", "CGO_ENABLED=1", "GOFLAGS="},
	{"GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0", "GOFLAGS="},
	{"GOOS=windows", "GOARCH=arm64", "CGO_ENABLED=0", "GOFLAGS="},
	{"GOOS=freebsd", "GOARCH=arm64", "CGO_ENABLED=0", "GOFLAGS="},
	{"GOOS=darwin", "GOARCH=arm64", "CGO_ENABLED=1", "GOFLAGS=-tags=cohere_listing_tag"},
	{"GOOS=darwin", "GOARCH=arm64", "CGO_ENABLED=0", "GOFLAGS="},
}

// Every build's listing through the reads the run's listings share is the listing it makes reading the disk itself,
// package for package and field for field (#yyvstj6): what a directory holds and what a file says are read once, the
// host's listing first, and each build's go/build still selects its own files from them, so the windows build lists
// its windows file and the tagged build its tagged one, as when each read for itself.
func TestEveryBuildsListingThroughSharedReadsIsItsOwn(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	writeListingFiles(test, directory, targetedModuleFiles)
	shared := NewSharedReads()
	for _, target := range sharedListingTargets {
		goEnvironment := goEnvironmentOf(test, directory, append([]string{"GOWORK=off"}, target...))
		alone, doubt := listInProcess(goEnvironment, []string{"./..."}, nil, nil, nil, nil)
		if doubt != "" {
			test.Fatalf("%v: the lister reading for itself did not answer: %s", target, doubt)
		}
		through, doubt := listInProcess(goEnvironment, []string{"./..."}, nil, shared, nil, nil)
		if doubt != "" {
			test.Fatalf("%v: the lister reading through the shared reads did not answer: %s", target, doubt)
		}
		if !reflect.DeepEqual(through, alone) {
			test.Errorf("%v: the listing through the shared reads differs from its own:\n%s", target,
				listingDifferences(through, alone))
		}
	}
	// A Go file's start is kept no longer, since its header's read is (Headers), which a test of its own holds.
	if len(shared.directories) == 0 || shared.headers.Parsed() == 0 {
		test.Errorf("the shared reads hold %d directories and read %d headers, so nothing was shared",
			len(shared.directories), shared.headers.Parsed())
	}
}

// A file longer than its shared head reads on from the disk, and a file the run holds is read from its held bytes,
// never the shared head, so a file written after its head was shared reads as written. The file is assembly, whose
// start go/build reads in every build; a Go file's header is read once a run (Headers), so no head of it is kept.
func TestASharedFileReadsOnPastItsHeadAndHeldBytesWin(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	long := filepath.Join(directory, "long.s")
	contents := "// " + strings.Repeat("x", 3*sharedFileHead) + "\nTEXT ·long(SB),0,$0\n"
	if writeError := os.WriteFile(long, []byte(contents), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	shared := NewSharedReads()
	for range 2 {
		reader, openError := shared.openFile(nil)(long)
		if openError != nil {
			test.Fatal(openError)
		}
		read, readError := readAllAndClose(reader)
		if readError != nil || read != contents {
			test.Fatalf("read %d bytes with %v, want the file's %d", len(read), readError, len(contents))
		}
	}
	written := "TEXT ·long(SB),0,$0\n"
	held := Held(func(file string) ([]byte, bool) { return []byte(written), file == long })
	reader, openError := shared.openFile(held)(long)
	if openError != nil {
		test.Fatal(openError)
	}
	if read, readError := readAllAndClose(reader); readError != nil || read != written {
		test.Errorf("a held file read %q with %v, want its held bytes %q", read, readError, written)
	}
}

// A file saved between its head's read and a read past the head is not spliced: the read past the head fails, naming
// the file and that it changed, and go/build never parses a header that never existed.
func TestAFileChangedPastItsHeadIsNeverSpliced(test *testing.T) {
	test.Parallel()
	long := filepath.Join(test.TempDir(), "long.s")
	if writeError := os.WriteFile(long, []byte("// "+strings.Repeat("x", 3*sharedFileHead)+"\n"),
		0o644); writeError != nil {
		test.Fatal(writeError)
	}
	reader, openError := NewSharedReads().openFile(nil)(long)
	if openError != nil {
		test.Fatal(openError)
	}
	saved := "// " + strings.Repeat("y", 5*sharedFileHead) + "\n"
	if writeError := os.WriteFile(long, []byte(saved), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	read, readError := readAllAndClose(reader)
	if readError == nil || !strings.Contains(readError.Error(), long+" changed while the run read it") {
		test.Errorf("a file saved past its head read %d bytes with %v, want refused, naming it", len(read), readError)
	}
}

// readAllAndClose is everything reader gives, then closed.
func readAllAndClose(reader io.ReadCloser) (string, error) {
	read, readError := io.ReadAll(reader)
	if closeError := reader.Close(); readError == nil {
		readError = closeError
	}
	return string(read), readError
}

// listingDifferences names the first packages two listings disagree on.
func listingDifferences(listed *listing, wanted *listing) string {
	var differences []string
	if len(listed.packages) != len(wanted.packages) {
		differences = append(differences, "package counts differ")
	}
	for index := range min(len(listed.packages), len(wanted.packages)) {
		if !reflect.DeepEqual(listed.packages[index], wanted.packages[index]) {
			differences = append(differences, listed.packages[index].ImportPath+": "+
				fieldDifferences(listed.packages[index], wanted.packages[index]))
		}
	}
	return strings.Join(differences, "\n")
}
