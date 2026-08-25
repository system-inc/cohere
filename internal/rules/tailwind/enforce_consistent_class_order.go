package tailwind

import (
	"errors"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
)

func messageInconsistentClassOrder(ordered string) rule.Message {
	return rule.Message{
		Id: "inconsistentClassOrder",
		Description: "These classes are not in Tailwind's own order, which is \"" + ordered + "\". " +
			"Order carries no meaning to the browser, which is exactly why it should be mechanical: " +
			"two elements with the same classes should read as the same line, so a diff shows what " +
			"actually changed and a reader can scan a list without parsing it.",
	}
}

// EnforceConsistentClassOrderOptions lets a project name the surfaces that carry class strings.
type EnforceConsistentClassOrderOptions struct {
	Attributes []string
	Callees    []string
	Variables  []string
}

// EnforceConsistentClassOrder reports class lists written in an order other than Tailwind's.
//
//	valid:   <div className="flex items-center gap-2" />
//	valid:   <div className="px-4 hover:px-8" />
//	invalid: <div className="items-center flex" />
//	invalid: <div className="hover:px-8 px-4" />
//
// Class order has no effect on rendering, which is the reason to make it mechanical rather than a
// reason to ignore it. Two elements meaning the same thing should read as the same line: otherwise
// a diff shows a reordering as a change, a reviewer spends attention deciding whether it was one,
// and a reader scanning a forty-class list has to parse rather than recognise it.
//
// # The comparator
//
// Three dimensions, and each was found by a differential against the engine rather than by reading
// upstream, which delegates the whole question to `getClassOrder` in three lines.
//
// Unranked classes sort first, and their source order is preserved. `group` and `peer` generate no
// CSS of their own and exist to be referenced by `group-hover:` on other elements, so the engine
// ranks them null and expresses no opinion about where they go.
//
// That the engine has no opinion is the whole point: this dimension is not ported from
// `getClassOrder`, it is a convention chosen on top of it, and the corpus measurement below cannot
// speak to it. The convention is taken from Tailwind's own Prettier plugin and
// `better-tailwindcss`, verified against the latter directly: `flex peer group items-center` is
// rewritten to `peer group flex items-center`, while both `peer group` and `group peer` are
// accepted as written. Sorting the nulls alphabetically would reject `peer group`, so the
// tiebreak is source order rather than any ordering of our own.
//
// This reversed a previous convention of sorting them last. Nothing measured it in either
// direction; it was stated in this comment and then read back as though it had been.
//
// Then variant position, because the engine groups by variant before anything else. Ordering by
// root first agreed with the engine on 51% of the corpus; adding the variant dimension and taking
// representatives from unprefixed classes took it to 91%.
//
// Then the class's own position. That last one cannot be reduced to a position per root: 84 of
// 1,210 root groups are non-contiguous in the global order and those 84 hold 18,936 of the 37,643
// ranked classes, so a root table with an exception list saves nothing.
//
// Measured over the whole corpus: 2,465 of 2,465 literals ordered identically to the engine. That
// number covers the ranked dimensions only. `getClassOrder` returns null for the markers, so a
// literal containing one has no engine answer to be identical to, and the agreement was never
// evidence about their placement in either direction.
//
// # No fix, and this one is a closer call than the others
//
// Reordering a class list is mechanical and the rule knows the answer, so a fix is tempting. It is
// left out because the rewrite has to preserve a literal's own formatting, and real class lists in
// this codebase wrap across lines with indentation that carries intent. A fix that reflowed them
// would produce diffs larger than the defect it repaired, and reordering is the one defect where
// the noise of the repair can exceed the cost of the problem.
var EnforceConsistentClassOrder = rule.Rule{
	Name: "enforce-consistent-class-order",
	// Declared because the rule reaches ctx.Program to get the design system. The stylesheet graph
	// reaches files the program does not contain at all, so a findings cache keyed on the linted
	// file alone is stale whenever `theme.css` changes and the `.tsx` file does not: zero findings,
	// forever, indistinguishable from a clean tree.
	ReadsProgram: true,
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.SourceFile == nil {
			return nil
		}

		// The design system is resolved once per file rather than once per literal, and before the
		// listeners are built so a repository whose CSS will not parse costs one lookup rather than
		// one per class attribute. `DesignSystemForProgram` is itself cached on the program, so this
		// is a mutex and a pointer compare.
		//
		// The two failures are told apart rather than collapsed, which is what `ErrNoTailwindEntryPoint`
		// exists for. A project with no Tailwind at all is silence with nothing wrong: this rule is
		// registered unconditionally, so reporting there would put a finding on every file of every
		// repository that does not use Tailwind. A project that HAS Tailwind and whose CSS would not
		// read is the loud case, and it is reported once per file rather than swallowed, because a
		// rule reporting zero findings over unreadable CSS is indistinguishable from a clean tree
		// and that is the one failure verify exists to remove.
		designSystem := DesignSystemForProgram(ctx)
		if designSystem.Err != nil {
			if errors.Is(designSystem.Err, ErrNoTailwindEntryPoint) || ctx.Program == nil {
				return nil
			}
			return declineListeners(ctx, "enforce-consistent-class-order", designSystem)
		}

		settings := DefaultClassLiteralSettings()
		if configured, isConfigured := options.(EnforceConsistentClassOrderOptions); isConfigured {
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
			for _, literal := range reader.ClassLiteralsIn(node) {
				reportClassOrder(ctx, literal, designSystem)
			}
		}

		listeners := rule.Listeners{}
		for _, kind := range ListenerKinds() {
			listeners[kind] = report
		}
		return listeners
	},
}

