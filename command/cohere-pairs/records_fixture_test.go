package main

import (
	"encoding/json"
	"fmt"
	"slices"
)

// fixtureRecords is a pair that passes every check: root and loom, two rounds, in the schedule's order. Each run's
// wall seconds are its corpus's base plus a step for its side, mode and round, so every median and delta in the
// golden table can be worked by hand: the branch is 0.5 s faster than base on cache-on runs and 1 s on `--no-cache`
// runs, with round 2 0.2 s slower than round 1 on both sides. Root's base has no extra builds, as a base from
// before them would, one of loom's runs wrote to stderr, and no run changed a tracked file, which would refuse the
// pair; withChangedFiles adds some for the table.
func fixtureRecords() []runRecord {
	records := []runRecord{}
	for _, planned := range schedule([]string{corpusRoot, corpusLoom}, 2) {
		record := runRecord{
			Corpus:       planned.corpus,
			CorpusCommit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Side:         planned.side,
			SideCommit:   "1111111111111111111111111111111111111111",
			BinarySha256: "0a4601b8ffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			Round:        planned.round,
			Mode:         planned.mode,
			ExitCode:     1,
		}
		wall, findings, md5 := 10.0, 120, "0df9cf540000000000000000000000ff"
		if planned.corpus == corpusLoom {
			record.CorpusCommit = "9510c6baddda7b04f1ac4fa5f927d627f9a01cb3"
			wall, findings, md5 = 16.0, 94167, "51daa0200000000000000000000000ff"
		}
		if planned.side == sideBranch {
			record.SideCommit = "2222222222222222222222222222222222222222"
			record.BinarySha256 = "bfa25050ffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
			wall -= 0.5
			if planned.mode == modeNoCache {
				wall -= 0.5
			}
		}
		if planned.mode == modeNoCache {
			wall += 2
		}
		wall += 0.2 * float64(planned.round-1)
		record.WallSeconds = wall
		record.UserSeconds = wall * 6
		record.SystemSeconds = wall / 4
		record.PeakResidentBytes = 8_130_000_000 + int64(planned.round)*10_000_000
		load := 0.4 + float64(len(records))
		record.LoadBefore = &load
		filesInScope, filesWalked, fromBinary := 1200, 1180, 180
		record.engineOutput = engineOutput{
			Findings:                  findings,
			FindingsMd5:               md5,
			FilesInScope:              &filesInScope,
			FilesWalked:               &filesWalked,
			StandardLibraryFromBinary: &fromBinary,
			BuildsAtOnce:              2,
		}
		if planned.side == sideBranch {
			record.BuildsAtOnce = 3
		}
		if planned.corpus == corpusLoom || planned.side == sideBranch {
			record.Builds = fixtureBuilds(planned)
		}
		if planned.corpus == corpusLoom && planned.side == sideBase && planned.round == 2 &&
			planned.mode == modeCache {
			record.StandardErrorBytes = 201
		}
		record.ChangedFiles = []string{}
		records = append(records, record)
	}
	return records
}

// fixtureBuilds is a lint record's builds for a run: the host's, and two extra builds whose counts differ by side
// and round, so their medians are each side's own.
func fixtureBuilds(planned plannedRun) json.RawMessage {
	restored := 400 + planned.round
	if planned.side == sideBranch {
		restored += 10
	}
	builds := []map[string]any{
		{"target": "linux/amd64 cgo", "host": true, "compiledFiles": 36},
		{"target": "linux/amd64 cgo tags race", "host": false, "restoredFromHost": restored, "declinedAtLoad": 4,
			"checkedFromSource": 33},
		{"target": "darwin/amd64", "host": false, "restoredFromHost": restored - 130, "declinedAtLoad": 90,
			"checkedFromSource": 166 + planned.round},
	}
	encoded, encodeError := json.Marshal(builds)
	if encodeError != nil {
		panic(fmt.Sprintf("encoding the fixture's builds: %v", encodeError))
	}
	return encoded
}

// withChangedFiles is records with tracked files changed as runs have changed them, for the table that names each:
// root's `--no-cache` runs each changed go.work.sum, as go list tidied it before #34r3v1y, and one of loom's branch
// runs changed two files.
func withChangedFiles(records []runRecord) []runRecord {
	changed := slices.Clone(records)
	for index, record := range changed {
		if record.Corpus == corpusRoot && record.Mode == modeNoCache {
			changed[index].ChangedFiles = []string{"go.work.sum"}
		}
		if record.Corpus == corpusLoom && record.Side == sideBranch && record.Round == 2 && record.Mode == modeCache {
			changed[index].ChangedFiles = []string{"go.sum", "tools/go.sum"}
		}
	}
	return changed
}
