package tailwind

import (
	"regexp"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
)

// messagePhysicalDirection names both the class and its replacement.
//
// The replacement is the whole value of the finding here. "Use a logical class" sends the reader to
// documentation; "use `ms-4` instead of `ml-4`" is the edit.
func messagePhysicalDirection(original string, replacement string) rule.Message {
	return rule.Message{
		Id: "useLogicalClass",
		Description: "The class \"" + original + "\" names a physical side, so it points the same " +
			"way regardless of reading direction and lays out backwards in a right-to-left locale. " +
			"Use \"" + replacement + "\", which is defined relative to the start and end of the " +
			"line rather than to left and right, so one class is correct in every locale.",
	}
}

// directionMapping is one physical class family and how to rewrite it.
//
// Expressed as a prefix match rather than a regular expression, which the original uses. The
// patterns are all anchored and all of the form "known prefix, then a value", so matching a prefix
// and keeping the remainder produces the same answer while staying readable and avoiding a regex
// compile per class per literal on a 139,000-literal tree.
type directionMapping struct {
	// prefix is what the class starts with, after any negative sign.
	prefix string

	// replacement is what that prefix becomes.
	replacement string

	// exact marks a mapping that matches the whole class rather than a prefix, so `border-l`
	// becomes `border-s` while `border-left-thing` is left alone.
	exact bool

	// negatable marks the families the original allows a leading `-` on. The distinction is the
	// original's rather than ours: it writes `^(-?)ml-(.+)$` for margins and `^border-l-(.+)$` with
	// no negative group for borders, so `-border-l-2` is not a violation to the gate even though it
	// would be to a rule that treated every family alike.
	negatable bool
}

// physicalToLogical is the mapping table, in the original's order.
//
// Order matters for the exact entries: `border-l` must be tested before `border-l-` would match, and
// both are present because the original lists them separately. Testing the exact form first is what
// makes `border-l` become `border-s` rather than `border-s-` with an empty value.
var physicalToLogical = []directionMapping{
	{prefix: "ml-", replacement: "ms-", negatable: true},
	{prefix: "mr-", replacement: "me-", negatable: true},
	{prefix: "pl-", replacement: "ps-", negatable: true},
	{prefix: "pr-", replacement: "pe-", negatable: true},
	{prefix: "text-left", replacement: "text-start", exact: true},
	{prefix: "text-right", replacement: "text-end", exact: true},
	{prefix: "left-", replacement: "start-", negatable: true},
	{prefix: "right-", replacement: "end-", negatable: true},
	{prefix: "border-l", replacement: "border-s", exact: true},
	{prefix: "border-l-", replacement: "border-s-"},
	{prefix: "border-r", replacement: "border-e", exact: true},
	{prefix: "border-r-", replacement: "border-e-"},
	{prefix: "rounded-l", replacement: "rounded-s", exact: true},
	{prefix: "rounded-l-", replacement: "rounded-s-"},
	{prefix: "rounded-r", replacement: "rounded-e", exact: true},
	{prefix: "rounded-r-", replacement: "rounded-e-"},
	{prefix: "scroll-ml-", replacement: "scroll-ms-"},
	{prefix: "scroll-mr-", replacement: "scroll-me-"},
	{prefix: "scroll-pl-", replacement: "scroll-ps-"},
	{prefix: "scroll-pr-", replacement: "scroll-pe-"},
}

// NoPhysicalDirection reports a Tailwind class that names left or right instead of start or end.
//
//	valid:   <div className="ms-4 text-start" />
//	valid:   <div className="rtl:ml-4" />          direction-aware on purpose
//	invalid: <div className="ml-4" />
//	invalid: <div className="md:hover:text-left" />
//
// Ported from `structure/tailwind-no-physical-direction`.
//
// # Every string literal, not only the class surfaces
//
// The other rules in this package read the attributes, callees and variables the configuration
// names. This one visits every string and template literal in the file, matching the original, and
// that breadth is load-bearing rather than incidental: all three findings on the tree sit in bare
// string entries of theme object arrays, reached by neither an attribute nor a known callee. A
// port that reused the class-literal reader would report zero and look correct.
//
// The cost of that breadth is a false-positive risk on strings that are not classes at all, which
// the original manages with a shape test rather than a whitelist. That test is reproduced exactly:
// a string qualifies if any of its space-separated tokens looks like a Tailwind class.
//
// # Prefixes strip, and two of them exempt
//
// `md:hover:ml-4` is a violation and `rtl:ml-4` is not. A variant prefix does not change what the
// class does, so the base is what gets tested; but `rtl:` and `ltr:` are the author saying the
// physical side is the point, which is the one case where a physical class is correct.
var NoPhysicalDirection = rule.Rule{
	Name: "better-tailwindcss/tailwind-no-physical-direction",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.SourceFile == nil {
			return nil
		}
		fileName := ctx.SourceFile.FileName()
		if !strings.HasSuffix(fileName, ".ts") && !strings.HasSuffix(fileName, ".tsx") {
			return nil
		}

		report := func(node *ast.Node, text string) {
			for _, violation := range physicalDirectionViolationsIn(text) {
				ctx.ReportNode(node, messagePhysicalDirection(violation.original, violation.replacement))
			}
		}

		return rule.Listeners{
			ast.KindStringLiteral: func(node *ast.Node) {
				report(node, node.Text())
			},
			ast.KindNoSubstitutionTemplateLiteral: func(node *ast.Node) {
				report(node, node.Text())
			},
			// An interpolated template is checked one static piece at a time, because the pieces are
			// what hold classes and the holes hold expressions. `flex ${x} ml-4` has to report on
			// its tail, and a rule reading only the head would miss every class after the first
			// interpolation.
			ast.KindTemplateExpression: func(node *ast.Node) {
				template := node.AsTemplateExpression()
				if template.Head != nil {
					report(template.Head, template.Head.Text())
				}
				if template.TemplateSpans == nil {
					return
				}
				for _, span := range template.TemplateSpans.Nodes {
					literal := span.AsTemplateSpan().Literal
					if literal != nil {
						report(literal, literal.Text())
					}
				}
			},
		}
	},
}

