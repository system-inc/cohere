package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/system-inc/cohere/internal/lint/configuration"
)

// A dead ignore is a finding (#cxeq6jw): an entry a settings file on disk wrote that matched nothing this run is
// reported at that entry, and fails the run, as a disable directive that suppresses nothing does. Two kinds of
// entry never are. One in a set cohere carries is a shape for every project that extends it, and no project can
// delete it. One of kind generated names build output, which may not exist yet: api-phi-health's `**/dist/**`
// matches after Base's container build and nothing on a clean checkout. Both are still listed with their count.

// unusedIgnoreRule is the dead ignore's rule, named by the house scheme (policy/RuleNaming.json): a pattern that
// matches nothing reads as excluding something and excludes nothing.
const unusedIgnoreRule = "correctness-no-unused-ignore"

// ignoreIsUnused reports whether an entry is a finding: it matched nothing, a file on disk wrote it, which its
// project can delete, and it does not name generated output, which may not have been written yet. One of cohere's
// defaults never is, since no file wrote it (see defaultIgnores).
func ignoreIsUnused(account ignoreAccount) bool {
	return account.Matched == 0 && !account.Default && !configuration.IsSet(account.File) &&
		account.Kind != string(configuration.IgnoreKindGenerated)
}

// unusedIgnoreFinding is an entry as a dead ignore's finding, at the entry in the file that wrote it. occurrence
// is how many earlier entries of that file have the same pattern, so a pattern written twice points at each.
func unusedIgnoreFinding(account ignoreAccount, occurrence int) runFinding {
	line, column := entryPosition(account.File, account.Pattern, occurrence)
	return runFinding{
		Path:      account.File,
		Line:      line,
		Column:    column,
		Severity:  "error",
		Rule:      unusedIgnoreRule,
		MessageId: "unusedIgnore",
		Message:   fmt.Sprintf("Unused ignore pattern %q (no path here matched it).", account.Pattern),
	}
}

// entryPosition finds an entry in its settings file, 1-based: the occurrence-th time the pattern is written as a
// JSON string after the `ignorePatterns` key, or the file's first character when it cannot be found, so the
// finding still names the file.
func entryPosition(file string, pattern string, occurrence int) (int, int) {
	contents, readFileError := os.ReadFile(file)
	if readFileError != nil {
		return 1, 1
	}
	quoted, marshalError := json.Marshal(pattern)
	if marshalError != nil {
		return 1, 1
	}
	offset := bytes.Index(contents, []byte(`"ignorePatterns"`))
	if offset < 0 {
		return 1, 1
	}
	for passed := 0; ; passed++ {
		found := bytes.Index(contents[offset:], quoted)
		if found < 0 {
			return 1, 1
		}
		offset += found
		if passed == occurrence {
			break
		}
		offset += len(quoted)
	}
	line := 1 + bytes.Count(contents[:offset], []byte("\n"))
	column := offset - bytes.LastIndexByte(contents[:offset], '\n')
	return line, column
}

// unusedIgnoreCount is how many of the accounts are findings.
func unusedIgnoreCount(accounts []ignoreAccount) int {
	count := 0
	for _, account := range accounts {
		if ignoreIsUnused(account) {
			count++
		}
	}
	return count
}

// writeUnusedIgnores prints each dead ignore as a finding, in every view.
func writeUnusedIgnores(out *bufio.Writer, accounts []ignoreAccount) {
	occurrences := map[string]int{}
	for _, account := range accounts {
		key := account.File + "\x00" + account.Pattern
		occurrence := occurrences[key]
		occurrences[key]++
		if !ignoreIsUnused(account) {
			continue
		}
		finding := unusedIgnoreFinding(account, occurrence)
		printFinding(out, finding, fmt.Sprintf("%s:%d:%d - %s [%s/%s]\n",
			finding.Path, finding.Line, finding.Column, finding.Message, finding.Rule, finding.MessageId))
	}
}

// ignoreVerdict is the exit a run answers with: its own, or 1 for a clean one when discovery found a dead ignore.
// The run cache records the tree's own code, since discovery runs again on every replay and says this again.
func ignoreVerdict(exitCode int) int {
	if exitCode == 0 && unusedIgnoreCount(discoveredIgnores) > 0 {
		return 1
	}
	return exitCode
}
