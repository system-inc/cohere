package program

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A missed run's check stats every input, and the content pack validates against those stats rather than taking
// each again (#kdee854). That is sound only through the run-cache clock, so this follows the whole chain: the
// check notes a file's stat; the file changes; a pack trusting the check serves the bytes the check saw, where a
// pack statting afresh refuses them; and recording that run is refused, since the change postdates the run's
// clock, so the next run reads the new bytes and nothing replays the old verdict.
func TestAPackTrustsTheCheckStatsAndTheRunClockRefusesAChangeAfterThem(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	cache := filepath.Join(root, "cache")
	if createDirectoryError := os.MkdirAll(cache, 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	file := filepath.Join(root, "a.ts")
	if writeError := os.WriteFile(file, []byte("export const a = 1;\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}

	// A previous run read the file and kept it in the pack, and recorded it as an input.
	identity, statted := statIdentity(file)
	if !statted {
		test.Skip("this platform gives the content pack no identity, so it serves nothing")
	}
	recording, openRecordingError := OpenContentPack(cache)
	if openRecordingError != nil {
		test.Fatal(openRecordingError)
	}
	recording.noteRead(file, identity, true, "export const a = 1;\n")
	if saveError := recording.Save(); saveError != nil {
		test.Fatal(saveError)
	}
	recorded, recordRunCacheError := RecordRunCache(
		"key",
		[]string{file},
		nil,
		nil,
		nil,
		[]byte("verdict"),
		0,
		time.Time{},
	)
	if recordRunCacheError != nil {
		test.Fatal(recordRunCacheError)
	}

	// This run: the clock starts, then the check stats every input and notes them.
	readSince := time.Now()
	snapshot := NewStatSnapshot()
	if checkNotingError := recorded.CheckNoting("key", snapshot); checkNotingError != nil {
		test.Fatalf("an unchanged input missed: %v", checkNotingError)
	}
	if _, noted := snapshot.identityOf(file); !noted {
		test.Fatal("the check noted no stat for the input, so trusting it proves nothing")
	}

	// The file changes after the check, at the same size.
	time.Sleep(10 * time.Millisecond)
	if writeError := os.WriteFile(file, []byte("export const a = 2;\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}

	trusting, openTrustingError := OpenContentPack(cache)
	if openTrustingError != nil {
		test.Fatal(openTrustingError)
	}
	trusting.TrustStats(snapshot)
	if served, ok := trusting.serve(file); !ok || served != "export const a = 1;\n" {
		test.Errorf("a pack trusting the check served %q (%v), want the bytes the check saw", served, ok)
	}
	statting, openStattingError := OpenContentPack(cache)
	if openStattingError != nil {
		test.Fatal(openStattingError)
	}
	if served, ok := statting.serve(file); ok {
		test.Errorf("a pack statting afresh served %q, so the snapshot was not what decided above", served)
	}

	if _, recordRunCacheError := RecordRunCache(
		"key",
		[]string{file},
		nil,
		nil,
		nil,
		[]byte("verdict"),
		0,
		readSince,
	); recordRunCacheError == nil ||
		!strings.Contains(recordRunCacheError.Error(), "changed after the run began reading") {
		test.Errorf(
			"a run that read the file before its change was recorded anyway (%v), so the next run could replay it",
			recordRunCacheError,
		)
	}
}
