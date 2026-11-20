package format

import (
	"bytes"
	"fmt"
	goformat "go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/bounded"
)

// gofmtSource prints with go/format's settings copied, a private bit among them, so it is held to go/format.Source byte
// for byte over every Go file of this repository, its testdata included, that formatFile would hand it: one whose
// package clause parses. A file that does not parse fails alike in both, with the same error.
func TestGofmtSourceIsGoFormatsOutputForEveryGoFile(test *testing.T) {
	test.Parallel()

	root := repositoryRoot(test)
	var files []string
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if entry.IsDir() {
			// TypeScript is the checkout of another project the repository links, not this repository's code; a
			// worktree's is a link, which the walk does not follow, and the shared checkout's a directory.
			if path == filepath.Join(root, "TypeScript") || entry.Name() == ".git" || entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() && strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	// Thousands of files are the repository's; a walk that found few started somewhere else.
	if len(files) < 1000 {
		test.Fatalf("found %d Go files under %s, want the repository's thousands", len(files), root)
	}

	// The repository is gofmt's already, so its files print as they are: these are printed differently, the private
	// bit's number literals among them, and a file that does not parse.
	planted := map[string]string{
		"unformatted":      unformattedSource,
		"broken":           brokenSource,
		"no trailing line": noTrailingNewlineSource,
		"numbers": "package fixture\n\nvar Numbers = []float64{0X1F, 0O17, 0B101, 1E5, 0x1P-2, 0XaBp+3, 017}\n" +
			"var Imaginary = 0X1Fi\n",
		"imports": "package fixture\n\nimport (\n\t\"strings\"\n\t\"bytes\"\n\t\"bytes\"\n)\n\n" +
			"var _ = bytes.Equal\nvar _ = strings.Cut\n",
	}
	for name, source := range planted {
		if difference, _ := gofmtDifference(name, []byte(source)); difference != "" {
			test.Error(difference)
		}
	}

	differences := make([]string, len(files))
	var compared atomic.Int64
	var failedAlike atomic.Int64
	bounded.ForEach(len(files), false, func(index int) {
		original, readError := os.ReadFile(files[index])
		if readError != nil {
			differences[index] = fmt.Sprintf("%s: %v", files[index], readError)
			return
		}
		// formatFile never hands gofmtSource a file whose package clause does not parse.
		headerMode := parser.PackageClauseOnly | parser.ParseComments | parser.SkipObjectResolution
		if _, headerError := parser.ParseFile(token.NewFileSet(), "", original, headerMode); headerError != nil {
			return
		}
		compared.Add(1)
		difference, failed := gofmtDifference(files[index], original)
		differences[index] = difference
		if failed && difference == "" {
			failedAlike.Add(1)
		}
	})
	for _, difference := range differences {
		if difference != "" {
			test.Error(difference)
		}
	}
	test.Logf("compared %d of %d files, %d of them failing alike", compared.Load(), len(files), failedAlike.Load())
}

// gofmtDifference says how gofmtSource's output for original, the file name, differs from go/format.Source's, empty
// when they are the same bytes or fail with the same error, and whether either failed. It prints into a buffer taken
// from gofmtBuffers, as formatFile does, so a buffer some earlier file left something in would show as a difference.
func gofmtDifference(name string, original []byte) (difference string, failed bool) {
	buffer, isBuffer := gofmtBuffers.Get().(*bytes.Buffer)
	if !isBuffer {
		buffer = new(bytes.Buffer)
	}
	defer func() {
		buffer.Reset()
		gofmtBuffers.Put(buffer)
	}()
	printed, printError := gofmtSource(original, buffer)
	want, wantError := goformat.Source(original)
	switch {
	case printError != nil || wantError != nil:
		if fmt.Sprint(printError) != fmt.Sprint(wantError) {
			return fmt.Sprintf("%s: gofmtSource failed with %v, go/format.Source with %v", name, printError,
				wantError), true
		}
		return "", true
	case !bytes.Equal(printed, want):
		return fmt.Sprintf("%s: gofmtSource differs from go/format.Source from line %d:\n%s\nwant:\n%s", name,
			firstDifferingLine(want, printed), printed, want), false
	}
	return "", false
}

// The output a caller keeps is never gofmtBuffers' buffer, which the next file formatted prints over: Output, under a
// run that keeps it, still reads gofmt's output after every buffer the pool hands out has been written over to its
// capacity.
func TestKeptOutputIsNeverAGofmtBuffer(test *testing.T) {
	test.Parallel()

	file := writeFile(test, unformattedSource)
	result := Run([]string{file}, Options{KeepOutput: true, SingleThreaded: true})
	if len(result.Changed) != 1 || string(result.Changed[0].Output) != reformattedSource {
		test.Fatalf("a run that keeps its output got %+v, want gofmt's output", result.Changed)
	}
	// The buffer the run handed back is the next one this goroutine takes, unless it moved to another core between, so
	// a few are taken, each written over.
	var taken []*bytes.Buffer
	for range 8 {
		buffer, isBuffer := gofmtBuffers.Get().(*bytes.Buffer)
		if !isBuffer {
			buffer = new(bytes.Buffer)
		}
		buffer.Write(bytes.Repeat([]byte{'x'}, max(buffer.Cap(), 1)))
		taken = append(taken, buffer)
	}
	for _, buffer := range taken {
		// Emptied, as formatFile hands buffers back, since other tests of the package format beside this one.
		buffer.Reset()
		gofmtBuffers.Put(buffer)
	}
	if string(result.Changed[0].Output) != reformattedSource {
		test.Fatalf("the kept output changed under a later use of the pool: %q", result.Changed[0].Output)
	}
}

// The bytes a run writes are each file's own gofmt output, never the pool's: many files, each different, formatted at
// once and written while other goroutines take buffers from gofmtBuffers and write them over, each file read back as
// gofmt prints it. A buffer handed back before its file was written is taken by one of them when the worker blocks in
// the write's system calls and its core runs another goroutine, which writes over the bytes the write still reads.
func TestWrittenFilesAreEachTheirOwnGofmtOutput(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	const count = 256
	files := make([]string, count)
	wants := make([][]byte, count)
	for index := range count {
		// Each file's length and names differ, so a file written from another's buffer reads wrong.
		source := fmt.Sprintf("package fixture\n\nvar  Count%d = %d\n", index, index) +
			strings.Repeat(fmt.Sprintf("\nvar  Total%d_%d = %d\n", index, index, index), index%17+1)
		want, formatError := goformat.Source([]byte(source))
		if formatError != nil {
			test.Fatal(formatError)
		}
		files[index] = filepath.Join(directory, fmt.Sprintf("file%03d.go", index))
		wants[index] = want
		if writeError := os.WriteFile(files[index], []byte(source), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	// Twice as many takers as cores, so a core a worker leaves in a system call has one waiting to run.
	stop := make(chan struct{})
	var takers sync.WaitGroup
	for range 2 * runtime.GOMAXPROCS(0) {
		takers.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				buffer, isBuffer := gofmtBuffers.Get().(*bytes.Buffer)
				if !isBuffer {
					buffer = new(bytes.Buffer)
				}
				buffer.Write(bytes.Repeat([]byte{'x'}, 4096))
				// Emptied before it goes back, as formatFile hands buffers back, so a file formatted next reads none
				// of it: only a buffer a file still reads after handing it back shows the x's.
				buffer.Reset()
				gofmtBuffers.Put(buffer)
				runtime.Gosched()
			}
		})
	}
	result := Run(files, Options{Write: true, Root: rootOf(test, directory)})
	close(stop)
	takers.Wait()
	if len(result.Changed) != count {
		test.Fatalf("changed %d of %d files: %+v", len(result.Changed), count, result.NotFormatted)
	}
	for index, file := range files {
		requireContents(test, file, string(wants[index]))
	}
}

// repositoryRoot is the directory holding the repository's go.work, found from the package's own directory, where a
// test runs, upward.
func repositoryRoot(test *testing.T) string {
	test.Helper()
	directory, directoryError := os.Getwd()
	if directoryError != nil {
		test.Fatal(directoryError)
	}
	for {
		if _, statError := os.Stat(filepath.Join(directory, "go.work")); statError == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			test.Fatal("no go.work above the package's directory")
		}
		directory = parent
	}
}
