package tailwind

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
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

// ClassSegmentsIn returns every run of static class text a node carries.
//
// Covers both plain strings and templates, over the same three surfaces the other readers use, so a
// rule built on this one cannot silently cover less than a rule built on those.
func (r *ClassLiteralReader) ClassSegmentsIn(node *ast.Node) []ClassSegment {
	if node == nil {
		return nil
	}

	// A plain string literal is one segment with no holes on either side.
	var segments []ClassSegment
	for _, literal := range r.ClassLiteralsIn(node) {
		segments = append(segments, ClassSegment{
			Text:   literal.Text,
			Range:  literal.Range,
			Origin: literal.Origin,
		})
	}
	if len(segments) > 0 {
		return segments
	}

	// Otherwise a template, broken into its static runs.
	//
	// Deliberately not routed through ClassTemplatesIn. That reader answers "where does static text
	// meet an interpolation without whitespace", so it returns nothing for a template whose seams
	// are all clean. `flex  ${x}  gap-2` has no boundary defects and two segments that still need
	// their doubled spaces collapsed, and reusing the boundary reader here would have silently
	// skipped exactly the templates that are otherwise well written.
	templateNode, origin := r.classTemplateNodeIn(node)
	return append(segments, segmentsOfTemplate(templateNode, origin)...)
}

// classTemplateNodeIn finds the template literal on a class surface, whatever its boundaries look
// like.
//
// Same surface dispatch as the other readers, so a rule built on segments covers the same three
// places a rule built on literals does.
func (r *ClassLiteralReader) classTemplateNodeIn(node *ast.Node) (*ast.Node, ClassLiteralOrigin) {
	switch node.Kind {
	case ast.KindJsxAttribute:
		attribute := node.AsJsxAttribute()
		if attribute == nil {
			return nil, ClassLiteralOriginAttribute
		}
		name := attribute.Name()
		if name == nil || !r.attributeNames[name.Text()] {
			return nil, ClassLiteralOriginAttribute
		}
		return templateExpressionOf(attribute.Initializer), ClassLiteralOriginAttribute

	case ast.KindCallExpression:
		call := node.AsCallExpression()
		if call == nil || call.Expression == nil || call.Arguments == nil {
			return nil, ClassLiteralOriginCallee
		}
		if call.Expression.Kind != ast.KindIdentifier || !r.calleeNames[call.Expression.Text()] {
			return nil, ClassLiteralOriginCallee
		}
		for _, argument := range call.Arguments.Nodes {
			if template := templateExpressionOf(argument); template != nil {
				return template, ClassLiteralOriginCallee
			}
		}
		return nil, ClassLiteralOriginCallee

	case ast.KindVariableDeclaration:
		declaration := node.AsVariableDeclaration()
		if declaration == nil || declaration.Initializer == nil {
			return nil, ClassLiteralOriginVariable
		}
		name := declaration.Name()
		if name == nil || name.Kind != ast.KindIdentifier {
			return nil, ClassLiteralOriginVariable
		}
		for _, pattern := range r.variablePatterns {
			if pattern.MatchString(name.Text()) {
				return templateExpressionOf(declaration.Initializer), ClassLiteralOriginVariable
			}
		}
		return nil, ClassLiteralOriginVariable
	}

	return nil, ClassLiteralOriginAttribute
}

// segmentsOfTemplate walks a template's head and spans, recording each static run.
//
// The head has a hole on its right and nothing on its left. Every span has a hole on its left, and
// one on its right unless it is the last. That asymmetry is exactly what LeadingHole and
// TrailingHole record, so a rule can trim the outer edges of a template while leaving the single
// space next to a hole intact.
func segmentsOfTemplate(node *ast.Node, origin ClassLiteralOrigin) []ClassSegment {
	if node == nil {
		return nil
	}
	template := node.AsTemplateExpression()
	if template == nil || template.Head == nil || template.TemplateSpans == nil {
		return nil
	}

	var segments []ClassSegment

	// The head's own range spans "`flex  ${", delimiters included, so it is trimmed by one leading
	// backtick and two trailing characters. A fix written against the untrimmed range would rewrite
	// the backtick and the interpolation opener out of the source, producing a file that no longer
	// parses. Measured directly rather than assumed: the head of `flex  ${x}  gap-2` reports
	// [26,35) and covers exactly "`flex  ${".
	segments = append(segments, ClassSegment{
		Text:         template.Head.Text(),
		Range:        trimDelimiters(template.Head.Loc, 1, 2),
		LeadingHole:  false,
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
			Range:        trimDelimiters(span.Literal.Loc, 1, trailing),
			LeadingHole:  true,
			TrailingHole: index < len(spans)-1,
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
