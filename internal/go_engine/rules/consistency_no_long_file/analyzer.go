// Package consistency_no_long_file is the Go engine's rule that a file holds at most 2,000 lines.
//
// The size of a file is what a reader pays for: a file past the limit is one no reader can hold whole, and it has
// seams in it to split along. The limit is the house max-lines and cohere-swift's file-length, and lines are counted
// the way both count them and an editor shows them: every line, comments and blank lines included, with a final
// newline ending the last line rather than starting another. A comment is part of what a reader holds, and a count
// that skipped comments would reward moving explanation out of the file.
//
// The finding covers the lines past the limit, from the start of the first one over to the end of the file, which is
// what a split has to move out. A file whose base name has the `*.generated.*` shape, such as SecretKey.generated.ts
// or schema.generated.go, gets no finding whatever its length or header: the name is the house's mark of a file a
// generator writes, and its size is the generator's to keep, not a reader's to split. The engine's separate drop of a
// file go/ast.IsGenerated accepts still applies on top of this.
//
// The max option moves the limit either way, as every engine's long-file rule takes it (#phdc7aa). A stricter limit
// is a project's own business. A looser one departs from the ruling, so the engine runs it only where the settings'
// go block states a departure for this rule with a reason, and refuses the run where they don't.
package consistency_no_long_file

import (
	"fmt"
	"go/token"
	"path"
	"path/filepath"
	"strconv"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-long-file"

// maximumOption is the option naming the most lines a file may hold, `max`, as every engine's long-file rule
// spells it.
const maximumOption = "max"

// generatedNamePattern is the base name a generated file carries, matched with path.Match, so the word alone, as in
// generated_long.go, is not the mark.
const generatedNamePattern = "*.generated.*"

// defaultLimit is the house limit, Kirk's ruling of 2026-10-03 (#r3hfpe8), shared with cohere-swift's file-length.
const defaultLimit = 2000

// NewAnalyzer builds the rule's analyzer with a flag set of its own. The rule reads its max option from the
// analyzer running it, so a test can run a configured and the default analyzer side by side without one setting a
// flag the other reads.
func NewAnalyzer() *analysis.Analyzer {
	analyzer := &analysis.Analyzer{
		Name: "consistency_no_long_file",
		Doc: "Flags a file of more than 2,000 lines once, from the start of its 2,001st line to its end. Every " +
			"line counts, comments and blank lines included, and a final newline ends the last line rather than " +
			"adding one, so a file of 2,000 lines ending in a newline is not over. A file whose base name matches " +
			"*.generated.* is exempt. The max option sets a stricter limit or a looser one, and a looser one " +
			"runs only beside a departure in the settings that says why.",
		Run: run,
	}
	// A repository may hold its files to fewer lines than the house does, or to more, so the limit is the max option.
	// A looser limit departs from a ruling, so the engine runs it only where the settings state a departure with a
	// reason, which it learns from the value's Loosens. The value is refused below one when it is set, so a bad
	// setting fails the run's configuration rather than this rule.
	limit := lineLimit(defaultLimit)
	analyzer.Flags.Var(&limit, maximumOption, "the most lines a file may hold, 1 or more; above 2000 needs a "+
		"departure stating why")
	return analyzer
}

// Analyzer runs the rule over one package.
var Analyzer = NewAnalyzer()

// Rule is this rule's entry in the Go engine's registry. It reads the files' line tables, never the types.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer}

var tooManyLinesMessage = policy.MessageOf(Name, "tooManyLines")

// LimitOf is the max option of analyzer, the most lines the rule lets a file hold as the settings configure it, read
// the way the rule's run reads it, so the fix phase's long-line wrap and the rule never disagree on the limit.
// NewAnalyzer is the only way the rule's analyzer is built, but an analyzer assembled some other way has no limit of
// its own, so it runs at the house limit.
func LimitOf(analyzer *analysis.Analyzer) int {
	option := analyzer.Flags.Lookup(maximumOption)
	if option == nil {
		return defaultLimit
	}
	limit, isLineLimit := option.Value.(*lineLimit)
	if !isLineLimit {
		return defaultLimit
	}
	return int(*limit)
}

// Reports is whether the rule reports a file named name that holds lineCount lines under limit: one past the limit
// whose base name lacks the `*.generated.*` shape. The rule's run reports through it, and the fix phase's long-line
// wrap asks it of a file before and after the wrap, so a wrap never takes a file past the limit the rule reads.
func Reports(name string, lineCount int, limit int) (bool, error) {
	hasGeneratedName, matchError := path.Match(generatedNamePattern, filepath.Base(name))
	if matchError != nil {
		return false, fmt.Errorf("matching %s against %s: %w", name, generatedNamePattern, matchError)
	}
	return !hasGeneratedName && lineCount > limit, nil
}

// LineCount is how many lines the rule counts in content: the lines of the token.File a parse of it gives, every
// newline starting a line but one at the very end, which the scanner and SetLinesForContent read alike. The fix phase
// counts a file it has not parsed with it.
func LineCount(content []byte) int {
	tokenFile := token.NewFileSet().AddFile("", -1, len(content))
	tokenFile.SetLinesForContent(content)
	return tokenFile.LineCount()
}

func run(pass *analysis.Pass) (any, error) {
	limit := LimitOf(pass.Analyzer)
	for _, file := range pass.Files {
		// The line table counts every newline the scanner read, in comments and raw strings too, and a newline at
		// the very end starts no line, which is the count an editor shows. It is the file as written, which a
		// //line directive does not change.
		tokenFile := pass.Fset.File(file.FileStart)
		reported, reportsError := Reports(tokenFile.Name(), tokenFile.LineCount(), limit)
		if reportsError != nil {
			return nil, reportsError
		}
		if !reported {
			continue
		}
		// The finding starts where the file goes past the limit, so a reader jumping to it lands on the first line
		// over, the natural place to look for a seam, and runs to the end of the file, the last byte as written,
		// so its range is every line a split has to move out.
		pass.Report(analysis.Diagnostic{
			Pos:      tokenFile.LineStart(limit + 1),
			End:      file.FileEnd,
			Category: tooManyLinesMessage.Id,
			Message: tooManyLinesMessage.Render(map[string]string{
				"lineCount": strconv.Itoa(tokenFile.LineCount()),
				"max":       strconv.Itoa(limit),
			}),
		})
	}
	return nil, nil
}
