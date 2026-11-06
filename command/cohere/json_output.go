package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/system-inc/cohere/internal/sentence"
	"github.com/system-inc/cohere/internal/streams"
)

// `--json` is the run for a program to read: newline-delimited JSON, one object per line, each with a
// `kind`. A finding is one line, each file the run fixed or formatted is one line, each crash is one
// line, and the summary is the last line, so a reader can stream the findings and still know the run
// ended by seeing it.
//
//	{"kind":"finding","path":"/repo/a.ts","line":3,"column":7,"severity":"error","rule":"nexus/...","messageId":"...","message":"..."}
//	{"kind":"fixed","path":"/repo/b.ts","rules":["prefer-const"]}
//	{"kind":"formatted","path":"/repo/c.ts"}
//	{"kind":"crash","path":"/repo/d.ts","rule":"react-hooks/purity","cause":"..."}
//	{"kind":"summary","schemaVersion":1,"verdict":"pass",...}
//
// It is the second view of runSummary, beside the footer, and the one a consumer such as Structure's
// parity check reads. A consumer that parsed the human text read a layout cohere is free to change; this
// is a contract, versioned by schemaVersion and described in schema/CohereOutput.schema.json.

// outputSchemaVersion is the version of the `--json` contract. A change a reader could notice, such as
// a field removed or renamed or a meaning changed, raises it; a new optional field does not.
const outputSchemaVersion = 1

// findingJson is one finding's line: the finding's own fields beside its kind.
type findingJson struct {
	Kind string `json:"kind"`
	runFinding
}

func findingAsJson(finding runFinding) findingJson {
	return findingJson{Kind: "finding", runFinding: finding}
}

// changedFileJson is one file the run rewrote, by fixing or by formatting.
type changedFileJson struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
	// Rules are the rules whose fixes were applied, for a fixed file, each once.
	Rules []string `json:"rules,omitempty"`
}

// changedFilesAsJson is a line per kind of rewrite: a file both fixed and formatted is a `fixed` line and
// a `formatted` line, so a reader counting either kind counts it.
func changedFilesAsJson(files []changedFile) []changedFileJson {
	lines := make([]changedFileJson, 0, len(files))
	for _, file := range files {
		if file.Fixed {
			rules := make([]string, 0, len(file.FixedBy))
			for rule := range file.FixedBy {
				rules = append(rules, rule)
			}
			sort.Strings(rules)
			lines = append(lines, changedFileJson{Kind: "fixed", Path: file.Path, Rules: rules})
		}
		if file.Formatted {
			lines = append(lines, changedFileJson{Kind: "formatted", Path: file.Path})
		}
	}
	return lines
}

// summaryJson is runSummary as the last line. Durations are seconds, as a reader would compare them.
type summaryJson struct {
	Kind          string      `json:"kind"`
	SchemaVersion int         `json:"schemaVersion"`
	Label         string      `json:"label,omitempty"`
	Verdict       string      `json:"verdict"`
	Seconds       float64     `json:"seconds"`
	GraphSeconds  float64     `json:"graphSeconds"`
	Rules         int         `json:"rules"`
	Phases        []phaseJson `json:"phases"`
	// FormattingSeconds is the part of the fix phase's seconds with a format in flight, absent when no
	// formatter ran.
	FormattingSeconds float64  `json:"formattingSeconds,omitempty"`
	Cache             cacheUse `json:"cache"`
	TypeErrors        int      `json:"typeErrors"`
	Findings          int      `json:"findings"`
	FilesFixed        int      `json:"filesFixed"`
	FilesFormatted    int      `json:"filesFormatted"`
	WouldChange       int      `json:"wouldChange"`
	// Cohered is the files rewritten; Checked and Cached split FilesInScope between fresh and cached.
	Cohered      int `json:"cohered"`
	FilesInScope int `json:"filesInScope"`
	Checked      int `json:"checked"`
	Cached       int `json:"cached"`
	Nodes        int `json:"nodes"`
	// Gaps is every way the run fell short of checking everything. A reader deciding whether a green run
	// can be trusted reads this, not the verdict alone.
	Gaps runGaps `json:"gaps"`
	// Adamic is the run's readiness, present only under --adamic-readiness (#9tgm3dq). A run that asked and
	// did not lint says not measured, and why, so a reader who asked never takes its absence for a pass.
	Adamic *readinessSummary `json:"adamic,omitempty"`
	// Error is why the run stopped before it could judge anything, in the words the human view prints, as
	// a tsconfig that does not load (#9zqayzh). Present only then, and the verdict is fail.
	Error string `json:"error,omitempty"`
}

