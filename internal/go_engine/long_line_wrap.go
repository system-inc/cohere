package go_engine

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"

	"github.com/system-inc/cohere/internal/go_engine/copied"
	"github.com/system-inc/cohere/internal/go_engine/format"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_line"
	"github.com/system-inc/cohere/internal/go_engine/suppression"
	"github.com/system-inc/cohere/internal/line_wrap"
)

// longLineRuleWord is how every directive naming consistency_no_long_line spells it, in full or by its leaf, so a
// file without it holds no such directive and is not indexed.
const longLineRuleWord = "consistency-no-long-line"

// wrapLongLines is the fix the long-line rule's findings get, handed to the fix phase as format.Options.Wrap: it
// wraps gofmt's output with line_wrap, acting only on the lines the rule reports, and says how many of them the wrap
// brought within the limit. It leaves alone every file the rule's findings never reach (a generated file, which the
// fix phase skips before asking, and a `cohere:copied` file, whose findings the driver withholds) and every file with
// a directive that withholds the rule, since a wrap moves the lines a directive covers. The wrap is CuredOnly: a
// reported line it would break and leave long stays as gofmt printed it, construct and all, since a break that cures
// nothing is churn. A wrap that brings no reported line within the limit is not kept.
func wrapLongLines(formatted []byte) format.Wrapping {
	if !consistency_no_long_line.HasLongLine(formatted) {
		return format.Wrapping{}
	}
	header, headerError := parser.ParseFile(token.NewFileSet(), "", formatted,
		parser.PackageClauseOnly|parser.ParseComments)
	if headerError != nil {
		return format.Wrapping{Declined: fmt.Sprintf("its header does not parse: %v", headerError)}
	}
	// Ignored: a header that breaks the copy grammar is still left alone, so only whether one is declared matters. The
	// file claims to be upstream's, a wrap would bury its diff against upstream, and the copied test fails naming it.
	if _, declared, _ := copied.Read(header); declared {
		return format.Wrapping{}
	}
	if withholdsTheRule(formatted) {
		return format.Wrapping{}
	}
	result, wrapError := line_wrap.Wrap(formatted, line_wrap.Options{ReportedOnly: true, CuredOnly: true})
	if wrapError != nil {
		return format.Wrapping{Declined: fmt.Sprintf("the wrap failed: %v", wrapError)}
	}
	if result.Rejected != nil {
		return format.Wrapping{Declined: fmt.Sprintf("the wrap failed its equivalence proof: %v", result.Rejected)}
	}
	lines := reportedCount(result.Before) - reportedCount(result.After)
	if bytes.Equal(result.Output, formatted) || lines < 1 {
		return format.Wrapping{}
	}
	return format.Wrapping{Output: result.Output, Lines: lines}
}

// reportedCount is how many of the long lines a ReportedOnly wrap counted are lines the rule reports.
func reportedCount(count line_wrap.Count) int {
	total := 0
	for category, lines := range count {
		if category != line_wrap.CategoryNotReported {
			total += lines
		}
	}
	return total
}

// withholdsTheRule is whether a well-formed //cohere:ignore or //cohere:file-ignore in source withholds the long-line
// rule anywhere, read through suppression itself so the answer is the lint phase's: one finding on every line, and
// the line after the last that a directive closing the file covers, and whether any of them is withheld.
func withholdsTheRule(source []byte) bool {
	if !bytes.Contains(source, []byte(longLineRuleWord)) {
		return false
	}
	const file = "wrapped.go"
	lineCount := bytes.Count(source, []byte("\n")) + 1
	findings := make([]suppression.Finding, 0, lineCount+1)
	for line := 1; line <= lineCount+1; line++ {
		findings = append(findings, suppression.Finding{Rule: consistency_no_long_line.Name, File: file, Line: line})
	}
	ruleNames := []string{consistency_no_long_line.Name}
	applied := suppression.Apply(ruleNames, ruleNames, []*suppression.Index{suppression.Build(file, source)}, findings)
	return len(applied.Kept) < len(findings)
}
