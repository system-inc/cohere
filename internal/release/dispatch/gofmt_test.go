package dispatch

import (
	"bytes"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

// Every Go file the release checks is gofmt-clean, so `go test ./...` refuses an unformatted tree before
// a release does. The release's first step is `gofmt -l ./command ./internal`, and on 2026-10-03 head
// failed it (62ac5d74) with nothing in the suite to say so.
//
// The same set the release checks: every `.go` file under command/ and internal/, testdata included,
// skipping only names that begin with a dot, as gofmt does. go/format is gofmt's own printer, run in
// process and on every core rather than as a command. A file that does not parse fails too, since gofmt
// stops the release on it as surely.
func TestEveryGoFileTheReleaseChecksIsGofmtClean(t *testing.T) {
	module, err := FindModuleDirectory()
	if err != nil {
		t.Fatal(err)
	}

	var files []string
	for _, root := range []string{"command", "internal"} {
		err := filepath.WalkDir(filepath.Join(module, root), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") && !strings.HasPrefix(entry.Name(), ".") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// The same guard the release keeps: a walk that found nothing would pass over a tree nobody examined.
	if len(files) == 0 {
		t.Fatal("found no Go files under command/ and internal/, so a clean answer would mean nothing")
	}

	var mutex sync.Mutex
	var unformatted []string
	work := make(chan string)
	var workers sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for path := range work {
				problem := gofmtProblem(path)
				if problem == "" {
					continue
				}
				mutex.Lock()
				relative, _ := filepath.Rel(module, path)
				unformatted = append(unformatted, filepath.ToSlash(relative)+problem)
				mutex.Unlock()
			}
		}()
	}
	for _, path := range files {
		work <- path
	}
	close(work)
	workers.Wait()

	if len(unformatted) > 0 {
		sort.Strings(unformatted)
		t.Fatalf("%d of %d Go files are not gofmt-clean, so the release's first step would fail; run `gofmt -w` on them:\n  %s",
			len(unformatted), len(files), strings.Join(unformatted, "\n  "))
	}
}

// gofmtProblem says what is wrong with one file, or nothing when gofmt would leave it as it is.
func gofmtProblem(path string) string {
	source, err := os.ReadFile(path)
	if err != nil {
		return " (unreadable: " + err.Error() + ")"
	}
	formatted, err := format.Source(source)
	if err != nil {
		return " (does not parse: " + err.Error() + ")"
	}
	if !bytes.Equal(source, formatted) {
		return " (not formatted)"
	}
	return ""
}