// physicalDirectionViolation is one offending class and what it should become.
type physicalDirectionViolation struct {
	original    string
	replacement string
}

// physicalDirectionViolationsIn scans a space-separated class string.
func physicalDirectionViolationsIn(classString string) []physicalDirectionViolation {
	if classString == "" || !looksLikeTailwindClasses(classString) {
		return nil
	}

	var violations []physicalDirectionViolation
	for _, token := range strings.Fields(classString) {
		base, prefixes := splitVariantPrefixes(token)

		// `rtl:` and `ltr:` are the author saying the physical side is deliberate.
		if isDirectionAware(prefixes) {
			continue
		}

		replacement, matched := logicalReplacementFor(base)
		if !matched {
			continue
		}
		if len(prefixes) > 0 {
			replacement = strings.Join(prefixes, ":") + ":" + replacement
		}
		violations = append(violations, physicalDirectionViolation{original: token, replacement: replacement})
	}
	return violations
}

// logicalReplacementFor rewrites one bare class, or reports that it is not a physical one.
func logicalReplacementFor(class string) (string, bool) {
	negative := ""
	body := class
	if strings.HasPrefix(body, "-") {
		negative = "-"
		body = body[1:]
	}

	for _, mapping := range physicalToLogical {
		if mapping.exact {
			if body != mapping.prefix {
				continue
			}
			// An exact mapping carries no value, so a leading `-` has nothing to attach to and the
			// original's pattern for these families has no negative group at all. `-text-left` is
			// therefore not a violation to the gate, and reproducing that is parity.
			if negative != "" {
				continue
			}
			return mapping.replacement, true
		}

		if !strings.HasPrefix(body, mapping.prefix) {
			continue
		}
		value := body[len(mapping.prefix):]
		// The original's patterns end in `(.+)`, which requires at least one character. A bare
		// `ml-` matches the prefix and has nothing after it, so it is not a violation.
		if value == "" {
			continue
		}
		if negative != "" && !mapping.negatable {
			continue
		}
		return negative + mapping.replacement + value, true
	}
	return "", false
}

// splitVariantPrefixes separates a class from its variant prefixes: `md:hover:ml-4`.
func splitVariantPrefixes(token string) (string, []string) {
	parts := strings.Split(token, ":")
	if len(parts) == 1 {
		return token, nil
	}
	return parts[len(parts)-1], parts[:len(parts)-1]
}

// isDirectionAware reports an `rtl:` or `ltr:` variant among the prefixes.
func isDirectionAware(prefixes []string) bool {
	for _, prefix := range prefixes {
		if prefix == "rtl" || prefix == "ltr" {
			return true
		}
	}
	return false
}

// looksLikeTailwindClasses is the original's shape test, reproduced as the same regular expression.
//
// The rule reads every string in the file, so something has to keep it away from prose, paths and
// identifiers. The original decides with this expression over each token and accepts the string if
// any token matches.
//
// # Written as a regex on purpose, after a hand-written scan of the same grammar was wrong
//
// The first version of this was a hand-rolled scan, on the reasoning that a regex per token across
// 139,000 literals would be expensive. It disagreed with the original on the most ordinary input
// there is: `ml-4` did not qualify, because the scan mis-split the value at the first dash rather
// than the last. The rule would have reported nothing at all, and every fixture written to the same
// misunderstanding would have passed.
//
// The lesson is cheaper than the bug: a grammar that already exists as a tested expression should be
// ported as that expression. Go compiles it once at package load, and the cost is a match against a
// token rather than a compile.
var tailwindClassPattern = regexp.MustCompile(`^-?(?:[a-z]+:)*(?:[a-z]+-)?[a-z]+(?:-[a-z0-9.\[\]/]+)?$`)

func looksLikeTailwindClasses(text string) bool {
	for _, token := range strings.Fields(text) {
		if tailwindClassPattern.MatchString(token) {
			return true
		}
	}
	return false
}
