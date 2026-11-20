package build

import (
	"bytes"
	"fmt"
	"go/build"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// A read through a pooled buffered reader is every byte of its own file (bufferedReaders): reads of many files run at
// once, each taking a reader from the pool as the others hand theirs back, and every header, import and embed read is
// the one readGoInfo reads from the file's bytes alone. Headers run past the buffer's 4 KiB, and an embedding file reads
// on to its end, so a reader handed back while its read still fills it is refilled from another file under it.
func TestEveryReadThroughAPooledReaderIsItsOwnFile(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	const fileCount = 16
	paths := make([]string, fileCount)
	expected := make([]fileInfo, fileCount)
	for index := range fileCount {
		var source strings.Builder
		// A header comment of the file's own letter and length, up to past two buffers' worth, so a byte from another
		// file shows wherever it lands.
		fmt.Fprintf(&source, "// %s\n\npackage file%d\n\nimport (\n", strings.Repeat(string(rune('a'+index)), index*600),
			index)
		for imported := range index%4 + 1 {
			fmt.Fprintf(&source, "\t\"example.com/file%d/import%d\"\n", index, imported)
		}
		source.WriteString(")\n")
		if index%3 == 0 {
			// The embed's line sits past a long body, so the read of it runs through the buffer again.
			fmt.Fprintf(&source, "\nimport _ \"embed\"\n\nvar _ = `%s`\n\n//go:embed file%d.txt\nvar embedded string\n",
				strings.Repeat("b", 3*4096), index)
		}
		paths[index] = filepath.Join(directory, fmt.Sprintf("file%d.go", index))
		if writeError := os.WriteFile(paths[index], []byte(source.String()), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		// Read from the bytes alone, through a buffer of the read's own, before any pooled read starts.
		expected[index] = fileInfo{name: paths[index], fset: token.NewFileSet()}
		if readError := readGoInfo(bytes.NewReader([]byte(source.String())), &expected[index]); readError != nil {
			test.Fatalf("reading %s from its bytes: %v", paths[index], readError)
		}
	}
	context := &Context{Context: build.Default}
	const readers, rounds = 8, 20
	var group sync.WaitGroup
	failures := make(chan string, readers*rounds*fileCount)
	for reader := range readers {
		group.Go(func() {
			for round := range rounds {
				for offset := range fileCount {
					// Each reader starts at its own file, so the files read at once differ.
					index := (reader + round + offset) % fileCount
					read, openError, readError := context.openAndReadGoInfo(paths[index], token.NewFileSet())
					if openError != nil || readError != nil {
						failures <- fmt.Sprintf("%s: opened with %v, read with %v", paths[index], openError, readError)
						continue
					}
					if difference := readDifference(read, expected[index]); difference != "" {
						failures <- fmt.Sprintf("file%d.go read through a pooled reader: %s", index, difference)
					}
				}
			}
		})
	}
	group.Wait()
	close(failures)
	reported := 0
	for failure := range failures {
		// The first few say what moved; the count says how often.
		if reported < 3 {
			test.Error(failure)
		}
		reported++
	}
	if reported > 0 {
		test.Errorf("%d of %d reads were not their own file's", reported, readers*rounds*fileCount)
	}
}

// readDifference is what of read differs from wanted in what a build reads of it, the header, the imports' paths and
// the embeds' patterns, or empty when nothing does.
func readDifference(read fileInfo, wanted fileInfo) string {
	if !bytes.Equal(read.header, wanted.header) {
		return fmt.Sprintf("its header is %d bytes, starting %q, want %d, starting %q", len(read.header),
			read.header[:min(len(read.header), 24)], len(wanted.header), wanted.header[:min(len(wanted.header), 24)])
	}
	importPaths := func(info fileInfo) []string {
		var paths []string
		for _, each := range info.imports {
			paths = append(paths, each.path)
		}
		return paths
	}
	if !slices.Equal(importPaths(read), importPaths(wanted)) {
		return fmt.Sprintf("its imports are %q, want %q", importPaths(read), importPaths(wanted))
	}
	embedPatterns := func(info fileInfo) []string {
		var patterns []string
		for _, each := range info.embeds {
			patterns = append(patterns, each.pattern)
		}
		return patterns
	}
	if !slices.Equal(embedPatterns(read), embedPatterns(wanted)) {
		return fmt.Sprintf("its embeds are %q, want %q", embedPatterns(read), embedPatterns(wanted))
	}
	return ""
}
