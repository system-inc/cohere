package program_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/types/program"
)

// sampleCacheTable holds both sections, so a test that loses one cannot pass by looking only at the other.
func sampleCacheTable() *program.CacheTable {
	table := program.NewCacheTable()
	table.Runs["--no-fix"] = &program.RunCache{
		Version: 3,
		Key:     "a-key",
		Inputs: []program.RunCacheInput{
			{Path: "/project/source/a.ts", Exists: true, Size: 20, ModifiedNanoseconds: 1},
			{Path: "/project/source", Directory: true, Exists: true, ModifiedNanoseconds: 2},
			{Path: "/project/missing.json"},
		},
		Output:                  []byte("lint: 1 findings\n"),
		Errors:                  []byte("note: something\n"),
		RecordedUnixNanoseconds: 42,
		ExitCode:                1,
	}
	table.Findings = sampleLintCache()
	table.Formatted = &program.FormatSection{
		Key:     "format-key",
		Entries: map[string]program.FormatEntry{"/project/source/a.ts": {Sum: "sum", Options: "options", Size: 20, ModifiedNanoseconds: 7}},
	}
	return table
}

// TestCacheTableRoundTripsBothSections is the table as a whole, through the encoding and back.
func TestCacheTableRoundTripsBothSections(t *testing.T) {
	encoded, err := program.EncodeCacheTable(sampleCacheTable(), testIdentity)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	decoded, err := program.DecodeCacheTable(encoded, testIdentity)
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}

	run := decoded.Runs["--no-fix"]
	if run == nil {
		t.Fatal("the recorded run did not survive")
	}
	if run.Key != "a-key" || run.ExitCode != 1 || run.RecordedUnixNanoseconds != 42 || run.Version != 3 {
		t.Errorf("run fields: key %q, exit %d, recorded %d, version %d", run.Key, run.ExitCode, run.RecordedUnixNanoseconds, run.Version)
	}
	if string(run.Output) != "lint: 1 findings\n" || string(run.Errors) != "note: something\n" {
		t.Errorf("run streams: %q / %q", run.Output, run.Errors)
	}
	if len(run.Inputs) != 3 || !run.Inputs[1].Directory || run.Inputs[2].Exists || run.Inputs[0].Size != 20 {
		t.Errorf("run inputs: %+v", run.Inputs)
	}
	if decoded.Findings == nil || len(decoded.Findings.Entries) != 2 {
		t.Fatalf("the findings section did not survive beside the run: %+v", decoded.Findings)
	}
	if decoded.Formatted == nil || decoded.Formatted.Key != "format-key" ||
		decoded.Formatted.Entries["/project/source/a.ts"] != (program.FormatEntry{Sum: "sum", Options: "options", Size: 20, ModifiedNanoseconds: 7}) {
		t.Errorf("the format section did not survive: %+v", decoded.Formatted)
	}
}

// TestCacheTableDiscardsWhatItCannotTrust is every way a file can be wrong, each of which must be a
// discard with a reason rather than a table assembled out of the wrong bytes. A finding pointing at the
// wrong rule and range is worse than no finding, because it reads as a real result.
func TestCacheTableDiscardsWhatItCannotTrust(t *testing.T) {
	valid, err := program.EncodeCacheTable(sampleCacheTable(), testIdentity)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	otherBuild := testIdentity
	otherBuild.SelfCommit = "another"
	otherCompiler := testIdentity
	otherCompiler.CompilerCommit = "another"
	otherToolchain := testIdentity
	otherToolchain.GoToolchain = "go-other"
	otherPlatform := testIdentity
	otherPlatform.Platform = "other/other"

	cases := []struct {
		name     string
		buffer   []byte
		identity program.CacheTableIdentity
	}{
		{"empty", nil, testIdentity},
		{"truncated in the header", valid[:10], testIdentity},
		{"truncated in the body", valid[:len(valid)/2], testIdentity},
		{"one byte short", valid[:len(valid)-1], testIdentity},
		{"trailing bytes", append(append([]byte{}, valid...), 0, 0, 0, 0), testIdentity},
		{"not a table at all", []byte("{\"version\":5}"), testIdentity},
		{"written by another cohere commit", valid, otherBuild},
		{"written against another compiler commit", valid, otherCompiler},
		{"written by another Go toolchain", valid, otherToolchain},
		{"written for another platform", valid, otherPlatform},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			decoded, err := program.DecodeCacheTable(testCase.buffer, testCase.identity)
			if err == nil {
				t.Fatalf("decoded without error into %d runs; the decoder cannot detect this and every "+
					"clean read from it is vacuous", len(decoded.Runs))
			}
			if !errors.Is(err, program.ErrCacheTableUnreadable) {
				t.Errorf("error should be ErrCacheTableUnreadable so a caller runs cold rather than failing: %v", err)
			}
		})
	}
}

// TestReadCacheTableAlwaysReturnsATable pins the caller's contract: a table to use, and an error that says
// whether its emptiness is worth reporting. A missing file is a first run; anything else is a discard.
func TestReadCacheTableAlwaysReturnsATable(t *testing.T) {
	directory := t.TempDir()

	t.Run("missing", func(t *testing.T) {
		table, err := program.ReadCacheTable(filepath.Join(directory, "absent.gob"), testIdentity)
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("a missing table should say it is missing, got %v", err)
		}
		if errors.Is(err, program.ErrCacheTableUnreadable) {
			t.Error("a missing table was reported as a discard, so every first run would print a warning")
		}
		if table == nil || table.Runs == nil || table.Findings != nil {
			t.Fatalf("a missing table should come back empty and usable, got %+v", table)
		}
		table.Runs["bare"] = nil
	})

	t.Run("corrupt", func(t *testing.T) {
		path := filepath.Join(directory, "corrupt.gob")
		if err := os.WriteFile(path, []byte("{not a table"), 0o600); err != nil {
			t.Fatal(err)
		}
		table, err := program.ReadCacheTable(path, testIdentity)
		if !errors.Is(err, program.ErrCacheTableUnreadable) {
			t.Errorf("a corrupt table should be a discard, got %v", err)
		}
		if table == nil || len(table.Runs) != 0 || table.Findings != nil {
			t.Fatalf("a corrupt table should come back empty, got %+v", table)
		}
	})
}

// TestCacheTableSurvivesDisk round-trips through the real filesystem, because the write path is where a
// cache is truncated or half-visible, and asserts the directory holds only the finished table: a leftover
// temporary means a failure path forgot to clean up, and it accumulates silently across runs.
func TestCacheTableSurvivesDisk(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "nested")
	path := filepath.Join(directory, "table.gob")
	original := sampleCacheTable()

	if err := program.WriteCacheTable(path, original, testIdentity); err != nil {
		t.Fatalf("writing: %v", err)
	}
	read, err := program.ReadCacheTable(path, testIdentity)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}

	// The interpolated description is the one thing in a finding nothing can rebuild, so it is the one
	// worth asserting survived a real write.
	entry, hit := read.Findings.Lookup(original.Findings.Entries[0].Path, original.Findings.Entries[0].ContentHash,
		original.Findings.Key, original.Findings.Entries[0].Rules)
	if !hit {
		t.Fatal("an entry written to disk came back as a miss")
	}
	if entry.Findings[1].MessageDescription != original.Findings.Entries[0].Findings[1].MessageDescription {
		t.Errorf("description did not survive disk: %q", entry.Findings[1].MessageDescription)
	}
	if read.Runs["--no-fix"] == nil {
		t.Error("the run did not survive disk")
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "table.gob" {
		names := []string{}
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("the directory holds %v, want exactly [table.gob]", names)
	}
}
