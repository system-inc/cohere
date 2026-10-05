package program

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
// Not parallel: it kills a child writer 200ms after the writer's temporary appears, trusting the child to
// reach its rename hook in that margin, and CPU taken by parallel siblings can eat it (it failed under load
// before). Serial, it runs before every parallel test in the package, with the cores to itself.
func TestAWriterKilledBeforeItsRenameLeavesTheOldTable(t *testing.T) {
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
		stopAt, _ := strconv.Atoi(os.Getenv("COHERE_TEST_STOP_AT_RENAME"))
		renames := 0
		beforeCacheTableRename = func(string) {
			renames++
			if renames == stopAt {
				time.Sleep(time.Minute)
			}
		}
		WriteCacheTable(directory, tableSaying("new"), identity, sections, PathAnchor{})
		return
	}

	for _, stopAt := range []int{1, 2} {
		// Not parallel: the same 200ms kill margin as the test, which the other case's child writer, run at the
		// same time, would share the CPU with.
		t.Run(fmt.Sprintf("killed at rename %d", stopAt), func(t *testing.T) {
			directory := t.TempDir()
			if err := WriteCacheTable(directory, tableSaying("old"), identity, sections, PathAnchor{}); err != nil {
				t.Fatal(err)
			}

			writer := exec.Command(os.Args[0], "-test.run=^TestAWriterKilledBeforeItsRenameLeavesTheOldTable$")
			writer.Env = append(os.Environ(), "COHERE_TEST_WRITE_TABLE="+directory, fmt.Sprintf("COHERE_TEST_STOP_AT_RENAME=%d", stopAt))
			if err := writer.Start(); err != nil {
				t.Fatal(err)
			}
			// Killed once a temporary is written in full: the moment the hook holds it.
			deadline := time.Now().Add(30 * time.Second)
			temporary := ""
			for temporary == "" && time.Now().Before(deadline) {
				entries, _ := os.ReadDir(directory)
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), ".cachetable-") {
						if information, err := entry.Info(); err == nil && information.Size() > 0 {
							temporary = entry.Name()
						}
					}
				}
				time.Sleep(20 * time.Millisecond)
			}
			if temporary == "" {
				writer.Process.Kill()
				t.Fatal("the writer never wrote its temporary, so the kill tests nothing")
			}
			time.Sleep(200 * time.Millisecond)
			writer.Process.Signal(syscall.SIGKILL)
			writer.Wait()

			read, err := ReadCacheTable(directory, identity, sections, PathAnchor{})
			if err != nil {
				t.Fatalf("the table is unreadable after the writer died before a rename: %v", err)
			}
			wantRun, wantFindings := "old", "/old.ts"
			if stopAt == 2 {
				wantRun = "new"
			}
			if run := read.Runs["--no-fix"]; run == nil || string(run.Output) != wantRun {
				t.Errorf("the run after the kill is %v, want the %s one", run, wantRun)
			}
			if read.Findings == nil || len(read.Findings.Entries) != 1 || read.Findings.Entries[0].Path != wantFindings {
				t.Errorf("the findings after the kill are %v, want the old ones", read.Findings)
			}
			if _, err := os.Stat(filepath.Join(directory, temporary)); err != nil {
				t.Errorf("the temporary is gone, so the writer was not stopped where this test meant: %v", err)
			}

			// And the next run rebuilds cleanly over what the killed one left, its temporary included.
			if err := WriteCacheTable(directory, tableSaying("rebuilt"), identity, sections, PathAnchor{}); err != nil {
				t.Fatalf("the write after the kill failed: %v", err)
			}
			rebuilt, err := ReadCacheTable(directory, identity, sections, PathAnchor{})
			if err != nil || string(rebuilt.Runs["--no-fix"].Output) != "rebuilt" || rebuilt.Findings.Entries[0].Path != "/rebuilt.ts" {
				t.Errorf("the table after the rebuild is not the rebuilt one: %v", err)
			}
		})
	}
}
