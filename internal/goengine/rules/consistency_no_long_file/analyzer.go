// Package consistency_no_long_file is the Go engine's rule that a file holds at most 2,000 lines.
//
// The size of a file is what a reader pays for: a file past the limit is one no reader can hold whole, and it has
// seams in it to split along. The limit is the house max-lines and cohere-swift's file-length, and lines are counted
// the way both count them and an editor shows them: every line, comments and blank lines included, with a final
// newline ending the last line rather than starting another. A comment is part of what a reader holds, and a count
// that skipped comments would reward moving explanation out of the file.
package consistency_no_long_file

import (
	"fmt"
	"strconv"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-long-file"

// NewAnalyzer builds the rule's analyzer with a flag set of its own. The rule reads its limit option from the
// analyzer running it, so a test can run a configured and the default analyzer side by side without one setting a
// flag the other reads.
func NewAnalyzer() *analysis.Analyzer {
	analyzer := &analysis.Analyzer{
		Name: "consistency_no_long_file",
		Doc: "Flags a file of more than 2,000 lines once, at the start of its 2,001st line. Every line counts, " +
			"comments and blank lines included, and a final newline ends the last line rather than adding one, " +
			"so a file of 2,000 lines ending in a newline is not over. The limit option sets a stricter limit.",
		Run: run,
	}
	// A repository may hold its files to fewer lines than the house does, so the limit is an option. Its value is
	// refused below one when it is set, so a bad setting fails the run's configuration rather than this rule.
	limit := lineLimit(defaultLimit)
	analyzer.Flags.Var(&limit, limitOption, "the most lines a file may hold, at least 1")
	return analyzer
}

// Analyzer runs the rule over one package.
var Analyzer = NewAnalyzer()

// Rule is this rule's entry in the Go engine's registry. It reads the files' line tables, never the types.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer}

var tooManyLinesMessage = policy.MessageOf(Name, "tooManyLines")

// limitOption is the option naming the most lines a file may hold.
const limitOption = "limit"

// defaultLimit is the house limit, Kirk's ruling of 2026-10-03 (#r3hfpe8), shared with cohere-swift's file-length.
const defaultLimit = 2000

// lineLimit is the limit option's value. It is a flag.Value of its own, rather than flag.Int, so a limit below one,
// which would flag every file or name a line no file has, is refused when it is set.
type lineLimit int

// String is the limit as the flag package prints it, in usage and as the default it compares against.
func (limit *lineLimit) String() string {
	return strconv.Itoa(int(*limit))
}

// Set takes the limit from the text the engine's configuration hands it, refusing what is not a whole number of
// lines, one or more.
func (limit *lineLimit) Set(text string) error {
	value, parseError := strconv.Atoi(text)
	if parseError != nil {
		return fmt.Errorf("reading %q as a whole number of lines: %w", text, parseError)
	}
	if value < 1 {
		return fmt.Errorf("refusing a limit of %d lines, since a file holds at least one", value)
	}
	*limit = lineLimit(value)
	return nil
}

// limitOf is the limit option of the analyzer running pass. NewAnalyzer is the only way the rule's analyzer is built,
// but an analyzer assembled some other way has no limit of its own, so it runs at the house limit.
func limitOf(pass *analysis.Pass) int {
	option := pass.Analyzer.Flags.Lookup(limitOption)
	if option == nil {
		return defaultLimit
	}
	limit, isLineLimit := option.Value.(*lineLimit)
	if !isLineLimit {
		return defaultLimit
	}
	return int(*limit)
}

func run(pass *analysis.Pass) (any, error) {
	limit := limitOf(pass)
	for _, file := range pass.Files {
		// The line table counts every newline the scanner read, in comments and raw strings too, and a newline at
		// the very end starts no line, which is the count an editor shows. It is the file as written, which a
		// //line directive does not change.
		tokenFile := pass.Fset.File(file.FileStart)
		lineCount := tokenFile.LineCount()
		if lineCount <= limit {
			continue
		}
		// The finding sits where the file goes past the limit, so a reader jumping to it lands on the first line
		// over, the natural place to look for a seam.
		pass.Report(analysis.Diagnostic{
			Pos:      tokenFile.LineStart(limit + 1),
			Category: tooManyLinesMessage.Id,
			Message: tooManyLinesMessage.Render(map[string]string{
				"lineCount": strconv.Itoa(lineCount),
				"limit":     strconv.Itoa(limit),
			}),
		})
	}
	return nil, nil
}
