package program

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// TestAWriterKilledBeforeItsRenameLeavesTheOldTable stops a writer after it has written a temporary and
// before it renames it into place, kills it there, and reads the table: every file whole, the old one or the
// new one, never half of either. A run that returns to its caller early writes its table after the caller
// has moved on, where nothing stops a kill (#zqsdzbq, early return).
//
// The writer replaces two files, a run and the findings, and is killed at the first rename and, separately,
// at the second. Before the first, both files are the old ones. Between them, the run is the new one and the
// findings the old: the files are not atomic with each other, and need not be, since each carries its own
// proof (#45ekc65).
//
// The writer says where it stopped: at the held rename it writes the temporary's name to a marker, whole, by
// rename, and then blocks, and the test kills it only once that marker exists. Nothing is timed. The test used to
// kill 200ms after the first temporary appeared, which under load could land after the writer had already renamed
// the run's temporary away (#dnmvs98).
func TestAWriterKilledBeforeItsRenameLeavesTheOldTable(test *testing.T) {
	test.Parallel()
	identity := CacheTableIdentity{SelfCommit: "kill-test"}
	sections := CacheTableSections{Runs: []string{"--no-fix"}, Findings: true}
	tableSaying := func(text string) *CacheTable {
		table := NewCacheTable()
		table.Runs["--no-fix"] = &RunCache{Version: runCacheVersion, Key: text, Output: []byte(text)}
		table.Findings = &LintCache{Version: lintCacheVersion, Entries: []LintCacheEntry{{Path: "/" + text + ".ts"}}}
		return table
	}
	if directory := os.Getenv("COHERE_TEST_WRITE_TABLE"); directory != "" {
		// The writer: the new table, held just before its nth rename until it is killed.
		stopAt, parseError := strconv.Atoi(os.Getenv("COHERE_TEST_STOP_AT_RENAME"))
		if parseError != nil {
			test.Fatalf("reading the rename to stop at: %v", parseError)
		}
		marker := os.Getenv("COHERE_TEST_HELD_MARKER")
		renames := 0
		beforeCacheTableRename = func(temporaryName string) {
			renames++
			if renames != stopAt {
				return
			}
			// Written beside the marker and renamed onto it, so the test never reads half a name.
			if writeError := os.WriteFile(marker+".partial", []byte(temporaryName), 0o644); writeError != nil {
				test.Fatalf("writing the held marker: %v", writeError)
			}
			if renameError := os.Rename(marker+".partial", marker); renameError != nil {
				test.Fatalf("renaming the held marker into place: %v", renameError)
			}
			time.Sleep(time.Minute)
		}
		writeCacheTableError := WriteCacheTable(directory, tableSaying("new"), identity, sections, PathAnchor{})
		if writeCacheTableError != nil {
			test.Fatal(writeCacheTableError)
		}
		return
	}

	for _, stopAt := range []int{1, 2} {
		test.Run(fmt.Sprintf("killed at rename %d", stopAt), func(test *testing.T) {
			test.Parallel()
			directory := test.TempDir()
			// Outside the table's directory, which the write after the kill cleans.
			marker := filepath.Join(test.TempDir(), "held")
			if writeCacheTableError := WriteCacheTable(
				directory,
				tableSaying("old"),
				identity,
				sections,
				PathAnchor{},
			); writeCacheTableError != nil {
				test.Fatal(writeCacheTableError)
			}

			writer := exec.Command(os.Args[0], "-test.run=^TestAWriterKilledBeforeItsRenameLeavesTheOldTable$")
			writer.Env = append(
				os.Environ(),
				"COHERE_TEST_WRITE_TABLE="+directory,
				fmt.Sprintf("COHERE_TEST_STOP_AT_RENAME=%d", stopAt),
				"COHERE_TEST_HELD_MARKER="+marker,
			)
			if startError := writer.Start(); startError != nil {
				test.Fatal(startError)
			}
			// Killed once the writer says it is held at the rename, naming the temporary it holds. The deadline
			// only ends a writer that never got there; it never decides when the kill lands.
			deadline := time.Now().Add(time.Minute)
			temporary := ""
			for temporary == "" && time.Now().Before(deadline) {
				if held, readError := os.ReadFile(marker); readError == nil {
					temporary = string(held)
				}
				time.Sleep(10 * time.Millisecond)
			}
			if temporary == "" {
				// Ignored: this only stops a writer that never got there, and the test fails on the next line
				// whatever the kill and the wait say.
				writer.Process.Kill()
				// Ignored: the writer was just killed, so Wait reports that, and the test fails next either way.
				writer.Wait()
				test.Fatal("the writer never reached its held rename, so the kill tests nothing")
			}
			if signalError := writer.Process.Signal(syscall.SIGKILL); signalError != nil {
				test.Fatalf("killing the writer held at its rename: %v", signalError)
			}
			// Ignored: a killed writer's Wait reports the kill, which is what the test did on purpose.
			writer.Wait()

			read, readCacheTableError := ReadCacheTable(directory, identity, sections, PathAnchor{})
			if readCacheTableError != nil {
				test.Fatalf("the table is unreadable after the writer died before a rename: %v", readCacheTableError)
			}
			wantRun, wantFindings := "old", "/old.ts"
			if stopAt == 2 {
				wantRun = "new"
			}
			if run := read.Runs["--no-fix"]; run == nil || string(run.Output) != wantRun {
				test.Errorf("the run after the kill is %v, want the %s one", run, wantRun)
			}
			if read.Findings == nil ||
				len(read.Findings.Entries) != 1 ||
				read.Findings.Entries[0].Path != wantFindings {
				test.Errorf("the findings after the kill are %v, want the old ones", read.Findings)
			}
			if _, statError := os.Stat(temporary); statError != nil {
				test.Errorf("the temporary is gone, so the writer was not stopped where this test meant: %v", statError)
			}

			// And the next run rebuilds cleanly over what the killed one left, its temporary included.
			if writeCacheTableError := WriteCacheTable(
				directory,
				tableSaying("rebuilt"),
				identity,
				sections,
				PathAnchor{},
			); writeCacheTableError != nil {
				test.Fatalf("the write after the kill failed: %v", writeCacheTableError)
			}
			rebuilt, readCacheTableError := ReadCacheTable(directory, identity, sections, PathAnchor{})
			if readCacheTableError != nil ||
				string(rebuilt.Runs["--no-fix"].Output) != "rebuilt" ||
				rebuilt.Findings.Entries[0].Path != "/rebuilt.ts" {
				test.Errorf("the table after the rebuild is not the rebuilt one: %v", readCacheTableError)
			}
		})
	}
}
