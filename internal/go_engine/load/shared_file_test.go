package load

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// A head read into a pooled buffer is never written over by a later read (headBuffers): a file at least a head long
// keeps the buffer itself, so handing it back would let the next short file's read write its bytes into a head the
// run still holds, and a short file keeps a copy, so keeping the buffer would let the next read write into it. Each
// goroutine reads a long file, then many short ones, so a buffer handed back too early is taken by the reads that
// follow on the same core and by the other goroutines' reads, and every head is then held to its file's bytes.
func TestASharedHeadIsNeverWrittenOverByALaterRead(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	const readers = 8
	const shortPerReader = 64
	contents := map[string]string{}
	for reader := range readers {
		long := filepath.Join(directory, fmt.Sprintf("long%d.s", reader))
		// Each long file's head is one letter of its own, so a head written over by another file's bytes shows.
		contents[long] = strings.Repeat(string(rune('a'+reader)), 2*sharedFileHead)
		for index := range shortPerReader {
			short := filepath.Join(directory, fmt.Sprintf("short%d_%d.s", reader, index))
			// Each short file a length of its own too, so a copy cut to another file's length shows.
			contents[short] = fmt.Sprintf("// short %d %d\n", reader, index) + strings.Repeat("z", reader*index)
		}
	}
	for path, content := range contents {
		if writeError := os.WriteFile(path, []byte(content), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	shared := NewSharedReads()
	open := shared.openFile(nil)
	var group sync.WaitGroup
	failures := make(chan error, readers*(shortPerReader+1))
	for reader := range readers {
		group.Go(func() {
			paths := []string{filepath.Join(directory, fmt.Sprintf("long%d.s", reader))}
			for index := range shortPerReader {
				paths = append(paths, filepath.Join(directory, fmt.Sprintf("short%d_%d.s", reader, index)))
			}
			for _, path := range paths {
				opened, openError := open(path)
				if openError != nil {
					failures <- openError
					continue
				}
				// Closed unread: the head is what was read, kept in the shared table this test reads below.
				if closeError := opened.Close(); closeError != nil {
					failures <- closeError
				}
			}
		})
	}
	group.Wait()
	close(failures)
	for failure := range failures {
		test.Fatal(failure)
	}
	for path, content := range contents {
		want := content[:min(len(content), sharedFileHead)]
		if got := string(shared.files[path].head); got != want {
			test.Errorf("%s's shared head is %q, want its first %d bytes %q", filepath.Base(path),
				clipped(got), len(want), clipped(want))
		}
	}
}

// clipped is text cut to its first 40 bytes, so a failure names a head without printing all of it.
func clipped(text string) string {
	if len(text) <= 40 {
		return text
	}
	return text[:40] + "..."
}