// phaseJson is one phase's outcome.
type phaseJson struct {
	Name    string  `json:"name"`
	Outcome string  `json:"outcome"`
	Seconds float64 `json:"seconds"`
	Detail  string  `json:"detail,omitempty"`
}

// summaryAsJson is the summary's last line.
func summaryAsJson(summary runSummary) summaryJson {
	verdict := "pass"
	if summary.failed() {
		verdict = "fail"
	}
	adamic := summary.Adamic
	if adamic == nil && summary.AdamicRequested {
		adamic = notMeasured("lint did not run")
	}
	phases := make([]phaseJson, 0, len(summary.Phases))
	for _, record := range summary.Phases {
		phases = append(phases, phaseJson{
			Name:    string(record.Name),
			Outcome: string(record.Outcome),
			Seconds: record.Elapsed.Seconds(),
			Detail:  record.Detail,
		})
	}
	return summaryJson{
		Kind:              "summary",
		SchemaVersion:     outputSchemaVersion,
		Label:             summary.Label,
		Verdict:           verdict,
		Seconds:           summary.Total.Seconds(),
		GraphSeconds:      summary.Graph.Seconds(),
		Rules:             summary.Rules,
		Phases:            phases,
		FormattingSeconds: summary.Formatting.Seconds(),
		Cache:             summary.Cache,
		TypeErrors:        summary.TypeErrors,
		Findings:          summary.Findings,
		FilesFixed:        summary.filesFixed(),
		FilesFormatted:    summary.filesFormatted(),
		WouldChange:       summary.WouldChange,
		Cohered:           summary.cohered(),
		FilesInScope:      summary.FilesInScope,
		Checked:           summary.FilesChecked,
		Cached:            summary.FilesCached,
		Nodes:             summary.Nodes,
		Gaps:              summary.Gaps,
		Adamic:            adamic,
	}
}

// summaryWritten is whether this run printed its summary line under `--json`, so a run that then fails
// prints no second one. See writeFailedRunSummary.
var summaryWritten bool

// summaryJsonLine is the summary as the last line, newline included, and notes that the run printed it.
func summaryJsonLine(summary summaryJson) string {
	summaryWritten = true
	return jsonLine(summary)
}

// writeSummaryLine prints the summary as the last line and notes that it did.
func writeSummaryLine(out *bufio.Writer, summary summaryJson) {
	fmt.Fprint(out, summaryJsonLine(summary))
}

// writeFailedRunSummary ends a run that stopped on an error with a summary line of its own under `--json`:
// verdict fail, the error as the sentence the human view prints, and the project's label in a repository with
// several. Every run ends on a summary, so a reader that saw none would read the stream as cut off, and one that
// counted the lines it did see would read a project that never loaded as one with nothing wrong (#9zqayzh). A
// run that already printed its summary prints nothing more, and the human views print nothing here: the error
// line on stderr is theirs.
func writeFailedRunSummary(out *bufio.Writer, runError error) {
	if activeOutput.Mode != outputJson || summaryWritten {
		return
	}
	summary := activeSummary
	summary.Total = time.Since(processStart)
	if label := os.Getenv(projectLabelVariable); label != "" {
		summary.Label = label
	}
	line := summaryAsJson(summary)
	line.Verdict = "fail"
	line.Error = sentence.FromError(runError)
	writeSummaryLine(out, line)
}

// writeJsonLine writes one object as one line.
func writeJsonLine[Value any](out *bufio.Writer, value Value) {
	fmt.Fprint(out, jsonLine(value))
}

// jsonLine is one object as one line, newline included. A value that does not encode is a line the reader never
// gets, so it is reported to package streams, and the run exits nonzero saying so, rather than printing a
// stream with a record missing; the line is then empty.
func jsonLine[Value any](value Value) string {
	line, marshalError := json.Marshal(value)
	if marshalError != nil {
		streams.Report(fmt.Errorf("encoding a line of --json: %w", marshalError))
		return ""
	}
	return string(line) + "\n"
}