// declineListeners reports, once per file, that the rule could not read this project's Tailwind.
//
// Attached to the source file rather than to the class literals, so a project whose stylesheet
// broke gets one finding per file instead of one per class attribute, and gets it even in a file
// that carries no classes at all. The second half matters more than it looks: a repository whose
// CSS stopped parsing would otherwise announce the problem only in the files that happen to hold a
// className, which is the subset a reader is least likely to open first.
func declineListeners(ctx rule.Context, ruleName string, designSystem DesignSystemResult) rule.Listeners {
	reported := false
	return rule.Listeners{
		ast.KindSourceFile: func(node *ast.Node) {
			if reported {
				return
			}
			reported = true
			ctx.ReportNode(node, rule.Message{
				Id:          "designSystemUnavailable",
				Description: DesignSystemDeclineMessage(ruleName, designSystem),
			})
		},
	}
}

// reportClassOrder compares a literal's class order against this repository's own design system.
//
// The whole literal is resolved in one population before anything is sorted, which is the shape the
// engine's own ordering requires rather than a convenience. See class_order_key.go: a variant's
// index is a rank among exactly the variants this list contains, so there is no per-class key to
// compute and no pairwise comparator that could compute one.
func reportClassOrder(ctx rule.Context, literal ClassLiteral, designSystem DesignSystemResult) {
	classes := SplitClasses(literal.Text)
	if len(classes) < 2 {
		return
	}

	// A repeated class is `no-duplicate-classes`'s finding. Sorting a list containing one would
	// report an ordering defect for a duplication problem, and the author would fix the wrong thing.
	seen := make(map[string]bool, len(classes))
	for _, className := range classes {
		if seen[className] {
			return
		}
		seen[className] = true
	}

	// The markers are separated out before anything is parsed, because the engine ranks them null
	// and so expresses no opinion about where they go. `ParseCandidate` returns nothing for them for
	// the same reason, so leaving them in the population would read as "this class is outside the
	// design system" and silence the literal.
	//
	// Where they go is therefore this consumer's convention rather than a ported fact, and it is
	// taken from the ecosystem: Tailwind's own Prettier plugin and `better-tailwindcss` both hoist
	// them, and both accept `peer group` and `group peer` as written. So they lead, in source order.
	markers, placeable := partitionMarkers(classes)

	// The class that could not be placed is deliberately not reported here, only used to decide.
	// `no-unknown-classes` is the rule that says which class is unknown, and two rules naming the
	// same class in two different sentences is how an author ends up fixing it twice.
	keys, _, resolved := classOrderKeys(placeable, designSystem.System, designSystem.Table)
	if !resolved {
		// A class this repository's design system cannot place means the literal holds something
		// outside it. Ordering the rest around it would be guessing.
		return
	}

	ordered := append(markers, sortClassesByKey(placeable, keys)...)
	if strings.Join(ordered, " ") == strings.Join(classes, " ") {
		return
	}

	ctx.ReportRange(literal.Range, messageInconsistentClassOrder(strings.Join(ordered, " ")))
}

// partitionMarkers splits the deliberately-unranked markers off the front of a class list.
//
// `group` and `peer` generate no CSS of their own and exist to be referenced by `group-hover:` on
// another element, so `getClassOrder` returns null for them and the engine's corpus agreement says
// nothing about where they belong. Both slices keep source order, which is what makes the tiebreak
// source order rather than an ordering of our own: sorting the markers alphabetically would rewrite
// `peer group`, and the reference implementations accept it.
func partitionMarkers(classes []string) (markers []string, placeable []string) {
	markers = make([]string, 0, 2)
	placeable = make([]string, 0, len(classes))
	for _, className := range classes {
		if isMarkerClass(className) {
			markers = append(markers, className)
			continue
		}
		placeable = append(placeable, className)
	}
	return markers, placeable
}

// isMarkerClass reports whether a class is one of the markers the engine ranks null.
//
// Checked against the whole class and against its base, because `dark:group` is still the marker
// and a variant prefix does not give it a reading.
func isMarkerClass(className string) bool {
	if alwaysKnownClasses.MatchString(className) {
		return true
	}
	_, base := splitVariants(className)
	return alwaysKnownClasses.MatchString(base)
}

// splitVariants separates a class's variant prefix from the rest.
//
// Deliberately not `dissectClass`: importance is part of the class for ordering purposes, since
// `px-4` and `px-4!` are different entries in the engine's order, while for the deprecation table
// they are the same utility.
func splitVariants(className string) (string, string) {
	index := strings.LastIndex(className, ":")
	if index < 0 {
		return "", className
	}
	return className[:index+1], className[index+1:]
}
