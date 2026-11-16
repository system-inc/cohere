package go_engine

import (
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// heldReadsDeadline bounds every wait on the builds' selection here: it reads a few files and finishes in
// milliseconds, so only a selection waiting on a read that never comes reaches it.
const heldReadsDeadline = 30 * time.Second

// For every census the fixtures hold, and one with a file nothing can read and one whose constraint does not parse,
// the builds project.BuildsFor reads from the description's reads, taken as readOurs reads them beside it, are the
// ones it reads from the disk, as it did before (#ghb3rcn): the builds, each unbuilt file and its reason, and the first
// error met, byte for byte.
func TestTheBuildsReadFromTheDescriptionsReadsAreTheDisks(test *testing.T) {
	test.Parallel()
	ports, portsError := project.ReadPorts(".", nil)
	if portsError != nil {
		test.Fatal(portsError)
	}
	host := project.HostBuild(&build.Default, nil)
	entries, readError := os.ReadDir("testdata")
	if readError != nil {
		test.Fatal(readError)
	}
	var roots []string
	for _, entry := range entries {
		root := filepath.Join("testdata", entry.Name())
		if _, statError := os.Stat(filepath.Join(root, "go.mod")); entry.IsDir() && statError == nil {
			roots = append(roots, root)
		}
	}
	if !slices.Contains(roots, filepath.Join("testdata", "other-builds")) {
		test.Fatalf("the fixtures read are %v, want the other-builds fixture among them", roots)
	}
	unreadableRoot, files := manyFileProject(test, 4)
	writeTestFile(test, filepath.Join(unreadableRoot, "only_freebsd.go"), "//go:build freebsd\n\npackage sample\n")
	if chmodError := os.Chmod(files[2], 0); chmodError != nil {
		test.Fatal(chmodError)
	}
	test.Cleanup(func() {
		// Ignored: the directory goes with the test either way, and a file left unreadable only fails its removal.
		_ = os.Chmod(files[2], 0o644)
	})
	unparsedRoot, _ := manyFileProject(test, 2)
	writeTestFile(test, filepath.Join(unparsedRoot, "unparsed.go"), "//go:build linux &&\n\npackage sample\n")
	roots = append(roots, unreadableRoot, unparsedRoot)

	failed := 0
	for _, root := range roots {
		workspace, workspaceError := project.ReadWorkspace(root)
		if workspaceError != nil {
			test.Fatal(workspaceError)
		}
		census, censusError := project.TakeCensus(workspace)
		if censusError != nil {
			test.Fatal(censusError)
		}
		builds, unbuilt, buildsError := project.BuildsFor(census, host, ports, nil, os.ReadFile)

		// As describeProject reads them: the selection takes each file through the slots as readOurs fills them.
		reads := newHeldReads(census.Ours)
		// Written by the selection before selected is closed, and read only after.
		var heldBuilds []project.Build
		var heldUnbuilt []project.Unbuilt
		var heldError error
		selected := make(chan struct{})
		go func() {
			defer close(selected)
			heldBuilds, heldUnbuilt, heldError = project.BuildsFor(census, host, ports, nil, reads.read)
		}()
		readOurs(reads, os.ReadFile, false)
		select {
		case <-selected:
		case <-time.After(heldReadsDeadline):
			test.Fatalf("%s: the selection did not finish in %v once every file was read", root, heldReadsDeadline)
		}

		if fmt.Sprint(heldBuilds) != fmt.Sprint(builds) {
			test.Errorf("%s: the builds from the reads are %v, from the disk %v", root, heldBuilds, builds)
		}
		if fmt.Sprintf("%q", heldUnbuilt) != fmt.Sprintf("%q", unbuilt) {
			test.Errorf("%s: the unbuilt files from the reads are %q, from the disk %q", root, heldUnbuilt, unbuilt)
		}
		if fmt.Sprint(heldError) != fmt.Sprint(buildsError) {
			test.Errorf("%s: the selection from the reads failed with %v, from the disk with %v", root, heldError,
				buildsError)
		}
		if buildsError != nil {
			failed++
		}
	}
	if failed != 1 {
		test.Errorf("%d censuses failed their selection, want the unparsed constraint's alone", failed)
	}
}

// A run under `--other-builds` opens each file of its census once (#ghb3rcn): every file is read through the run's
// reader once, and the builds are the bytes it handed, never the disk's. The disk names freebsd where the reader
// names openbsd, so a selection that read the disk again would choose freebsd's build.
func TestARunWithBuildsReadsEachFileOnce(test *testing.T) {
	test.Parallel()
	root, _ := manyFileProject(test, 8)
	chosen := filepath.Join(root, "chosen.go")
	writeTestFile(test, chosen, "//go:build freebsd\n\npackage sample\n")
	request, parseError := parseInvocation([]string{"--contract", fmt.Sprint(contract.Version), "--root", root,
		"--no-fix", "--other-builds"})
	if parseError != nil {
		test.Fatal(parseError)
	}
	var lock sync.Mutex
	readCounts := map[string]int{}
	request.readFromDisk = func(file string) ([]byte, error) {
		lock.Lock()
		readCounts[file]++
		lock.Unlock()
		if file == chosen {
			return []byte("//go:build openbsd\n\npackage sample\n"), nil
		}
		return os.ReadFile(file)
	}
	described, describeError := describeProject(request)
	if describeError != nil {
		test.Fatal(describeError)
	}
	// What the description started beside its reads ends before the test and its directory do.
	test.Cleanup(described.awaitStarted)
	if described.buildsError != nil {
		test.Fatal(described.buildsError)
	}
	for _, file := range described.census.Ours {
		if readCounts[file] != 1 {
			test.Errorf("%s was read %d times, want once", file, readCounts[file])
		}
	}
	var operatingSystems []string
	for _, each := range described.builds[1:] {
		operatingSystems = append(operatingSystems, each.OperatingSystem)
	}
	if !slices.Contains(operatingSystems, "openbsd") || slices.Contains(operatingSystems, "freebsd") {
		test.Errorf("the extra builds are on %v, want openbsd's, from the bytes the run read, and never freebsd's, "+
			"from the disk's", operatingSystems)
	}
}

// A selection waiting on a file whose read never comes returns once the description lets its reads go (abandon), as it
// does on every way out, a refusal or a panic among them: the file is unbuilt as never read, and nothing hangs.
func TestTheBuildsSelectionReturnsWhenAReadNeverComes(test *testing.T) {
	test.Parallel()
	root, files := manyFileProject(test, 3)
	census := project.Census{Root: root, Files: files, Ours: files}
	reads := newHeldReads(census.Ours)
	// The first file is read, and the rest never are.
	contents, readError := os.ReadFile(files[0])
	reads.hold(0, contents, readError)
	// Written by the selection before selected is closed, and read only after.
	var unbuilt []project.Unbuilt
	var buildsError error
	selected := make(chan struct{})
	go func() {
		defer close(selected)
		_, unbuilt, buildsError = project.BuildsFor(census, project.HostBuild(&build.Default, nil), nil, nil,
			reads.read)
	}()
	reads.abandon()
	select {
	case <-selected:
	case <-time.After(heldReadsDeadline):
		test.Fatalf("the selection did not return in %v once the reads were let go", heldReadsDeadline)
	}
	if buildsError != nil {
		test.Fatal(buildsError)
	}
	var unbuiltFiles []string
	for _, each := range unbuilt {
		unbuiltFiles = append(unbuiltFiles, each.File)
		if want := "reading " + each.File + ": " + errorNeverRead.Error(); each.Reason != want {
			test.Errorf("%s is unbuilt as %q, want %q", each.File, each.Reason, want)
		}
	}
	if !slices.Equal(unbuiltFiles, files[1:]) {
		test.Errorf("the unbuilt files are %v, want the two never read, %v", unbuiltFiles, files[1:])
	}
}

// writeTestFile writes contents to path, failing the test when it cannot.
func writeTestFile(test *testing.T, path string, contents string) {
	test.Helper()
	if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
}
