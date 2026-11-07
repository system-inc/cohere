package go_engine

import (
	"bytes"
	"cmp"
	"fmt"
	"go/parser"
	"go/token"

	"github.com/system-inc/cohere/internal/go_engine/copied"
	"github.com/system-inc/cohere/internal/go_engine/format"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_file"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_line"
	"github.com/system-inc/cohere/internal/go_engine/suppression"
	"github.com/system-inc/cohere/internal/line_wrap"
)

// longLineRuleWord is how every directive naming consistency_no_long_line spells it, in full or by its leaf, so a
// file without it holds no such directive and is not indexed.
const longLineRuleWord = "consistency-no-long-line"

// wrapLongLines is the fix the long-line rule's findings get, which the fix phase's wrap after the load asks of each
// file through format.Options.Wrap: it wraps gofmt's output with line_wrap, acting only on the lines the rule reports,
// and says how many of them the wrap brought within the limit. It leaves alone every file the rule's findings never
// reach (a generated file, which the fix phase skips before asking, and a `cohere:copied` file, whose findings the
// driver withholds) and every file with a directive that withholds the rule, since a wrap moves the lines a directive
// covers. The wrap is CuredOnly: a reported line it would break and leave long stays as gofmt printed it, construct
// and all, since a break that cures nothing is churn. It is KeepComments: no comment is reflowed, and a construct
// whose break would move one is left whole, since comments are data that line-based readers parse (an Example's
// `// Output:` block, a directive, a project's own marker lines) and a long comment is a person's to rewrite. A wrap
// that brings no reported line within the limit is not kept.
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
	result, wrapError := line_wrap.Wrap(formatted, line_wrap.Options{ReportedOnly: true, CuredOnly: true,
		KeepComments: true})
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

// heldToLongFileLimit is wrapping, a wrap of formatted, gofmt's output of file, declined when it would take the file
// past the long-file rule's limit: when the rule, asked through its own judgment (consistency_no_long_file.Reports),
// does not report the file before the wrap and would after it. A fix whose output trips another rule is no fix, and
// where the file splits is a person's call. A file the rule already reports may still be wrapped: its finding is
// there either way, so the wrap creates none. The limit is judged as the rule's own run judges it, without the file's
// directives: a file under the limit that withholds the rule holds a directive with nothing to cover, which the
// lint phase reports as unused, and a wrap past the limit would give it something to cover and so hide that finding.
// So the decline holds there too, and its reason names only the line count and the limit, true either way.
func heldToLongFileLimit(file string, formatted []byte, wrapping format.Wrapping, limit int) format.Wrapping {
	if wrapping.Output == nil {
		return wrapping
	}
	before, beforeError := consistency_no_long_file.Reports(file, consistency_no_long_file.LineCount(formatted), limit)
	afterCount := consistency_no_long_file.LineCount(wrapping.Output)
	after, afterError := consistency_no_long_file.Reports(file, afterCount, limit)
	if judgeError := cmp.Or(beforeError, afterError); judgeError != nil {
		return format.Wrapping{Declined: fmt.Sprintf("the long-file rule could not judge the wrap: %v", judgeError)}
	}
	if before || !after {
		return wrapping
	}
	return format.Wrapping{Declined: fmt.Sprintf("the wrap would take it to %d lines, past %s's limit of %d lines",
		afterCount, consistency_no_long_file.Name, limit)}
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
