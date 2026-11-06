// Package differential measures a candidate formatter against the Prettier fork, byte for byte, over
// real corpora.
//
// It exists because cohere is replacing Prettier with a native printer, and "byte-identical on our
// code" is a claim no amount of reading either implementation can establish. The acceptance test is
// this package's number reaching 100% on every corpus we own. Until then the number is the progress
// report, so the instrument has to be right before anything is measured with it.
//
// # The oracle is goja(file), not file
//
// The predecessor of this harness (internal/format/comparison) treated every tree file as a fixed point
// of Prettier, so any change a candidate proposed was a divergence. That no longer holds. The projects
// run Prettier with prettier-plugin-tailwindcss, and the embedded engine runs core Prettier without
// it, so the tree is a fixed point of a different formatter than the one being matched. Class order is
// cohere's lint fixer's job, not the format phase's. So the oracle output is computed, never assumed.
//
// # Two numbers, because one of them flatters
//
// The corpora are almost entirely already formatted. A candidate that returns its input unchanged
// matches every file the oracle leaves alone and would report a high overall score while having no
// line-breaking engine at all. So every tally carries the subset the oracle actually rewrites, and the
// identity rate on that subset is reported separately. That subset is where formatting ability shows;
// the rest only proves a candidate does not break formatted code.
//
// # What never counts as a match
//
// A candidate that refuses a file, or errors on it, has not produced the oracle's bytes. Refusal is a
// separate outcome so it is visible, and it sits in the denominator. A file the oracle itself cannot
// format (a genuine parse error in the tree) has no right answer to match, so it leaves the denominator
// but is counted and named, never dropped silently.
package differential

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/system-inc/cohere/internal/types/sourcename"
)

// Formatter is either side of the comparison.
//
// It is the shape prettier.Engine already has, so the embedded Prettier is an oracle with no adapter,
// and a native printer only has to say which files it handles and format them.
type Formatter interface {
	Handles(fileName string) bool
	Format(fileName string, text string) (string, error)
}

// NewFormatter builds one Formatter for one worker.
//
// A factory rather than a value because the embedded engine is a goja runtime and goja runtimes are not
// safe for concurrent use. Each worker owns its own instance, so parallelism never shares one.
type NewFormatter func() (Formatter, error)

// Outcome is what happened to one file.
type Outcome string

const (
	// Identical means the candidate produced the oracle's bytes exactly.
	Identical Outcome = "Identical"

	// Different means the candidate produced bytes, and they were not the oracle's.
	Different Outcome = "Different"

	// CandidateRefused means the candidate did not handle the file or errored on it. Counts against.
	CandidateRefused Outcome = "CandidateRefused"

	// OracleFailed means Prettier itself could not format the file. Excluded from the denominator,
	// because there is no correct output to match, and named in the report.
	OracleFailed Outcome = "OracleFailed"
)

// FileResult is one file's comparison.
type FileResult struct {
	Path     string
	Language string
	Outcome  Outcome

	// OracleRewrites is whether the oracle's output differs from the file on disk. True files are the
	// subset that tests formatting rather than preservation.
	OracleRewrites bool

	// Detail is the first difference for Different, the error for CandidateRefused and OracleFailed.
	Detail string
}

// Language buckets a file by what parses it, which is what the port is organized by. An Adamic `.a` file is
// parsed as TypeScript, so it buckets with `.ts` (#6mhafvb).
func Language(fileName string) string {
	switch strings.ToLower(filepath.Ext(sourcename.TreatedAs(fileName))) {
	case ".ts", ".mts", ".cts":
		return "ts"
	case ".tsx":
		return "tsx"
	case ".js", ".mjs", ".cjs", ".jsx":
		return "js"
	case ".json", ".jsonc", ".json5":
		return "json"
	case ".md", ".markdown":
		return "md"
	case ".graphql", ".gql":
		return "graphql"
	case ".css", ".scss", ".less":
		return "css"
	case ".yaml", ".yml":
		return "yaml"
	default:
		return "other"
	}
}

