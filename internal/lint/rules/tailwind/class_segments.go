package tailwind

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// ClassSegment is one run of static class text, with the range that would rewrite exactly it.
//
// This is a third reading of the same surfaces, and the three are deliberately separate rather than
// one type with optional fields. They answer different questions and a rule that took the wrong one
// would be quietly wrong: ClassLiteral is "the whole class string, safe to rewrite", which excludes
// templates because they are only partly knowable; ClassTemplate is "where does static text meet an
// interpolation", which only a boundary rule wants; ClassSegment is "every run of static text,
// individually rewritable", which is what a rule that edits within a template needs.
//
// A plain string literal yields exactly one segment. A template yields one per static run, so
// `flex  ${x}  gap-2` yields two, and each can be repaired without touching the interpolation
// between them.
type ClassSegment struct {
	Text  string
	Range core.TextRange
	// LeadingHole and TrailingHole say whether an interpolation sits immediately on either side.
	//
	// They exist because the correct repair differs at a hole. Trimming a segment to nothing is
	// right at the edge of a literal and wrong next to a hole, where the whitespace is the only
	// thing keeping two class names apart once the value is substituted.
	LeadingHole  bool
	TrailingHole bool
	Origin       ClassLiteralOrigin
}

// segmentsOfTemplate walks a template's head and spans, recording each static run.
//
// The head has a hole on its right and nothing on its left. Every span has a hole on its left, and
// one on its right unless it is the last. That asymmetry is exactly what LeadingHole and
// TrailingHole record, so a rule can trim the outer edges of a template while leaving the single
// space next to a hole intact. A template that is itself inside another template's hole has a hole
// beyond both of its outer edges too, so neither edge is trimmed.
func segmentsOfTemplate(value classTemplateValue) []ClassSegment {
	node, origin := value.node, value.origin
	if node == nil {
		return nil
	}
	template := node.AsTemplateExpression()
	if template == nil || template.Head == nil || template.TemplateSpans == nil {
		return nil
	}

	var segments []ClassSegment
	sourceFile := ast.GetSourceFileOfNode(node)

	// Token ranges, not Loc. Loc starts before the whitespace in front of a token, which for a
	// template on a surface's own brace is nothing, so trimming one backtick off it was right for
	// every template the reader used to return. A template after `? ` or a span after `${ x }` has
	// trivia there, and the trim then lands on the delimiter: the range no longer matches the text,
	// the run reads as untokenizable, and every rule that edits runs skips it. The same defect
	// c079219 fixed for string literals, found here when templates in conditionals were first read.
	//
	// The head's own range spans "`flex  ${", delimiters included, so it is trimmed by one leading
	// backtick and two trailing characters. A fix written against the untrimmed range would rewrite
	// the backtick and the interpolation opener out of the source, producing a file that no longer
	// parses. Measured directly rather than assumed: the head of `flex  ${x}  gap-2` reports
	// [26,35) and covers exactly "`flex  ${".
	segments = append(segments, ClassSegment{
		Text:         template.Head.Text(),
		Range:        trimDelimiters(rule.TokenRange(sourceFile, template.Head), 1, 2),
		LeadingHole:  value.edges.Leading,
		TrailingHole: true,
		Origin:       origin,
	})

	spans := template.TemplateSpans.Nodes
	for index, spanNode := range spans {
		span := spanNode.AsTemplateSpan()
		if span == nil || span.Literal == nil {
			continue
		}

		// A middle span reads "}  gap-2${" and a final one reads "}  gap-2`", so both open with a
		// single closing brace and close with either two characters or one.
		trailing := 1
		if index < len(spans)-1 {
			trailing = 2
		}

		segments = append(segments, ClassSegment{
			Text:         span.Literal.Text(),
			Range:        trimDelimiters(rule.TokenRange(sourceFile, span.Literal), 1, trailing),
			LeadingHole:  true,
			TrailingHole: index < len(spans)-1 || value.edges.Trailing,
			Origin:       origin,
		})
	}

	return segments
}

// trimDelimiters narrows a range by the punctuation on either end.
//
// Returns the original range rather than an inverted one when the span is too short to hold the
// delimiters it is supposed to have. An inverted range is a fix that deletes backwards, and a rule
// should never be able to emit one.
func trimDelimiters(textRange core.TextRange, leading int, trailing int) core.TextRange {
	start := textRange.Pos() + leading
	end := textRange.End() - trailing
	if start > end {
		return textRange
	}
	return core.NewTextRange(start, end)
}
