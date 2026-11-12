package program_test

import (
	"bytes"
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
func TestARunIsNotRecordedOverAFileChangedWhileItRan(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	file := filepath.Join(directory, "a.ts")
	if writeError := os.WriteFile(file, []byte("export const a = 1;\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	readSince := time.Now()
	time.Sleep(10 * time.Millisecond)
	if writeError := os.WriteFile(file, []byte("export const a = 2;\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if _, recordRunCacheError := program.RecordRunCache(
		"key",
		[]string{file},
		nil,
		nil,
		nil,
		nil,
		0,
		readSince,
	); recordRunCacheError == nil ||
		!strings.Contains(recordRunCacheError.Error(), "a.ts changed after the run began reading") {
		test.Fatalf("a run that began reading before a.ts was saved was recorded (%v)", recordRunCacheError)
	}
	if _, recordRunCacheError := program.RecordRunCache(
		"key",
		[]string{file},
		nil,
		nil,
		nil,
		nil,
		0,
		time.Now(),
	); recordRunCacheError != nil {
		test.Fatalf("a run that began reading after the save was refused: %v", recordRunCacheError)
	}
}

// An input stamped at the very time the run began reading is refused, and one stamped a nanosecond before it is
// not. Linux stamps from a coarse clock, so a file added in the same tick as the run's clock carries the clock's
// own time; refused only when stamped after it, that file was replayed over on Home (#6cmmpm1).
func TestAnInputStampedAtTheClockIsRefused(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	file := filepath.Join(directory, "a.ts")
	if writeError := os.WriteFile(file, []byte("export const a = 1;\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	recorded, recordRunCacheError := program.RecordRunCache("key", []string{file}, nil, nil, nil, nil, 0, time.Time{})
	if recordRunCacheError != nil {
		test.Fatal(recordRunCacheError)
	}
	// The latest stamp among the inputs, a.ts and the directory holding it, which this test's clock lands on.
	var latest int64
	for _, input := range recorded.Inputs {
		latest = max(latest, input.ModifiedNanoseconds, input.ChangedNanoseconds)
	}
	if latest == 0 {
		test.Fatal("no input carries a stamp, so the boundary below is not about one")
	}
	if _, recordRunCacheError := program.RecordRunCache(
		"key", []string{file}, nil, nil, nil, nil, 0, time.Unix(0, latest),
	); recordRunCacheError == nil ||
		!strings.Contains(recordRunCacheError.Error(), "changed after the run began reading") {
		test.Fatalf("an input stamped at the clock's own time was recorded (%v)", recordRunCacheError)
	}
	if _, recordRunCacheError := program.RecordRunCache(
		"key", []string{file}, nil, nil, nil, nil, 0, time.Unix(0, latest+1),
	); recordRunCacheError != nil {
		test.Fatalf("inputs stamped before the clock were refused: %v", recordRunCacheError)
	}
}

// Different bytes of the same size under the old modification time, as cp -p, rsync -t and touch -r leave
// a file, are a miss. Only the change time moved.
func TestASameSizeEditUnderARestoredModificationTimeIsAMiss(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	file := filepath.Join(directory, "a.ts")
	if writeError := os.WriteFile(file, []byte("export const a = 1;\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	cache, recordRunCacheError := program.RecordRunCache("key", []string{file}, nil, nil, nil, nil, 0, time.Time{})
	if recordRunCacheError != nil {
		test.Fatal(recordRunCacheError)
	}
	if checkError := cache.Check("key"); checkError != nil {
		test.Fatalf("an untouched file missed, so the miss below proves nothing: %v", checkError)
	}
	information, statError := os.Stat(file)
	if statError != nil {
		test.Fatal(statError)
	}
	time.Sleep(10 * time.Millisecond)
	if writeError := os.WriteFile(file, []byte("export const a = 9;\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if changeTimesError := os.Chtimes(file, information.ModTime(), information.ModTime()); changeTimesError != nil {
		test.Fatal(changeTimesError)
	}
	if checkError := cache.Check("key"); !errors.Is(checkError, program.ErrRunCacheMiss) {
		test.Fatalf("a same-size edit under the restored modification time was a hit (%v)", checkError)
	}
}

// A recorded directory is an input by its own signature, and its parent is not: the directory's change time
// and inode already catch it being replaced. Recording the parent made every replay depend on whatever else
// lives beside the project, ~/Projects on a developer's machine and the benchmark's work directory, where
// something is created all day (#r9jevk9). A file's directory is still recorded, which is what catches a file
// added beside one the run read.
func TestADirectoryInputDoesNotMakeItsParentOne(test *testing.T) {
	test.Parallel()
	parent := test.TempDir()
	project := filepath.Join(parent, "project")
	if createDirectoryError := os.MkdirAll(filepath.Join(project, "source"), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	file := filepath.Join(project, "source", "a.ts")
	if writeError := os.WriteFile(file, []byte("export const a = 1;\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	cache, recordRunCacheError := program.RecordRunCache(
		"key",
		[]string{project, file},
		nil,
		nil,
		nil,
		nil,
		0,
		time.Time{},
	)
	if recordRunCacheError != nil {
		test.Fatal(recordRunCacheError)
	}
	recorded := map[string]bool{}
	for _, input := range cache.Inputs {
		recorded[input.Path] = true
	}
	for _, want := range []string{project, file, filepath.Join(project, "source")} {
		if !recorded[want] {
			test.Errorf("%s is not recorded", want)
		}
	}
	if recorded[parent] {
		test.Errorf("the directory holding the project, %s, is recorded because the project directory is", parent)
	}
	if writeError := os.WriteFile(filepath.Join(parent, "beside.txt"), []byte("beside\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if checkError := cache.Check("key"); checkError != nil {
		test.Fatalf("a file created beside the project made the run miss: %v", checkError)
	}
}

// --cache-dump says, under each recorded run, whether it would replay now, and when it would not, which input
// changed: a miss is never silent (#r9jevk9).
func TestTheCacheDumpNamesTheInputThatKeepsARunFromReplaying(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	file := filepath.Join(directory, "a.ts")
	if writeError := os.WriteFile(file, []byte("export const a = 1;\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	cache, recordRunCacheError := program.RecordRunCache("key", []string{file}, nil, nil, nil, nil, 0, time.Time{})
	if recordRunCacheError != nil {
		test.Fatal(recordRunCacheError)
	}
	table := program.NewCacheTable()
	table.Runs["--no-fix"] = cache
	dump := func() string {
		var out bytes.Buffer
		program.DumpCacheTable(&out, directory, table, program.CacheTableIdentity{})
		return out.String()
	}
	if got := dump(); !strings.Contains(got, "now: every input as recorded") {
		test.Fatalf("an unchanged run is not reported as replayable:\n%s", got)
	}
	time.Sleep(10 * time.Millisecond)
	if writeError := os.WriteFile(file, []byte("export const a = 22;\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if got := dump(); !strings.Contains(got, "now: would not replay") || !strings.Contains(got, "a.ts changed size") {
		test.Fatalf("the dump does not name the input that changed:\n%s", got)
	}
}
