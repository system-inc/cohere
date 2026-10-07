package tailwind

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/ecmascript/text"
)

// ClassTemplate is class text that meets a value known only at runtime, broken at those seams: a
// template literal's holes, and a `+` joining a string to something else.
//
// This is deliberately a separate reading from ClassLiteral. A template is only partly knowable, so
// rules that rewrite class text must not see it, and the reader that feeds them keeps excluding it.
// Only a rule that reasons about the boundaries themselves wants this shape.
type ClassTemplate struct {
	// Node is the template, or the concatenated string.
	Node *ast.Node
	// Boundaries are the seams between static text and a runtime value, in source order.
	Boundaries []TemplateBoundary
	Origin     ClassLiteralOrigin
}

// TemplateBoundary is one seam where static class text meets an interpolation.
//
// Side says which way the static text faces: text ending at a hole is on the hole's left, text
// starting after a hole is on its right. Fragment is the class text actually touching the seam, and
// is empty when whitespace separates them, which is the case that is fine.
type TemplateBoundary struct {
	Side     TemplateBoundarySide
	Fragment string
	// Range points at the static text that owns the seam, so a finding lands on the fragment the
	// author wrote rather than on the whole template.
	Range core.TextRange
}

// TemplateBoundarySide distinguishes the two directions a fragment can touch a hole from.
type TemplateBoundarySide string

const (
	// TemplateBoundarySideBeforeHole is static text running up to an interpolation: the `px-` of
	// `px-${size}`.
	TemplateBoundarySideBeforeHole TemplateBoundarySide = "BeforeHole"
	// TemplateBoundarySideAfterHole is static text resuming after one: the `-4` of `${prefix}-4`.
	TemplateBoundarySideAfterHole TemplateBoundarySide = "AfterHole"
)

// ClassTemplatesIn returns every seam a node's class text has with a runtime value: each template
// with holes the reader found, and each string a `+` joins to something, which upstream's
// no-concatenated-classes reports alike (isConcatenatedLiteral).
//
// Read from the same reading as ClassLiteralsIn, so a rule reading seams covers exactly the strings a
// rule reading classes does. Anything else is a difference in coverage that nobody declared.
func (r *ClassLiteralReader) ClassTemplatesIn(node *ast.Node) []ClassTemplate {
	values := r.classValuesIn(node)
	var templates []ClassTemplate
	for _, template := range values.templates {
		if classTemplate, hasBoundaries := templateBoundaries(template); hasBoundaries {
			templates = append(templates, classTemplate)
		}
	}
	for _, literal := range values.literals {
		if classTemplate, hasBoundaries := concatenationBoundaries(literal); hasBoundaries {
			templates = append(templates, classTemplate)
		}
	}
	return templates
}

// concatenationBoundaries is a concatenated string's seams: its first class when a `+` joins
// something before it and no whitespace separates them, and its last when one joins something after.
func concatenationBoundaries(literal ClassLiteral) (ClassTemplate, bool) {
	classTemplate := ClassTemplate{Node: literal.Node, Origin: literal.Origin}
	if literal.Concatenated.Leading {
		if boundary, hasBoundary := boundaryAfterHole(literal.Text, literal.Range); hasBoundary {
			classTemplate.Boundaries = append(classTemplate.Boundaries, boundary)
		}
	}
	if literal.Concatenated.Trailing {
		if boundary, hasBoundary := boundaryBeforeHole(literal.Text, literal.Range); hasBoundary {
			classTemplate.Boundaries = append(classTemplate.Boundaries, boundary)
		}
	}
	return classTemplate, len(classTemplate.Boundaries) > 0
}

