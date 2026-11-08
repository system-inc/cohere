package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/streams"
)

// The gate's fifth step runs cohere on the tree it lands (#06g3f7x). Main at 80ac82fd read 8 findings on a bare
// `cohere --no-fix` at cohere's own root, and both landings that brought them had gated green: the gate ran go
// vet, the rule CPU budget, the tests and the docs check, and never cohere on cohere. Now a landing is refused
// unless every engine's run reads 0 findings, 0 type errors and no gap, so main can't go red under our own rules.
//
// The run checks every build cohere's Go files name, `--other-builds`, beside the host's (#72n9mq0). A plain run names
// each file only another platform or tag builds as a gap, which is true of it, and the gate would refuse every
// landing over those; with the switch each such file is checked by a build that compiles it, and only a file no build
// compiles stays a gap the gate refuses.

// cohereSource is cohere's own command, relative to the repository. A repository without it has no cohere to
// run on itself, and the step says so.
const cohereSource = "./command/cohere"

// gateCohereVariable names a cohere to run in place of one built from the tree, so a test can stand in for it
// without building cohere's whole module. It also stands in for the source's presence.
const gateCohereVariable = "COHERE_DEV_GATE_COHERE"

// cohereRecord is one line of `cohere --json`: a finding, a summary, or a kind the step doesn't read.
type cohereRecord struct {
	Kind       string                     `json:"kind"`
	Path       string                     `json:"path"`
	Line       int                        `json:"line"`
	Column     int                        `json:"column"`
	Rule       string                     `json:"rule"`
	Message    string                     `json:"message"`
	Label      string                     `json:"label"`
	Verdict    string                     `json:"verdict"`
	Findings   int                        `json:"findings"`
	TypeErrors int                        `json:"typeErrors"`
	Error      string                     `json:"error"`
	Gaps       map[string]json.RawMessage `json:"gaps"`
}

// gateCohereArguments are the gate's run of cohere: write nothing, check every build, and print the records.
var gateCohereArguments = []string{"--no-fix", "--other-builds", "--json"}

// checkCohere builds cohere at the worktree's tree and runs `cohere --no-fix --other-builds --json` at its root, and
// refuses the landing unless every summary passes with 0 findings, 0 type errors and no gap. The boolean reports
// whether the check ran. build and run are the environments for the build and the run, each asked when it
// starts, so either widens once the pool goes idle.
func checkCohere(worktree string, build func() []string, run func() []string) (int, bool) {
	binary := os.Getenv(gateCohereVariable)
	if binary == "" {
		if _, statError := os.Stat(filepath.Join(worktree, cohereSource)); statError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: no %s here, so no cohere to run on the tree\n", cohereSource)
			return 0, false
		}
		directory, temporaryDirectoryError := os.MkdirTemp("", "cohere-dev-gate-")
		if temporaryDirectoryError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: making a place to build cohere: %v\n", temporaryDirectoryError)
			return 1, true
		}
		// Ignored: a temporary directory left behind costs a binary's worth of disk, and the gate's verdict
		// is already decided.
		defer os.RemoveAll(directory)
		binary = filepath.Join(directory, "cohere")
		building := exec.Command("go", "build", "-o", binary, cohereSource)
		building.Dir, building.Env = worktree, build()
		building.Stdout, building.Stderr = os.Stdout, os.Stderr
		streams.Show(streams.Output)
		streams.Show(streams.Errors)
		if runError := building.Run(); runError != nil {
			fmt.Fprintln(streams.Errors, "cohere-dev: cohere did not build at the tree being landed, so nothing landed")
			return exitCodeOf(runError), true
		}
	}
	started := time.Now()
	checking := exec.Command(binary, gateCohereArguments...)
	var output bytes.Buffer
	checking.Dir, checking.Env = worktree, run()
	checking.Stdout, checking.Stderr = &output, os.Stderr
	streams.Show(streams.Output)
	streams.Show(streams.Errors)
	// Its exit is read from the records, which say why, and a run that wrote none is refused below.
	runError := checking.Run()
	problems := cohereProblems(output.Bytes())
	if runError != nil && len(problems) == 0 {
		problems = append(problems, fmt.Sprintf("cohere exited with %v and its records name no reason", runError))
	}
	if len(problems) > 0 {
		fmt.Fprintf(streams.Errors, "cohere-dev: cohere on the tree being landed did not read 0, so nothing landed:\n")
		for _, problem := range problems {
			fmt.Fprintf(streams.Errors, "  %s\n", problem)
		}
		return 1, true
	}
	fmt.Fprintf(streams.Errors, "cohere-dev: cohere on the tree reads 0, every engine, no gaps (%s)\n",
		time.Since(started).Round(time.Second))
	return 0, true
}

// cohereProblems is every reason a run's records refuse a landing: each finding, by its place and rule, and each
// summary that failed, counted type errors or findings, carried an error, or named a gap. A run with no summary
// at all is one, since every run ends on one. A gap is any field of a summary's gaps that is not zero, false or
// empty, so a gap added later is refused without this changing.
func cohereProblems(output []byte) []string {
	var problems []string
	summaries := 0
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var record cohereRecord
		if json.Unmarshal(scanner.Bytes(), &record) != nil {
			problems = append(problems, "a line cohere printed is not a JSON record: "+scanner.Text())
			continue
		}
		switch record.Kind {
		case "finding":
			problems = append(problems, fmt.Sprintf("%s:%d:%d %s %s", record.Path, record.Line, record.Column,
				record.Rule, record.Message))
		case "summary":
			summaries++
			label := record.Label
			if label == "" {
				label = "."
			}
			var why []string
			if record.Verdict != "pass" {
				why = append(why, "verdict "+record.Verdict)
			}
			if record.Findings > 0 {
				why = append(why, fmt.Sprintf("%d findings", record.Findings))
			}
			if record.TypeErrors > 0 {
				why = append(why, fmt.Sprintf("%d type errors", record.TypeErrors))
			}
			if record.Error != "" {
				why = append(why, record.Error)
			}
			if gaps := namedGaps(record.Gaps); len(gaps) > 0 {
				why = append(why, "gaps: "+strings.Join(gaps, ", "))
			}
			if len(why) > 0 {
				problems = append(problems, fmt.Sprintf("%s: %s", label, strings.Join(why, "; ")))
			}
		}
	}
	if summaries == 0 {
		problems = append(problems, "cohere printed no summary, so nothing says what it checked")
	}
	return problems
}

// namedGaps are the gaps a summary names, as `name=value`, sorted. A value is no gap when it is JSON's zero,
// false, empty string, empty list, empty object or null, so a gap of any kind is named.
func namedGaps(gaps map[string]json.RawMessage) []string {
	named := []string{}
	for name, value := range gaps {
		switch strings.TrimSpace(string(value)) {
		case "0", "false", `""`, "[]", "{}", "null":
			continue
		}
		named = append(named, name+"="+strings.TrimSpace(string(value)))
	}
	sort.Strings(named)
	return named
}
