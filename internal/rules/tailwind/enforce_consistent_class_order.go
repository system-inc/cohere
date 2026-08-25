package tailwind

import (
	"sort"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
	tailwindengine "github.com/system-inc/verify/internal/tailwind"
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
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.SourceFile == nil {
			return nil
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
				reportClassOrder(ctx, literal)
			}
		}

		listeners := rule.Listeners{}
		for _, kind := range ListenerKinds() {
			listeners[kind] = report
		}
		return listeners
	},
}

// reportClassOrder compares a literal's class order against Tailwind's.
func reportClassOrder(ctx rule.Context, literal ClassLiteral) {
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

	// A class Tailwind does not rank at all, other than the deliberately-unranked markers, means the
	// literal holds something outside the design system. Ordering it would be guessing, and
	// `no-unknown-classes` is the rule that has something to say about it.
	for _, className := range classes {
		if !isRankableClass(className) {
			return
		}
	}

	ordered := make([]string, len(classes))
	copy(ordered, classes)
	sort.SliceStable(ordered, func(left int, right int) bool {
		return classSortsBefore(ordered[left], ordered[right])
	})

	if strings.Join(ordered, " ") == strings.Join(classes, " ") {
		return
	}

	ctx.ReportRange(literal.Range, messageInconsistentClassOrder(strings.Join(ordered, " ")))
}

// classSortsBefore is Tailwind's own comparator, ported.
//
// From the `astNodes.sort` in Tailwind's `compile.ts`: variant, then the first differing index into
// the property order, then more declarations first, then alphabetically. Porting the comparator
// rather than tabulating its answers is the difference between 359 rows and 37,643.
func classSortsBefore(left string, right string) bool {
	leftKey := sortKeyFor(left)
	rightKey := sortKeyFor(right)

	// A class the tables cannot place sorts first, matching Tailwind's own Prettier plugin and
	// `better-tailwindcss`. The engine ranks `group` and `peer` null and so expresses no opinion;
	// the placement is the consumer's convention, and agreeing with the ecosystem's is worth more
	// than a defensible convention of our own that reorders every file the other tools accept.
	if leftKey.placeable != rightKey.placeable {
		return !leftKey.placeable
	}

	// Two unplaceable classes keep their source order. The sort is stable, so returning false for
	// every such pair leaves them as written. Ordering them alphabetically instead was measurably
	// wrong: `peer group` is accepted by the reference and alphabetical would rewrite it.
	if !leftKey.placeable {
		return false
	}

	// Compared through the segment comparator rather than as a single position, because a compound
	// prefix has no position of its own: `dark:placeholder:` and `dark:focus:` both fall to the
	// unknown fallback and would tie, then be separated by their properties instead of their
	// variants. That was four real class lists.
	if variantComparison := compareVariants(leftKey.variants, rightKey.variants); variantComparison != 0 {
		return variantComparison < 0
	}

	// The first position where the two property lists differ decides. Both are ascending, so this
	// walks them in lockstep exactly as the engine does.
	offset := 0
	for offset < len(leftKey.order) && offset < len(rightKey.order) && leftKey.order[offset] == rightKey.order[offset] {
		offset++
	}
	leftIndex := propertyIndexBeyond(leftKey.order, offset)
	rightIndex := propertyIndexBeyond(rightKey.order, offset)
	if leftIndex != rightIndex {
		return leftIndex < rightIndex
	}

	// Both tiebreaks are load-bearing. `space-x-1` and `me-0.5` reach here with the same first index
	// and are separated by count.
	if leftKey.count != rightKey.count {
		return leftKey.count > rightKey.count
	}
	return left < right
}

// classSortKey is everything the comparator needs about one class.
type classSortKey struct {
	placeable bool
	variant   int
	// order is the class's property indices, deduplicated and ascending, matching what Tailwind's
	// getPropertySort builds.
	order []int
	// count is every declaration including custom properties, which is what the engine counts.
	count int
	// variants is the prefix as written, kept so compounds compare segment by segment.
	variants string
}

// propertyIndexBeyond returns the index at a position, or a value past every real one.
//
// The engine uses Infinity here, so a class that ran out of properties sorts after one that has more.
func propertyIndexBeyond(order []int, offset int) int {
	if offset < len(order) {
		return order[offset]
	}
	return 1 << 30
}