// templateBoundaries breaks a template into its boundaries.
//
// The head runs up to the first hole, and each span's literal runs from its own hole to the next, so
// every span contributes a boundary on its left and, unless it is the last, one on its right. That
// enumeration is the whole rule: a boundary is a defect exactly when no whitespace sits at the seam.
// A template that is itself a `+` operand has a seam at that outer end too.
func templateBoundaries(value classTemplateValue) (ClassTemplate, bool) {
	template := value.node.AsTemplateExpression()
	if template == nil || template.Head == nil || template.TemplateSpans == nil {
		return ClassTemplate{}, false
	}

	classTemplate := ClassTemplate{Node: value.node, Origin: value.origin}

	if value.concatenated.Leading {
		if boundary, hasBoundary := boundaryAfterHole(template.Head.Text(), template.Head.Loc); hasBoundary {
			classTemplate.Boundaries = append(classTemplate.Boundaries, boundary)
		}
	}

	// The head faces the first hole on its right.
	if boundary, hasBoundary := boundaryBeforeHole(template.Head.Text(), template.Head.Loc); hasBoundary {
		classTemplate.Boundaries = append(classTemplate.Boundaries, boundary)
	}

	spans := template.TemplateSpans.Nodes
	for index, spanNode := range spans {
		span := spanNode.AsTemplateSpan()
		if span == nil || span.Literal == nil {
			continue
		}
		text := span.Literal.Text()

		// Text resuming after this span's hole.
		if boundary, hasBoundary := boundaryAfterHole(text, span.Literal.Loc); hasBoundary {
			classTemplate.Boundaries = append(classTemplate.Boundaries, boundary)
		}

		// And, unless this is the final span, the text also runs up to the next hole, and the final
		// span's to whatever a `+` joins after the template.
		if index < len(spans)-1 || value.concatenated.Trailing {
			if boundary, hasBoundary := boundaryBeforeHole(text, span.Literal.Loc); hasBoundary {
				classTemplate.Boundaries = append(classTemplate.Boundaries, boundary)
			}
		}
	}

	return classTemplate, len(classTemplate.Boundaries) > 0
}

// boundaryBeforeHole reports the class fragment left touching an interpolation on its right.
//
// Empty text is fine: `${a}${b}` has nothing glued to anything, so there is no fragment whose name
// got broken. Text ending in whitespace is also fine, and is the shape the rule is teaching people
// to write.
func boundaryBeforeHole(spanText string, textRange core.TextRange) (TemplateBoundary, bool) {
	if spanText == "" || endsWithWhitespace(spanText) {
		return TemplateBoundary{}, false
	}

	fields := text.WhitespaceFields(spanText)
	if len(fields) == 0 {
		return TemplateBoundary{}, false
	}

	return TemplateBoundary{
		Side:     TemplateBoundarySideBeforeHole,
		Fragment: fields[len(fields)-1],
		Range:    textRange,
	}, true
}

// boundaryAfterHole reports the class fragment right touching an interpolation on its left.
func boundaryAfterHole(spanText string, textRange core.TextRange) (TemplateBoundary, bool) {
	if spanText == "" || startsWithWhitespace(spanText) {
		return TemplateBoundary{}, false
	}

	fields := text.WhitespaceFields(spanText)
	if len(fields) == 0 {
		return TemplateBoundary{}, false
	}

	return TemplateBoundary{
		Side:     TemplateBoundarySideAfterHole,
		Fragment: fields[0],
		Range:    textRange,
	}, true
}

// endsWithWhitespace and startsWithWhitespace ask about any whitespace, not only a space.
//
// A tab separates two class names as well as a space does, and upstream treats it that way: a
// tab-separated pair is not a finding for this rule or for the whitespace rule. Testing for `' '`
// alone would report every tab-indented multi-line class list in the tree.
func endsWithWhitespace(text string) bool {
	if text == "" {
		return false
	}
	trimmed := strings.TrimRightFunc(text, isSpace)
	return len(trimmed) != len(text)
}

func startsWithWhitespace(text string) bool {
	if text == "" {
		return false
	}
	trimmed := strings.TrimLeftFunc(text, isSpace)
	return len(trimmed) != len(text)
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f'
}