// Compare formats every file with both sides and tallies the result.
//
// The oracle side goes through the cache when one is given, because the oracle is deterministic in its
// bundle digest, its options and the file's bytes, and it is by far the slower side. Re-reading the
// progress number should cost the candidate's time, not Prettier's.
func Compare(
	root string,
	files []string,
	newOracle NewFormatter,
	newCandidate NewFormatter,
	workers int,
	cache *OracleCache,
) (Report, error) {
	// At least one worker, so a zero or negative count still compares every file.
	workerCount := max(workers, 1)

	results := make([]FileResult, len(files))
	indexes := make(chan int)
	errs := make(chan error, workerCount)
	var group sync.WaitGroup

	for worker := 0; worker < workerCount; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			oracle, newOracleError := newOracle()
			if newOracleError != nil {
				errs <- fmt.Errorf("building the oracle: %w", newOracleError)
				return
			}
			candidate, newCandidateError := newCandidate()
			if newCandidateError != nil {
				errs <- fmt.Errorf("building the candidate: %w", newCandidateError)
				return
			}
			for index := range indexes {
				results[index] = compareOne(files[index], oracle, candidate, cache)
			}
		}()
	}

	for index := range files {
		select {
		case receivedError := <-errs:
			close(indexes)
			group.Wait()
			return Report{}, receivedError
		case indexes <- index:
		}
	}
	close(indexes)
	group.Wait()
	select {
	case receivedError := <-errs:
		return Report{}, receivedError
	default:
	}

	report := Report{Root: root, ByLanguage: map[string]*Tally{}, Files: results}
	for _, result := range results {
		tally := report.ByLanguage[result.Language]
		if tally == nil {
			tally = &Tally{}
			report.ByLanguage[result.Language] = tally
		}
		switch result.Outcome {
		case Identical:
			tally.Identical++
		case Different:
			tally.Different++
		case CandidateRefused:
			tally.CandidateRefused++
		case OracleFailed:
			tally.OracleFailed++
			continue
		}
		if result.OracleRewrites {
			tally.RewriteTotal++
			if result.Outcome == Identical {
				tally.RewriteIdentical++
			}
		}
	}
	return report, nil
}

// compareOne measures a single file.
func compareOne(path string, oracle Formatter, candidate Formatter, cache *OracleCache) FileResult {
	result := FileResult{Path: path, Language: Language(path)}

	source, readError := os.ReadFile(path)
	if readError != nil {
		result.Outcome = OracleFailed
		result.Detail = "reading the file: " + readError.Error()
		return result
	}
	text := string(source)

	expected, formatError := cache.format(oracle, path, text)
	if formatError != nil {
		result.Outcome = OracleFailed
		result.Detail = formatError.Error()
		return result
	}
	result.OracleRewrites = expected != text

	if !candidate.Handles(path) {
		result.Outcome = CandidateRefused
		result.Detail = "the candidate does not handle this file type"
		return result
	}
	actual, formatError := candidate.Format(path, text)
	if formatError != nil {
		result.Outcome = CandidateRefused
		result.Detail = formatError.Error()
		return result
	}

	if actual == expected {
		result.Outcome = Identical
		return result
	}
	result.Outcome = Different
	result.Detail = FirstDifference(expected, actual)
	return result
}

// FirstDifference names the first line where two outputs diverge.
//
// A line, not a byte offset, because the reader is going to open the file. Both lines are quoted so a
// whitespace-only difference is visible rather than looking like two identical strings.
func FirstDifference(expected string, actual string) string {
	expectedLines := strings.Split(expected, "\n")
	actualLines := strings.Split(actual, "\n")
	for line := 0; line < len(expectedLines) || line < len(actualLines); line++ {
		var want string
		var got string
		if line < len(expectedLines) {
			want = expectedLines[line]
		}
		if line < len(actualLines) {
			got = actualLines[line]
		}
		if want != got || line >= len(expectedLines) || line >= len(actualLines) {
			return fmt.Sprintf("line %d: want %q, got %q", line+1, truncate(want), truncate(got))
		}
	}
	return "identical by line, different by bytes (a trailing newline or a carriage return)"
}

func truncate(line string) string {
	const limit = 120
	if len(line) <= limit {
		return line
	}
	return line[:limit] + "…"
}

func firstLine(text string) string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	if scanner.Scan() {
		return scanner.Text()
	}
	return text
}
