package guard

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

// Every Go file cohere owns is gofmt-clean, so `go test ./...` refuses an unformatted tree before a
// release does. The release's first step is `gofmt -l ./command ./internal`, and on 2026-10-03 head
// failed it (62ac5d74) with nothing in the suite to say so.
//
// Wider than the release's set: every `.go` file in the module, testdata, TypeScript-shim/ and the shim
// generators included, since the generator emitting unformatted shims is a regression of the same kind.
// Excluded are only what cohere does not own:
//   - the TypeScript submodule, which is upstream's;
//   - third_party, whose vendored copies are kept byte for byte as their authors released them (#j3bfnyt);
//   - directories whose name begins with a dot, `.cache` among them, which holds snapshots and worktrees
//     of other trees;
//   - node_modules, which is npm's.
//
// Files whose name begins with a dot are skipped too, as gofmt skips them. go/format is gofmt's own printer,
// run in process and on every core rather than as a command. A file that does not parse fails too, since
// gofmt stops the release on it as surely.
func TestEveryGoFileCohereOwnsIsGofmtClean(test *testing.T) {
	test.Parallel()

	// This package sits two directories below the module root.
	module, absolutePathError := filepath.Abs(filepath.Join("..", ".."))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}

	var files []string
	walkError := filepath.WalkDir(module, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if entry.IsDir() {
			if path != module && (path == filepath.Join(module, "TypeScript") ||
				path == filepath.Join(module, "third_party") ||
				strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".go") && !strings.HasPrefix(entry.Name(), ".") {
			files = append(files, path)
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	// The same guard the release keeps: a walk that found nothing would pass over a tree nobody examined.
	// The release's own set is required in particular, so a walk that lost it cannot pass on the rest.
	var underReleaseSet int
	for _, path := range files {
		relative, relativeError := filepath.Rel(module, path)
		if relativeError != nil {
			test.Fatal(relativeError)
		}
		if strings.HasPrefix(filepath.ToSlash(relative), "command/") ||
			strings.HasPrefix(filepath.ToSlash(relative), "internal/") {
			underReleaseSet++
		}
	}
	if underReleaseSet == 0 {
		test.Fatal("found no Go files under command/ and internal/, so a clean answer would mean nothing")
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
				// A worker cannot end the test, so a path Rel cannot shorten is named in full instead.
				named := path
				if relative, relativeError := filepath.Rel(module, path); relativeError == nil {
					named = filepath.ToSlash(relative)
				}
				unformatted = append(unformatted, named+problem)
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
		test.Fatalf(
			"%d of %d Go files are not gofmt-clean, and any under command/ or internal/ would fail the release's first step; run `gofmt -w` on them:\n  %s",
			len(unformatted),
			len(files),
			strings.Join(unformatted, "\n  "),
		)
	}
}

// gofmtProblem says what is wrong with one file, or nothing when gofmt would leave it as it is.
func gofmtProblem(path string) string {
	source, readError := os.ReadFile(path)
	if readError != nil {
		return " (unreadable: " + readError.Error() + ")"
	}
	formatted, formatError := format.Source(source)
	if formatError != nil {
		return " (does not parse: " + formatError.Error() + ")"
	}
	if !bytes.Equal(source, formatted) {
		return " (not formatted)"
	}
	return ""
}
