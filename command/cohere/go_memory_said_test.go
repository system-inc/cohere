package main

import (
	"strings"
	"testing"
)

// goStreamExtended is the Go contract's clean stream with addition, a run of fields, appended to the first record
// whose line starts with prefix, inside its closing brace.
func goStreamExtended(test *testing.T, lines []string, prefix string, addition string) []string {
	test.Helper()
	extended := make([]string, 0, len(lines))
	found := false
	for _, line := range lines {
		if !found && strings.HasPrefix(line, prefix) {
			found = true
			line = strings.TrimSuffix(line, "}") + addition + "}"
		}
		extended = append(extended, line)
	}
	if !found {
		test.Fatalf("no record starts %s in the stream", prefix)
	}
	return extended
}

// A Go run says its child's memory limit once in the account `--verbose` prints, with how many builds ran at once
// under it, which only the lint record carries, so the line waits for it; a run of one build says the limit alone, and
// a run that asked for no lint says it by its summary (#jrr6a5x).
//
// Not parallel: it renders through contractRun, whose record acceptors set the package-level activeOutput and
// activeSummary.
func TestAGoRunSaysItsMemoryWithTheBuildsAtOnce(test *testing.T) {
	savedOutput, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = savedOutput, savedSummary }()
	const memory = `,"memory":{"limitBytes":5750000000,"source":"Available","availableBytes":11500000000}`
	const builds = `,"builds":[{"target":"darwin/arm64 cgo","host":true},{"target":"linux/arm64"},` +
		`{"target":"windows/arm64"},{"target":"freebsd/arm64"}],"buildsAtOnce":3`
	withMemory := goStreamExtended(test, contractFixture(test, "GoClean.jsonl"), `{"kind":"types"`, memory)

	wide, _ := renderGoStreamAs(test, outputVerbose, goStreamExtended(test, withMemory, `{"kind":"lint"`, builds))
	alone, _ := renderGoStreamAs(test, outputVerbose, withMemory)
	lintless := make([]string, 0, len(withMemory))
	for _, line := range withMemory {
		switch {
		case strings.HasPrefix(line, `{"kind":"lint"`):
			continue
		case strings.HasPrefix(line, `{"kind":"phase","name":"lint"`):
			line = `{"kind":"phase","name":"lint","outcome":"skipped","elapsedMilliseconds":0,"detail":"not requested"}`
		}
		lintless = append(lintless, line)
	}
	unlinted, _ := renderGoStreamAs(test, outputVerbose, lintless)

	const said = "  memory limit 5.4 GiB, half of the 11.5 GB available"
	if strings.Count(wide, said) != 1 || !strings.Contains(wide, said+": 3 builds at once\n") {
		test.Errorf("a run of 3 builds at once does not say its limit once with them:\n%s", wide)
	}
	if strings.Count(alone, said) != 1 || !strings.Contains(alone, said+"\n") {
		test.Errorf("a run of one build does not say its limit once, alone:\n%s", alone)
	}
	if strings.Count(unlinted, said) != 1 || !strings.Contains(unlinted, said+"\n") {
		test.Errorf("a run that asked for no lint does not say its limit once, alone:\n%s", unlinted)
	}
}
