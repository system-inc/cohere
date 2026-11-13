package main

import (
	"slices"
	"testing"
)

// A pair whose every run passes is reported, its stderr shown in the table rather than refused.
func TestRefusalsPassAPairThatHoldsEveryCheck(test *testing.T) {
	test.Parallel()
	if failures := refusals(fixtureRecords()); len(failures) > 0 {
		test.Errorf("a passing pair refused: %q", failures)
	}
}

// Each check refuses the run that breaks it, by name, and only that run.
func TestRefusalsNameEachRunThatBreaksACheck(test *testing.T) {
	test.Parallel()
	zero := 0
	for _, refusal := range []struct {
		name   string
		breaks func(*runRecord)
		want   []string
	}{
		{"exit 2", func(record *runRecord) { record.ExitCode = 2 }, []string{"root-base-1-cache exited 2"}},
		{"exit 0 passes", func(record *runRecord) { record.ExitCode = 0 }, []string{}},
		{"stopped", func(record *runRecord) {
			record.Stopped, record.ExitCode = true, -1
		}, []string{"root-base-1-cache was stopped after 15m0s"}},
		{"never started", func(record *runRecord) {
			record.StartError, record.FilesInScope, record.FilesWalked = "no such file", nil, nil
		}, []string{"root-base-1-cache did not start: no such file"}},
		{"output unread", func(record *runRecord) { record.OutputError = "short read" },
			[]string{"root-base-1-cache's output could not be read: short read"}},
		{"no files in scope", func(record *runRecord) { record.FilesInScope = &zero },
			[]string{"root-base-1-cache has filesInScope 0"}},
		{"no project record", func(record *runRecord) { record.FilesInScope = nil },
			[]string{"root-base-1-cache has filesInScope missing"}},
		{"no files walked", func(record *runRecord) { record.FilesWalked = &zero },
			[]string{"root-base-1-cache has filesWalked 0"}},
		{"no standard library from the binary", func(record *runRecord) { record.StandardLibraryFromBinary = &zero },
			[]string{"root-base-1-cache has standardLibraryFromBinary 0"}},
		{"no lint record", func(record *runRecord) { record.StandardLibraryFromBinary = nil },
			[]string{"root-base-1-cache has standardLibraryFromBinary missing"}},
		{"changed tracked files", func(record *runRecord) { record.ChangedFiles = []string{"go.work.sum", "go.sum"} },
			[]string{"root-base-1-cache changed tracked files: go.work.sum, go.sum"}},
		{"another findings md5", func(record *runRecord) { record.FindingsMd5 = "ffff" }, []string{
			"root's runs do not share one findings md5: ffff in root-base-1-cache; " +
				"0df9cf540000000000000000000000ff in root-base-1-no-cache, root-branch-1-cache, " +
				"root-branch-1-no-cache, root-branch-2-cache, root-branch-2-no-cache, root-base-2-cache, " +
				"root-base-2-no-cache",
		}},
	} {
		test.Run(refusal.name, func(test *testing.T) {
			test.Parallel()
			records := fixtureRecords()
			refusal.breaks(&records[0])
			if failures := refusals(records); !slices.Equal(failures, refusal.want) {
				test.Errorf("refusals are\n%q\nwant\n%q", failures, refusal.want)
			}
		})
	}
}
