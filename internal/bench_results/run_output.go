package bench_results

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// RunOutput is what one measured run said about itself, read from its `--json` summary lines, the contract
// cohere versions (command/cohere/json_output.go, schema/CohereOutput.schema.json). The human footer is
// free to change, and when it moved to one line every reader of it read nothing, so the benchmark reads
// the contract instead.
//
// A root holding several projects prints a summary for each (`.`, `. (Format)`, `swift`), and they run side
// by side, so the run is all of them: the longest engine time, every finding, and a cache use that holds for
// every project. Reading only the last one timed a Swift project on ahra and missed a Go engine that never
// ran on cohere's root (#7z52pqf).
type RunOutput struct {
	// EngineSeconds is the engine's own wall clock for the run: the longest project's.
	EngineSeconds float64
	// Findings is every finding the run reported, whichever phase found it: lint findings, type errors,
	// and files the formatter would change.
	Findings int
	// Cache is what the run took from the cache: "replay" for a run answered whole, "files N/M" for N of
	// the M files in scope taken from the cache, "off" under --no-cache, and "none" when it checked every
	// file. N is the summary's `cached`, the count the footer prints, which splits the files in scope with
	// `checked`; `cache.filesReplayed` is narrower, and read 0 on a run that took every file from the cache.
	Cache string
	// Builds are the builds the run's Go project checked, as its summary names them, the host's first: one for a
	// run of the host's build alone, every build the files name for one that checks them all (#ccqee0e). Empty when
	// no project is Go, or its summary names none.
	Builds []string
}

// outputSchemaVersion is the `--json` contract version this reader knows.
const outputSchemaVersion = 1

// ParseRunOutput reads a run's `--json` output: every line whose kind is summary, one a project. Output
// with no summary line, one of another contract version, or a project whose run did less than a
// developer's (`checkedLess`) is refused, so an instrument that cannot read a run, or a run that skipped
// its work, says so rather than recording a time.
func ParseRunOutput(output []byte) (RunOutput, error) {
	var summaries []runSummary
	for _, line := range bytes.Split(output, []byte("\n")) {
		if !bytes.HasPrefix(bytes.TrimSpace(line), []byte("{")) {
			continue
		}
		var kind struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(line, &kind) != nil || kind.Kind != "summary" {
			continue
		}
		var summary runSummary
		if decodeError := json.Unmarshal(line, &summary); decodeError != nil {
			return RunOutput{}, fmt.Errorf("reading the summary line: %w", decodeError)
		}
		summaries = append(summaries, summary)
	}
	if len(summaries) == 0 {
		return RunOutput{}, fmt.Errorf("no --json summary line in the run's output")
	}
	result := RunOutput{}
	everyOff, everyReplayed := true, true
	cached, inScope := 0, 0
	for _, summary := range summaries {
		if summary.SchemaVersion != outputSchemaVersion {
			return RunOutput{}, fmt.Errorf(
				"the summary of %q is --json schema %d, and this reader knows %d",
				summary.Label,
				summary.SchemaVersion,
				outputSchemaVersion,
			)
		}
		if reason := summary.checkedLess(); reason != "" {
			return RunOutput{}, fmt.Errorf("the project %q did less than a developer's run: %s", summary.Label, reason)
		}
		result.EngineSeconds = max(result.EngineSeconds, summary.Seconds)
		result.Findings += summary.Findings + summary.TypeErrors + summary.WouldChange
		everyOff = everyOff && summary.Cache.Off
		everyReplayed = everyReplayed && summary.Cache.Replayed
		cached += summary.Cached
		inScope += summary.FilesInScope
		for _, checked := range summary.Builds {
			result.Builds = append(result.Builds, checked.Target)
		}
	}
	switch {
	case everyOff:
		result.Cache = "off"
	case everyReplayed:
		result.Cache = "replay"
	case cached > 0:
		result.Cache = fmt.Sprintf("files %d/%d", cached, inScope)
	default:
		result.Cache = "none"
	}
	return result, nil
}
