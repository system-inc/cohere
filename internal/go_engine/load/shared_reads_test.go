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
		environment, environmentError := sourceEnvironment(directory, append([]string{"GOWORK=off"}, target...))
		if environmentError != nil {
			test.Fatal(environmentError)
		}
		alone, doubt := listInProcess(directory, environment, []string{"./..."}, nil, nil, nil)
		if doubt != "" {
			test.Fatalf("%v: the lister reading for itself did not answer: %s", target, doubt)
		}
		through, doubt := listInProcess(directory, environment, []string{"./..."}, nil, shared, nil)
		if doubt != "" {
			test.Fatalf("%v: the lister reading through the shared reads did not answer: %s", target, doubt)
		}
		if !reflect.DeepEqual(through, alone) {
			test.Errorf("%v: the listing through the shared reads differs from its own:\n%s", target,
				listingDifferences(through, alone))
		}
	}
	if len(shared.directories) == 0 || len(shared.files) == 0 {
		test.Errorf("the shared reads hold %d directories and %d files, so nothing was shared", len(shared.directories),
			len(shared.files))
	}
}

// A file longer than its shared head reads on from the disk, and a file the run holds is read from its held bytes,
// never the shared head, so a file written after its head was shared reads as written.
func TestASharedFileReadsOnPastItsHeadAndHeldBytesWin(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	long := filepath.Join(directory, "long.go")
	contents := "package long\n\n// " + strings.Repeat("x", 3*sharedFileHead) + "\n"
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
	written := "package long\n"
	held := Held(func(file string) ([]byte, bool) { return []byte(written), file == long })
	reader, openError := shared.openFile(held)(long)
	if openError != nil {
		test.Fatal(openError)
	}
	if read, readError := readAllAndClose(reader); readError != nil || read != written {
		test.Errorf("a held file read %q with %v, want its held bytes %q", read, readError, written)
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
