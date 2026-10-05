package benchresults

import (
	"path/filepath"
	"strings"
	"testing"
)

// Every committed record is the honest account of its own runs, and is named for itself.
func TestEveryCommittedRecordValidates(t *testing.T) {
	t.Parallel()
	if _, err := Read(filepath.Join("..", "..", Directory)); err != nil {
		t.Fatal(err)
	}
}

// honestRecord is a record that validates: two rounds at a ceiling of 4, with Cold quiet once, Warm quiet
// twice and Edit never.
func honestRecord() Record {
	commit := strings.Repeat("a", 40)
	runs := []Run{
		{Mode: ModeWarm, Round: 1, VerdictSeconds: 0.11, SettledSeconds: 0.2, EngineSeconds: 0.08, LoadBefore: 2, LoadAfter: 3, Cache: "replay"},
		{Mode: ModeEdit, Round: 1, VerdictSeconds: 0.7, SettledSeconds: 0.9, EngineSeconds: 0.65, LoadBefore: 3, LoadAfter: 5, Cache: "files 3891/3976"},
		{Mode: ModeCold, Round: 1, VerdictSeconds: 1.8, SettledSeconds: 2.1, EngineSeconds: 1.75, LoadBefore: 3.5, LoadAfter: 4, Cache: "off"},
		{Mode: ModeWarm, Round: 2, VerdictSeconds: 0.1, SettledSeconds: 0.2, EngineSeconds: 0.07, LoadBefore: 1, LoadAfter: 1.5, Cache: "files 3976/3976"},
		{Mode: ModeEdit, Round: 2, VerdictSeconds: 0.68, SettledSeconds: 0.8, EngineSeconds: 0.62, LoadBefore: 5, LoadAfter: 4, Cache: "files 3891/3976"},
		{Mode: ModeCold, Round: 2, VerdictSeconds: 1.9, SettledSeconds: 2.2, EngineSeconds: 1.85, LoadBefore: 4, LoadAfter: 4.5, Cache: "off"},
	}
	for index := range runs {
		// Round 2 needed a second prime, as #547dhjz makes a round do, and the record says so.
		runs[index].Primes = runs[index].Round
		runs[index].Quiet = runs[index].LoadBefore <= 4 && runs[index].LoadAfter <= 4
	}
	return Record{
		Schema:      SchemaVersion,
		RecordedAt:  "2026-10-05T10:00:00Z",
		Cohere:      Cohere{Commit: commit, Version: "cohere dev"},
		Project:     Project{Name: "ahra", Commit: strings.Repeat("b", 40)},
		Machine:     Machine{Model: "Mac16,5", Cores: 16},
		LoadCeiling: 4,
		Rounds:      2,
		Edit:        "modules/tasks/TasksSearchQuery.ts",
		Runs:        runs,
		Modes:       Summarize(runs),
	}
}

func TestAnHonestRecordValidates(t *testing.T) {
	t.Parallel()
	record := honestRecord()
	if err := Validate(record); err != nil {
		t.Fatal(err)
	}
	if record.Modes[ModeEdit].Quiet != nil {
		t.Fatalf("Edit had no quiet run and carries a quiet number: %+v", record.Modes[ModeEdit].Quiet)
	}
	if quiet := record.Modes[ModeCold].Quiet; quiet == nil || quiet.Count != 1 || quiet.Best != 1.8 {
		t.Fatalf("Cold's one quiet run gave %+v, want a count of 1 at 1.8s", quiet)
	}
	if got := FileName(record); got != "2026-10-05-aaaaaaaaaaaa-mac16-5.json" {
		t.Errorf("named %s", got)
	}
}

// Each way a record could claim more than its runs measured is refused.
func TestADishonestRecordIsRefused(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*Record){
		"a quiet number on a mode with no quiet run": func(record *Record) {
			summary := record.Modes[ModeEdit]
			summary.Quiet = &QuietSummary{Count: 1, Best: 0.68, Median: 0.68, Worst: 0.68}
			record.Modes[ModeEdit] = summary
		},
		"a run marked quiet over the ceiling": func(record *Record) {
			record.Runs[1].Quiet = true
			record.Modes = Summarize(record.Runs)
		},
		"a quiet run marked loaded": func(record *Record) {
			record.Runs[0].Quiet = false
			record.Modes = Summarize(record.Runs)
		},
		"a median the runs do not give": func(record *Record) {
			record.Modes[ModeWarm].Quiet.Median = 0.09
		},
		"a loaded run left out of the loaded list": func(record *Record) {
			summary := record.Modes[ModeEdit]
			summary.LoadedVerdictSeconds = summary.LoadedVerdictSeconds[:1]
			record.Modes[ModeEdit] = summary
		},
		// The first real record (91cf8e39) read 'none' on every replay, since the instrument grepped a footer
		// that had moved behind --verbose (#zrgrk14).
		"a replay whose cache read none":            func(record *Record) { record.Runs[0].Cache = "none" },
		"an edit run that replayed nothing":         func(record *Record) { record.Runs[1].Cache = "none" },
		"an edit run that replayed everything":      func(record *Record) { record.Runs[1].Cache = "files 3976/3976" },
		"a cold run that read the cache":            func(record *Record) { record.Runs[2].Cache = "files 3891/3976" },
		"a round whose runs disagree on its primes": func(record *Record) { record.Runs[1].Primes = 2 },
		"a run with no primes recorded":             func(record *Record) { record.Runs[0].Primes = 0 },
		"a run whose output was not read":           func(record *Record) { record.Runs[0].EngineSeconds = 0 },
		"an engine from a modified tree":            func(record *Record) { record.Cohere.Dirty = true },
		"an abbreviated commit":                     func(record *Record) { record.Cohere.Commit = record.Cohere.Commit[:12] },
		"a missing round":                           func(record *Record) { record.Runs = record.Runs[:5]; record.Modes = Summarize(record.Runs) },
		"a ceiling of zero":                         func(record *Record) { record.LoadCeiling = 0 },
		"another schema":                            func(record *Record) { record.Schema = SchemaVersion + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			record := honestRecord()
			mutate(&record)
			if err := Validate(record); err == nil {
				t.Fatal("validated")
			}
			if _, err := Encode(record); err == nil {
				t.Fatal("encoded")
			}
		})
	}
}