// sortKeyFor reads a class's sort key from the generated tables.
func sortKeyFor(className string) classSortKey {
	variants, base := splitVariants(className)

	// The override is checked first, because a utility that declares `--tw-sort` sorts at that
	// property regardless of what else it declares, and some of them declare nothing this table
	// records: `container` emits several rules and is deliberately absent from the property tables,
	// so resolving properties first bailed before the override could apply.
	if overrideProperty, hasOverride := sortOverrideFor(base); hasOverride {
		if position, isOrdered := tailwindengine.PropertyOrder[overrideProperty]; isOrdered {
			return classSortKey{placeable: true, variant: variantPosition(variants), variants: variants, order: []int{position}, count: 1}
		}
	}

	properties := declaredPropertiesForOrdering(base)
	if len(properties) == 0 {
		return classSortKey{placeable: false}
	}

	order := make([]int, 0, len(properties))
	seen := make(map[int]bool, len(properties))
	for _, property := range properties {
		position, isOrdered := tailwindengine.PropertyOrder[property]
		if !isOrdered || seen[position] {
			continue
		}
		seen[position] = true
		order = append(order, position)
	}
	sort.Ints(order)

	// A class whose properties are all outside the order still has a place: it sorts after every
	// class that has one, but before anything unplaceable. `outline-none` declares only
	// `--tw-outline-style`, which the order does not list, and treating it as unplaceable sent it to
	// the very end instead of just after its neighbours.
	if len(order) == 0 {
		order = []int{1 << 29}
	}

	return classSortKey{placeable: true, variant: variantPosition(variants), variants: variants, order: order, count: len(properties)}
}

// sortOverrideProperties maps a utility root to the property its `--tw-sort` names.
//
// The values are also generated into `SortOverrideProperties`, read out of Tailwind's bundle; the
// roots live here because the minified bundle does not pair them with their utility in any
// recoverable way. Sixteen exist across the whole framework, and a generated value the map below
// does not cover is a signal that a new one was added.
var sortOverrideProperties = map[string]string{
	"space-x":          "row-gap",
	"space-y":          "column-gap",
	"space-x-reverse":  "row-gap",
	"space-y-reverse":  "column-gap",
	"divide":           "divide-color",
	"divide-x":         "divide-x-width",
	"divide-y":         "divide-y-width",
	"divide-y-reverse": "divide-style",
	"placeholder":      "placeholder-color",
	"from":             "--tw-gradient-from",
	"via":              "--tw-gradient-via",
	"to":               "--tw-gradient-to",
	"container":        "--tw-container-component",
	"size":             "size",
}

// sortOverrideFor finds the `--tw-sort` property for a class base, by longest matching root.
func sortOverrideFor(base string) (string, bool) {
	if property, isOverridden := sortOverrideProperties[base]; isOverridden {
		return property, true
	}

	longest := ""
	for root := range sortOverrideProperties {
		if !strings.HasPrefix(base, root) {
			continue
		}
		if len(base) > len(root) && base[len(root)] != '-' {
			continue
		}
		if len(root) > len(longest) {
			longest = root
		}
	}
	if longest == "" {
		return "", false
	}
	return sortOverrideProperties[longest], true
}

// declaredPropertiesForOrdering resolves a class base to the properties it declares.
func declaredPropertiesForOrdering(base string) []string {
	if properties, isStatic := tailwindengine.OrderingPropertiesByStatic[base]; isStatic {
		return properties
	}

	root := functionalRootOf(base)
	if root == "" {
		return nil
	}

	// A root whose emitted declarations depend on its value has one row per distinct reading,
	// keyed by the root and that value. `font-medium` emits `font-weight` and `font-sans` emits
	// `font-family`, so the root's own entry cannot answer for both.
	// A colour value matches one row covering the whole palette; any other value matches its own.
	//
	// A `(--custom-property)` value counts as a colour here when the root has a colour reading at
	// all. `ring-(--color-content--3)` is the theme's own colour token and the engine sorts it at
	// `--tw-ring-color`, but the name is a variable rather than a palette entry so a colour-name
	// lookup misses it, and it fell back to the width reading.
	if valueIsColor(base, root) || isCustomPropertyValue(base, root) {
		if properties, hasReading := tailwindengine.OrderingPropertiesByClass[root+"\x00\x01color"]; hasReading {
			return properties
		}
	}
	if properties, hasReading := tailwindengine.OrderingPropertiesByClass[root+"\x00"+valueTextOfClass(base, root)]; hasReading {
		return properties
	}

	return tailwindengine.OrderingPropertiesByRoot[root]
}

// variantPosition ranks a variant prefix, from the generated order.
//
// Bare classes come first. A first version ranked by prefix length, which put `focus:` before
// `hover:` because it is shorter and is not the order Tailwind emits them in.
func variantPosition(variants string) int {
	if variants == "" {
		return -1
	}
	if position, isOrdered := tailwindengine.VariantOrder[variants]; isOrdered {
		return position
	}
	// A variant the table has never seen sorts after every known one, consistently, so two classes
	// sharing it still compare by their own properties rather than arbitrarily.
	return 1 << 30
}

