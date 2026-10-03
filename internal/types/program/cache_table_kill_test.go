package program

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestAWriterKilledBeforeItsRenameLeavesTheOldTable stops a writer after it has written its temporary and
// before it renames it into place, kills it there, and reads the table: the old one, whole. A run that
// returns to its caller early writes its table after the caller has moved on, where nothing stops a kill
// (#zqsdzbq, early return).
func TestAWriterKilledBeforeItsRenameLeavesTheOldTable(t *testing.T) {
	if path := os.Getenv("COHERE_TEST_WRITE_TABLE"); path != "" {
		// The writer: a different table, held just before its rename until it is killed.
		beforeCacheTableRename = func(string) { time.Sleep(time.Minute) }
		table := NewCacheTable()
		table.Runs["new"] = &RunCache{Version: runCacheVersion, Key: "new", Output: []byte("the new table")}
		WriteCacheTable(path, table, CacheTableIdentity{SelfCommit: "kill-test"})
		return
	}

	directory := t.TempDir()
	path := filepath.Join(directory, "table.gob")
	identity := CacheTableIdentity{SelfCommit: "kill-test"}
	old := NewCacheTable()
	old.Runs["old"] = &RunCache{Version: runCacheVersion, Key: "old", Output: []byte("the old table")}
	if err := WriteCacheTable(path, old, identity); err != nil {
		t.Fatal(err)
	}

	writer := exec.Command(os.Args[0], "-test.run=^TestAWriterKilledBeforeItsRenameLeavesTheOldTable$")
	writer.Env = append(os.Environ(), "COHERE_TEST_WRITE_TABLE="+path)
	if err := writer.Start(); err != nil {
		t.Fatal(err)
	}
	// Killed once its temporary is written in full: the moment the hook holds it.
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

	read, err := ReadCacheTable(path, identity)
	if err != nil {
		t.Fatalf("the table is unreadable after the writer died before its rename: %v", err)
	}
	if read.Runs["old"] == nil || string(read.Runs["old"].Output) != "the old table" || read.Runs["new"] != nil {
		t.Fatalf("the table after the kill is not the old one: %v", read.Runs)
	}
	if _, err := os.Stat(filepath.Join(directory, temporary)); err != nil {
		t.Errorf("the temporary is gone, so the writer was not stopped where this test meant: %v", err)
	}
}
