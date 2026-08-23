package tailwind

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
)

func messageUnnecessaryWhitespace() rule.Message {
	return rule.Message{
		Id: "unnecessaryWhitespace",
		Description: "This class string carries whitespace that does nothing: a leading or trailing " +
			"run, or more than one space between two classes. It is invisible in the rendered output " +
			"and it makes the string a worse thing to diff and search, because two literals that mean " +
			"the same list stop being the same text.",
	}
}

// NoUnnecessaryWhitespaceOptions lets a project name the surfaces that carry class strings.
type NoUnnecessaryWhitespaceOptions struct {
	Attributes []string
	Callees    []string
	Variables  []string
}

// NoUnnecessaryWhitespace reports padding and doubled spaces inside a class string.
//
//	valid:   <div className="flex items-center" />
//	valid:   <div className={`flex ${extra} gap-2`} />
//	invalid: <div className="flex  items-center" />
//	invalid: <div className=" flex" />
//	invalid: <div className="flex " />
//
// The defect is cosmetic in the browser and not cosmetic in the repository. Class strings are the
// one part of the codebase with no compiler, so the only way anyone finds them is by reading and
// searching text. Two literals meaning the same list should be the same bytes, otherwise a search
// for one spelling misses the other, a diff shows a change where nothing changed, and the reviewer
// spends attention on whitespace instead of on the classes.
//
// Runs over every static run of class text, which for a template means each piece between the
// interpolations. That is what makes it safe next to a hole: whitespace adjacent to an interpolation
// is load-bearing, because it is the only thing keeping the substituted value from fusing with the
// class beside it. Measured against upstream: `flex  ${x}` becomes `flex ${x}`, keeping exactly one
// space, and never `flex${x}`.
//
// Fixable and safe to fix unattended, because the repair is defined entirely by what the string
// already says. Whitespace-only becomes empty, matching upstream, rather than being deleted: the
// attribute is the author's and this rule has no opinion about whether it should exist.
var NoUnnecessaryWhitespace = rule.Rule{
	Name: "no-unnecessary-whitespace",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.SourceFile == nil {
			return nil
		}

		settings := DefaultClassLiteralSettings()
		if configured, isConfigured := options.(NoUnnecessaryWhitespaceOptions); isConfigured {
			if len(configured.Attributes) > 0 {
				settings.AttributeNames = configured.Attributes
			}
			if len(configured.Callees) > 0 {
				settings.CalleeNames = configured.Callees
			}
			if len(configured.Variables) > 0 {
				settings.VariablePatterns = configured.Variables
			}
		}

		reader := NewClassLiteralReader(settings)

		report := func(node *ast.Node) {
			for _, segment := range reader.ClassSegmentsIn(node) {
				tidied, needsTidying := tidyWhitespace(segment)
				if !needsTidying {
					continue
				}

				ctx.Report(rule.Diagnostic{
					Range:      segment.Range,
					Message:    messageUnnecessaryWhitespace(),
					SourceFile: ctx.SourceFile,
					Fixes:      []rule.Fix{rule.ReplaceRange(segment.Range, tidied)},
				})
			}
		}

		listeners := rule.Listeners{}
		for _, kind := range ListenerKinds() {
			listeners[kind] = report
		}
		return listeners
	},
}

// tidyWhitespace returns what a segment should say, and whether that differs from what it says.
//
// The rule is: collapse each run of whitespace that already separates two classes down to its first
// character, and drop the padding at the outer edges. Two properties make this safe next to an
// interpolation, and both were wrong in a first version that rebuilt the string from its classes:
//
//   - Existing separators are shortened, never synthesized. `px-${size}` has no whitespace at the
//     hole, so this rule adds none. That seam is `no-concatenated-classes`'s finding, and inventing
//     a space here would silently change `px-4` into two class names.
//   - A separator keeps its own first character rather than being replaced by a space. A tab
//     separates two class names as well as a space does, upstream leaves it alone, and rewriting it
//     would touch every tab-indented multi-line class list in the tree.
//
// Comparing the rewritten text against the original, rather than pattern-matching for defects, is
// what keeps the finding and the fix from disagreeing: the rule reports exactly when it has
// something different to propose.
func tidyWhitespace(segment ClassSegment) (string, bool) {
	runes := []rune(segment.Text)

	var builder strings.Builder
	for index := 0; index < len(runes); index++ {
		if !isSpace(runes[index]) {
			builder.WriteRune(runes[index])
			continue
		}

		// The full run of whitespace starting here.
		runStart := index
		for index < len(runes) && isSpace(runes[index]) {
			index++
		}
		index--

		atSegmentStart := runStart == 0
		atSegmentEnd := index == len(runes)-1

		// Padding at an outer edge goes entirely; padding at an edge that touches an interpolation
		// is a separator and keeps one character.
		if atSegmentStart && !segment.LeadingHole {
			continue
		}
		if atSegmentEnd && !segment.TrailingHole {
			continue
		}

		// Whitespace-only segments between two holes collapse to a single separator; a segment that
		// is nothing but padding at an outer edge disappears above.
		builder.WriteRune(runes[runStart])
	}

	tidied := builder.String()
	return tidied, tidied != segment.Text
}
