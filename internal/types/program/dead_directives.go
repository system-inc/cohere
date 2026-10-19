package program

import (
	"strings"
	"unicode/utf8"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/ecmascript/text"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/suppression"
)

// reportDeadDirectives runs after suppression has seen every finding. Only a rule offered this file
// can prove its name dead; an unselected name stays in the comment, including on a filtered run.
// Report directly, so a blanket disable cannot silence a finding about itself. These findings are
// rebuilt even on a findings-cache hit, after its withheld findings have been marked by rule.
func reportDeadDirectives(sourceFile *ast.SourceFile, index *suppression.Index, ran []rule.Rule,
	measureOnly map[string]bool, crashes []RuleCrash, report func(rule.Diagnostic)) {
	if len(index.Directives()) == 0 {
		return
	}
	eligible := make([]string, 0, len(ran))
	complete := len(crashes) == 0
	for _, subject := range ran {
		if measureOnly[subject.Name] {
			complete = false
			continue
		}
		crashed := false
		for _, crash := range crashes {
			if crash.RuleName == subject.Name {
				crashed = true
				break
			}
		}
		if !crashed {
			eligible = append(eligible, subject.Name)
		}
	}
	ReportDeadDirectives(sourceFile, index, eligible, complete, report)
}

// ReportDeadDirectives reports and fixes unused names after a caller has run the given rules and
// filtered their findings through index. ran contains only rules that completed on these bytes;
// skipped type-aware rules, unselected rules and crashed rules cannot prove a directive dead.
// complete says every selected rule completed: without it a blanket may still be live for a
// skipped rule. The fix relint path uses this too, so dead names settle in one invocation.
func ReportDeadDirectives(sourceFile *ast.SourceFile, index *suppression.Index, ran []string, complete bool, report func(rule.Diagnostic)) {
	if len(ran) == 0 {
		return
	}
	for directiveIndex, directive := range index.Directives() {
		kept := make([]string, 0, len(directive.Rules))
		dead := make([]string, 0, len(directive.Rules))
		for _, name := range directive.Rules {
			named := suppression.Directive{Rules: []string{name}}
			selected := false
			for _, subject := range ran {
				if named.Names(subject) {
					selected = true
					break
				}
			}
			if selected && !directive.AppliedTo(name) {
				dead = append(dead, name)
			} else {
				kept = append(kept, name)
			}
		}
		if len(directive.Rules) == 0 {
			if !complete || index.AppliedCount(directiveIndex) != 0 {
				continue
			}
		} else if len(dead) == 0 {
			continue
		}

		description := "Unused disable directive (no findings were suppressed)."
		if len(dead) > 0 {
			description = "Unused disable directive for " + strings.Join(dead, ", ") + " (no findings from these rules were suppressed)."
		}
		comment := sourceFile.Text()[directive.Pos:directive.End]
		report(rule.Diagnostic{
			RuleName: "unused-disable", SourceFile: sourceFile,
			Range:   core.NewTextRange(directive.Pos, directive.End),
			Message: rule.Message{Id: "unusedDisable", Description: description},
			Fixes:   []rule.Fix{{Range: core.NewTextRange(directive.Pos, directive.End), Text: withoutDeadNames(comment, kept)}},
		})
	}
}

// withoutDeadNames preserves the directive spelling, remaining names, and reason. Removing the
// whole comment keeps line terminators (other next-line directives must still target the same line)
// and leaves a space for a one-line block comment that may separate two tokens.
func withoutDeadNames(comment string, kept []string) string {
	if len(kept) == 0 {
		if strings.HasPrefix(comment, "//") {
			return ""
		}
		breaks := commentLineBreaks(comment)
		if breaks == "" {
			return " "
		}
		return breaks
	}
	bodyEnd := len(comment)
	if strings.HasSuffix(comment, "*/") {
		bodyEnd -= 2
	}
	// Walk over the comment marker, leading whitespace and directive word. JavaScript whitespace
	// includes a BOM and no-break space, which strings.Fields and ASCII-only trimming disagree on.
	position := 2
	skipSpace := func() {
		for position < bodyEnd {
			character, size := utf8.DecodeRuneInString(comment[position:bodyEnd])
			if !text.IsWhitespace(character) {
				break
			}
			position += size
		}
	}
	skipSpace()
	for position < bodyEnd {
		character, size := utf8.DecodeRuneInString(comment[position:bodyEnd])
		if text.IsWhitespace(character) {
			break
		}
		position += size
	}
	skipSpace()
	start := position
	if reason := strings.Index(comment[start:bodyEnd], "--"); reason >= 0 {
		bodyEnd = start + reason
	}
	end := start + len(text.TrimWhitespace(comment[start:bodyEnd]))
	return comment[:start] + strings.Join(kept, ", ") + commentLineBreaks(comment[start:end]) + comment[end:]
}

func commentLineBreaks(comment string) string {
	return strings.Map(func(character rune) rune {
		switch character {
		case '\r', '\n', '\u2028', '\u2029':
			return character
		}
		return -1
	}, comment)
}
