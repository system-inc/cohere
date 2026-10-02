package tailwind

import (
	"errors"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	tailwindengine "github.com/system-inc/cohere/internal/lint/rules/tailwind/collapse"
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
// ranks them null and expresses no opinion about where they go. So does every class that compiles
// to nothing: one that does not parse (`ahralia-splash`, `notavariant:flex`) and one whose value
// does not resolve (`text-dark` where the theme has no `dark` colour).
//
// That the engine has no opinion is the whole point: this dimension is not ported from
// `getClassOrder`, it is a convention chosen on top of it, and the corpus measurement below cannot
// speak to it. The convention is Tailwind's own Prettier plugin's, whose comparator returns -1 for
// any null against a ranked class and 0 for two nulls under a stable sort: `flex peer group
// items-center` becomes `peer group flex items-center`, and both `peer group` and `group peer` stand
// as written. Sorting the nulls alphabetically would reject `peer group`, so the tiebreak is source
// order rather than any ordering of our own.
//
// This reversed a previous convention of sorting them last. Nothing measured it in either
// direction; it was stated in this comment and then read back as though it had been. Then the
// nulls were the two markers only, and a literal holding any other null was declined. That was
// silent where the plugin reorders, and #vf1hd6j measured the plugin ranking six such classes null
// on the committed trees (`text-dark`, `dark:bg-dark-2`, `hover:content`, and three more).
//
// A class that parses and resolves and that this port still cannot place is not treated as null.
// The engine ranks it, so hoisting it would be a rewrite the plugin never makes, and the literal is
// declined instead: a port gap stays a missing finding rather than becoming a wrong fix.
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
// # The fix moves classes and leaves every separator where it was
//
// This rule shipped without a fix, on the reasoning that real class lists wrap across lines with
// indentation that carries intent, and a rewrite would reflow them. That objection is to joining
// the sorted classes with spaces, not to fixing. The plugin's own `sortClasses` splits the string
// into classes and the whitespace runs between them, sorts the classes, and puts them back into the
// same slots, so the third separator is still the third separator whatever moved around it. The
// fix does exactly that, and a wrapped list stays wrapped.
//
// It rewrites only the class slots whose class changes. Collapsing whitespace is
// `no-unnecessary-whitespace`'s finding and removing a repeat is `no-duplicate-classes`'s, and the
// bytes those two edit are never ones this fix claims, so all three land in the same pass
// (class_tokens.go). A literal with a repeat is not ordered at all until the repeat is gone.
//
// Prettier's plugin sorted every list in our trees until it left, which is why this fix exists: on
// a tree the plugin sorted it must change nothing, and the rule reported nothing on ahra before or
// after the null semantics above were widened.
//
// A literal whose source text differs from its value is reported and not fixed. An escape like
// ` ` decodes to a space, so the decoded classes cannot be written back at the source's offsets
// without rewriting the escape too, and a fix that silently decodes an author's escape is a change
// they did not ask for. `reorderFixes` names the other two cases.
var EnforceConsistentClassOrder = rule.Rule{
	Name: "better-tailwindcss/enforce-consistent-class-order",
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
		// and that is the one failure cohere exists to remove.
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

	// The unknown classes are not reported here, only placed. `no-unknown-classes` is the rule that
	// says which class is unknown, and two rules naming the same class in two different sentences is
	// how an author ends up fixing it twice.
	ordered, decided := orderClasses(classes, designSystem)
	if !decided {
		return
	}
	if strings.Join(ordered, " ") == strings.Join(classes, " ") {
		return
	}

	message := messageInconsistentClassOrder(strings.Join(ordered, " "))

	fixes, fixable := reorderFixes(ctx.SourceFile.Text(), literal, classes, ordered)
	if !fixable {
		ctx.ReportRange(literal.Range, message)
		return
	}

	ctx.Report(rule.Diagnostic{
		Range:      literal.Range,
		Message:    message,
		SourceFile: ctx.SourceFile,
		Fixes:      fixes,
	})
}

// reorderFixes writes into each class slot the class that belongs there, and touches nothing else.
//
// The plugin's `sortClasses` permutes the classes and puts them back between the same whitespace
// runs, so the separators never move. Writing only the slots whose class changes is that same
// result, with every separator byte left to `no-unnecessary-whitespace` and every unmoved class left
// alone, so the fix composes with the other fixers in one pass (class_tokens.go).
//
// Not fixable, and reported without a fix, in three cases. The source is not the decoded value (an
// escape), so slots cannot be found at source offsets. The slots are not the classes that were
// ordered: `SplitClasses` splits on every Unicode space and drops a fragment holding `${`, and the
// plugin's `[\t\r\f\n ]` splits on neither, so a `\v` inside a list would be two classes here and
// one there. Or a class that moves is one `no-deprecated-classes` renames, whose rename claims the
// same bytes: the order is settled on the pass after the rename lands.
func reorderFixes(sourceText string, literal ClassLiteral, classes []string, ordered []string) ([]rule.Fix, bool) {
	if strings.ContainsRune(literal.Text, '\v') {
		return nil, false
	}
	tokens, tokenized := classTokensIn(sourceText, literal.Range, literal.Text)
	if !tokenized {
		return nil, false
	}
	slots := classesOf(tokens)
	if len(slots) != len(classes) {
		return nil, false
	}
	for index, slot := range slots {
		if slot.Text != classes[index] {
			return nil, false
		}
	}

	fixes := []rule.Fix{}
	for index, slot := range slots {
		if slot.Text == ordered[index] {
			continue
		}
		if replacement, deprecated := deprecationFor(slot.Text); deprecated && replacement != "" {
			return nil, false
		}
		fixes = append(fixes, rule.ReplaceRange(slot.Range, ordered[index]))
	}
	return fixes, true
}

// orderClasses is the plugin's order for one literal, or false when this port cannot decide it.
//
// The engine's nulls lead in source order and the ranked classes follow in the engine's order. A
// class that parses and resolves and still cannot be placed is a gap in this port rather than a
// null, so the literal is declined: ordering the rest around it would be guessing.
func orderClasses(classes []string, designSystem DesignSystemResult) ([]string, bool) {
	unranked, ranked := partitionUnranked(classes, designSystem)
	keys, _, resolved := classOrderKeys(ranked, designSystem.System, designSystem.Table)
	if !resolved {
		return nil, false
	}
	return append(unranked, sortClassesByKey(ranked, keys)...), true
}

// partitionUnranked splits off every class the engine ranks null, keeping source order in both.
//
// `getClassOrder` returns null for a class that produces no CSS: the markers, which exist to be
// referenced by `group-hover:` on another element, and any class that does not compile. Both slices
// keep source order, which is what makes the tiebreak source order rather than an ordering of our
// own: sorting the nulls alphabetically would rewrite `peer group`, and the plugin accepts it.
func partitionUnranked(classes []string, designSystem DesignSystemResult) (unranked []string, ranked []string) {
	unranked = make([]string, 0, 2)
	ranked = make([]string, 0, len(classes))
	for _, className := range classes {
		if isMarkerClass(className) || !classCompilesIn(className, designSystem) {
			unranked = append(unranked, className)
			continue
		}
		ranked = append(ranked, className)
	}
	return unranked, ranked
}

// classCompilesIn reports whether the engine would emit CSS for a class, and so rank it.
//
// The same two questions `no-unknown-classes` asks, minus its allowances for markers and arbitrary
// properties, which it accepts as known for its own reasons: a marker is known and still unranked.
// Answering true for a nil system is the failing-safe direction, since a class wrongly read as
// ranked is declined by `classOrderKeys`, while one wrongly read as null is moved.
func classCompilesIn(className string, designSystem DesignSystemResult) bool {
	if designSystem.System == nil {
		return true
	}
	candidates := tailwindengine.ParseCandidate(className, designSystem.System)
	if len(candidates) == 0 {
		return false
	}
	return tailwindengine.ClassValueResolvesIn(&candidates[0], designSystem.System)
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