// variantSegments splits a variant prefix into its parts, outermost first.
//
// `dark:placeholder:` is two variants stacked, and the table holds only single ones. Ranking the
// whole prefix gives every compound the same unknown position, so `dark:placeholder:` and
// `dark:focus:` could not be separated at all and four real class lists came out wrong. Comparing
// segment by segment resolves them on the first that differs, which is what the engine does.
func variantSegments(variants string) []int {
	if variants == "" {
		return nil
	}

	positions := make([]int, 0, 2)
	for _, segment := range strings.Split(strings.TrimSuffix(variants, ":"), ":") {
		if segment == "" {
			continue
		}
		positions = append(positions, variantPosition(namedGroupBase(segment)+":"))
	}
	return positions
}

// compareVariants orders two variant prefixes, resolving compounds on their first differing part.
//
// A prefix that is a proper prefix of the other sorts first, so `dark:` precedes `dark:focus:`,
// matching how a shorter selector precedes the one that narrows it.
func compareVariants(left string, right string) int {
	if left == right {
		return 0
	}

	// The whole prefix wins when the table knows it, because a compound Tailwind names itself is
	// ranked directly rather than assembled.
	leftWhole, leftKnown := tailwindengine.VariantOrder[left]
	rightWhole, rightKnown := tailwindengine.VariantOrder[right]
	if leftKnown && rightKnown {
		switch {
		case leftWhole < rightWhole:
			return -1
		case leftWhole > rightWhole:
			return 1
		default:
			return 0
		}
	}

	leftParts, rightParts := variantSegments(left), variantSegments(right)
	leftNames, rightNames := variantSegmentNames(left), variantSegmentNames(right)

	// Depth first: every single variant precedes every stacked one, whatever they start with. The
	// engine emits `group-hover:` and `disabled:` before `group-hover:disabled:`, because a stacked
	// variant narrows an already-narrowed selector and lands in a later layer. Comparing the first
	// segment instead groups `group-hover:disabled:` with `group-hover:`, which put two classes in
	// the wrong place.
	if len(leftParts) != len(rightParts) {
		if len(leftParts) < len(rightParts) {
			return -1
		}
		return 1
	}
	for index := 0; index < len(leftParts) && index < len(rightParts); index++ {
		if leftParts[index] != rightParts[index] {
			if leftParts[index] < rightParts[index] {
				return -1
			}
			return 1
		}
		// Two variants the table does not know tie on position, and the engine still groups each
		// one's classes together. Ordering them by their own text keeps that grouping: without it
		// `data-[show=false]:` and `data-[show=true]:` interleave and their classes are sorted by
		// property across both groups.
		if leftParts[index] == 1<<30 && leftNames[index] != rightNames[index] {
			if leftNames[index] < rightNames[index] {
				return -1
			}
			return 1
		}
	}

	switch {
	case len(leftParts) < len(rightParts):
		return -1
	case len(leftParts) > len(rightParts):
		return 1
	default:
		return 0
	}
}

// isRankableClass reports whether ordering this class is something the tables can answer.
func isRankableClass(className string) bool {
	if alwaysKnownClasses.MatchString(className) {
		return true
	}
	_, base := splitVariants(className)
	if alwaysKnownClasses.MatchString(base) {
		return true
	}
	return sortKeyFor(className).placeable
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

// valueTextOfClass renders the value a class carries, as the ordering exceptions key it.
//
// An opacity modifier is part of the colour rather than the name, and an arbitrary value has no
// stable text to key on, so both reduce to the plain value the table recorded.
func valueTextOfClass(base string, root string) string {
	if len(base) <= len(root) {
		return ""
	}
	value := strings.TrimPrefix(base[len(root):], "-")
	if slash := strings.Index(value, "/"); slash >= 0 {
		value = value[:slash]
	}
	if strings.HasPrefix(value, "[") || strings.HasPrefix(value, "(") {
		return ""
	}
	return value
}

// isCustomPropertyValue reports whether a class's value is a `(--name)` reference.
func isCustomPropertyValue(base string, root string) bool {
	if len(base) <= len(root) {
		return false
	}
	value := strings.TrimPrefix(base[len(root):], "-")
	return strings.HasPrefix(value, "(--")
}

// namedGroupBase strips the name from a named group or peer variant.
//
// `group-hover/csv-download:` is `group-hover:` applied to a specific named group, and it sorts
// where `group-hover:` sorts. The table holds the unnamed forms, so an unstripped name falls to the
// unknown position and ties with every other named variant.
func namedGroupBase(segment string) string {
	if slash := strings.Index(segment, "/"); slash >= 0 {
		return segment[:slash]
	}
	return segment
}

// variantSegmentNames is variantSegments' companion, returning the text of each part.
//
// Needed only for variants the table cannot rank, where the text is the sole stable thing to order
// by. Kept alongside rather than merged into one slice of pairs, so the common path stays integers.
func variantSegmentNames(variants string) []string {
	if variants == "" {
		return nil
	}

	names := make([]string, 0, 2)
	for _, segment := range strings.Split(strings.TrimSuffix(variants, ":"), ":") {
		if segment == "" {
			continue
		}
		names = append(names, segment)
	}
	return names
}
