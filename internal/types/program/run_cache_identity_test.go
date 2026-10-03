package program_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/types/program"
)

// A file saved while a run is reading is never recorded against that run's verdict. The run may have read
// it before the save, and the stat taken when it records would sign the new bytes, so the next run would
// replay a verdict about bytes that are gone. The same recording made after the save, by a run that began
// reading after it, is fine, which is what makes the refusal mean something.
func TestARunIsNotRecordedOverAFileChangedWhileItRan(t *testing.T) {
	directory := t.TempDir()
	file := filepath.Join(directory, "a.ts")
	if err := os.WriteFile(file, []byte("export const a = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	readSince := time.Now()
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(file, []byte("export const a = 2;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := program.RecordRunCache("key", []string{file}, nil, nil, nil, 0, readSince); err == nil ||
		!strings.Contains(err.Error(), "a.ts changed after the run began reading") {
		t.Fatalf("a run that began reading before a.ts was saved was recorded (%v)", err)
	}
	if _, err := program.RecordRunCache("key", []string{file}, nil, nil, nil, 0, time.Now()); err != nil {
		t.Fatalf("a run that began reading after the save was refused: %v", err)
	}
}

// Different bytes of the same size under the old modification time, as cp -p, rsync -t and touch -r leave
// a file, are a miss. Only the change time moved.
func TestASameSizeEditUnderARestoredModificationTimeIsAMiss(t *testing.T) {
	directory := t.TempDir()
	file := filepath.Join(directory, "a.ts")
	if err := os.WriteFile(file, []byte("export const a = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache, err := program.RecordRunCache("key", []string{file}, nil, nil, nil, 0, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Check("key"); err != nil {
		t.Fatalf("an untouched file missed, so the miss below proves nothing: %v", err)
	}
	information, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(file, []byte("export const a = 9;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, information.ModTime(), information.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := cache.Check("key"); !errors.Is(err, program.ErrRunCacheMiss) {
		t.Fatalf("a same-size edit under the restored modification time was a hit (%v)", err)
	}
}
