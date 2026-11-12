package go_engine

import (
	"bytes"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// TestReadOursKeepsTheFilesOrderWhicheverReadFinishesFirst holds the first file's read until every other has finished,
// so the reads end in another order than the files', and reads them as a serial read does all the same: the readable
// files in the files' order with their own bytes, and the unreadable one with its error. With one core the reads run
// one at a time, nothing can finish before the first, and the first is not held.
func TestReadOursKeepsTheFilesOrderWhicheverReadFinishesFirst(test *testing.T) {
	test.Parallel()
	const count = 64
	const unreadableIndex = 9
	files := make([]string, count)
	for index := range files {
		files[index] = fmt.Sprintf("/project/file_%02d.go", index)
	}
	refused := &fs.PathError{Op: "open", Path: files[unreadableIndex], Err: fs.ErrPermission}
	contentsOf := func(file string) []byte { return []byte("package sample // " + file + "\n") }
	serialRead := func(file string) ([]byte, error) {
		if file == files[unreadableIndex] {
			return nil, refused
		}
		return contentsOf(file), nil
	}

	holdsFirst := runtime.GOMAXPROCS(0) > 1
	var othersRead atomic.Int64
	othersDone := make(chan struct{})
	read := func(file string) ([]byte, error) {
		if file == files[0] && holdsFirst {
			select {
			case <-othersDone:
			case <-time.After(10 * time.Second):
				test.Error("the first file's read waited 10s for the others, so they were not read beside it")
			}
			return serialRead(file)
		}
		contents, readError := serialRead(file)
		if othersRead.Add(1) == count-1 {
			close(othersDone)
		}
		return contents, readError
	}
	readable, contents, unread := readOurs(files, read, false)

	var wantReadable []string
	wantContents := map[string][]byte{}
	var wantUnread []unreadFile
	for _, file := range files {
		fileContents, readError := serialRead(file)
		if readError != nil {
			wantUnread = append(wantUnread, unreadFile{file: file, readError: readError})
			continue
		}
		wantReadable = append(wantReadable, file)
		wantContents[file] = fileContents
	}
	if !slices.Equal(readable, wantReadable) {
		test.Errorf("readable files\n got %v\nwant %v", readable, wantReadable)
	}
	if !maps.EqualFunc(contents, wantContents, bytes.Equal) {
		test.Errorf("the contents read are not the serial read's: got %d files, want %d", len(contents),
			len(wantContents))
	}
	if !slices.Equal(unread, wantUnread) {
		test.Errorf("unreadable files\n got %v\nwant %v", unread, wantUnread)
	}
}

// TestDescribeProjectReadsAProjectOfManyFilesAsASerialReadDoes describes a project of many files, one of which nothing
// can read, and finds what a serial read of the census gives: the readable files in its order with their bytes, the
// unreadable one's record and its place in scope, and every file counted as ours.
func TestDescribeProjectReadsAProjectOfManyFilesAsASerialReadDoes(test *testing.T) {
	test.Parallel()
	root, files := manyFileProject(test, 48)
	unreadable := files[7]
	if chmodError := os.Chmod(unreadable, 0); chmodError != nil {
		test.Fatal(chmodError)
	}
	test.Cleanup(func() {
		// Ignored: the directory goes with the test either way, and a file left unreadable only fails its removal.
		_ = os.Chmod(unreadable, 0o644)
	})
	request, parseError := parseInvocation([]string{"--contract", fmt.Sprint(contract.Version), "--root", root,
		"--no-fix"})
	if parseError != nil {
		test.Fatal(parseError)
	}
	described, describeError := describeProject(request)
	if describeError != nil {
		test.Fatal(describeError)
	}

	var wantReadable []string
	wantContents := map[string][]byte{}
	var wantUnreadable []contract.UnreadableRecord
	for _, file := range described.census.Ours {
		contents, readError := os.ReadFile(file)
		if readError != nil {
			wantUnreadable = append(wantUnreadable,
				contract.UnreadableRecord{File: file, Error: unreadableSentence(readError)})
			continue
		}
		wantReadable = append(wantReadable, file)
		wantContents[file] = contents
	}
	if len(wantUnreadable) != 1 || wantUnreadable[0].File != unreadable {
		test.Fatalf("the fixture's unreadable files are %v, want %s alone", wantUnreadable, unreadable)
	}
	if !slices.Equal(described.readableOurs, wantReadable) {
		test.Errorf("readable files\n got %v\nwant %v", described.readableOurs, wantReadable)
	}
	if !maps.EqualFunc(described.contents, wantContents, bytes.Equal) {
		test.Errorf("the contents held are not the serial read's: got %d files, want %d", len(described.contents),
			len(wantContents))
	}
	if !slices.Equal(described.unreadable, wantUnreadable) {
		test.Errorf("unreadable records\n got %v\nwant %v", described.unreadable, wantUnreadable)
	}
	if !slices.Equal(described.inScope, files) {
		test.Errorf("files in scope\n got %v\nwant %v", described.inScope, files)
	}
	if described.record.FilesOurs != len(files) {
		test.Errorf("files ours: got %d, want %d", described.record.FilesOurs, len(files))
	}
}

// TestDescribeProjectHoldsTheEditorsBufferForItsSave describes the editor's save of one file of many and holds the
// buffer for that file, never the disk's bytes, and the disk's for every other.
func TestDescribeProjectHoldsTheEditorsBufferForItsSave(test *testing.T) {
	test.Parallel()
	root, files := manyFileProject(test, 48)
	saved := files[20]
	request, parseError := parseInvocation([]string{"--contract", fmt.Sprint(contract.Version), "--root", root,
		"--fix", "--stdin-filepath", saved})
	if parseError != nil {
		test.Fatal(parseError)
	}
	request.buffer = []byte("package sample\n\n// the editor's buffer, never saved\n")
	described, describeError := describeProject(request)
	if describeError != nil {
		test.Fatal(describeError)
	}
	if !bytes.Equal(described.contents[saved], request.buffer) {
		test.Errorf("the save's file holds %q, want the buffer %q", described.contents[saved], request.buffer)
	}
	if !slices.Equal(described.readableOurs, files) {
		test.Errorf("readable files\n got %v\nwant %v", described.readableOurs, files)
	}
}

// TestDescribeProjectUnderSingleThreadedReadsOneFileAtATime describes a project of many files under
// `--single-threaded`, holding the first file's read until another read starts beside it, which only a run reading
// more than one file at once ever starts, and finds no two reads in flight at once. A run that reads one at a time
// starts no other, and its first read is let go after a second.
func TestDescribeProjectUnderSingleThreadedReadsOneFileAtATime(test *testing.T) {
	test.Parallel()
	root, files := manyFileProject(test, 32)
	request, parseError := parseInvocation([]string{"--contract", fmt.Sprint(contract.Version), "--root", root,
		"--no-fix", "--single-threaded"})
	if parseError != nil {
		test.Fatal(parseError)
	}
	var inFlight atomic.Int64
	var mostInFlight atomic.Int64
	anotherStarted := make(chan struct{})
	var anotherOnce sync.Once
	request.readFromDisk = func(file string) ([]byte, error) {
		now := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			most := mostInFlight.Load()
			if now <= most || mostInFlight.CompareAndSwap(most, now) {
				break
			}
		}
		if file == files[0] {
			select {
			case <-anotherStarted:
			case <-time.After(time.Second):
			}
		} else {
			anotherOnce.Do(func() { close(anotherStarted) })
		}
		return os.ReadFile(file)
	}
	described, describeError := describeProject(request)
	if describeError != nil {
		test.Fatal(describeError)
	}
	if most := mostInFlight.Load(); most > 1 {
		test.Errorf("%d reads were in flight at once under --single-threaded, want one", most)
	}
	if !slices.Equal(described.readableOurs, files) {
		test.Errorf("readable files\n got %v\nwant %v", described.readableOurs, files)
	}
}

// manyFileProject is a module of count files of one package, and the files, sorted as the census sorts them.
func manyFileProject(test *testing.T, count int) (string, []string) {
	test.Helper()
	// Resolved, as the engine resolves its root to its real path (#yyv7nr2), so the files name the paths it reads.
	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	writeFile := func(path string, contents string) {
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	writeFile(filepath.Join(root, "go.mod"), "module example.com/sample\n\ngo 1.22\n")
	files := make([]string, count)
	for index := range files {
		files[index] = filepath.Join(root, fmt.Sprintf("file_%02d.go", index))
		writeFile(files[index], fmt.Sprintf("package sample\n\n// Value%02d is file %d's.\nconst Value%02d = %d\n",
			index, index, index, index))
	}
	return root, files
}
