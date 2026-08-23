package tailwind

import (
	"sort"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
	tailwindengine "github.com/system-inc/verify/internal/tailwind"
)

func messageConflictingClasses(className string, conflicting []string, properties []string) rule.Message {
	return rule.Message{
		Id: "conflictingClasses",
		Description: "The class \"" + className + "\" and \"" + strings.Join(conflicting, "\", \"") +
			"\" set the same CSS " + propertyWord(properties) + " (" + strings.Join(properties, ", ") +
			"), so only one of them takes effect and which one depends on the order Tailwind emitted " +
			"them in rather than the order they are written here. Keep the one that was meant and " +
			"remove the rest.",
	}
}

func propertyWord(properties []string) string {
	if len(properties) == 1 {
		return "property"
	}
	return "properties"
}

// NoConflictingClassesOptions lets a project name the surfaces that carry class strings.
type NoConflictingClassesOptions struct {
	Attributes []string
	Callees    []string
	Variables  []string
}

// NoConflictingClasses reports two classes in one literal that set the same CSS property.
//
//	valid:   <div className="flex items-center" />
//	valid:   <div className="w-8 h-8" />
//	valid:   <div className="p-4 px-8" />
//	valid:   <div className="flex hover:block" />
//	invalid: <div className="flex block" />
//	invalid: <div className="px-4 px-8" />
//
// Only one of two conflicting classes takes effect, and which one is decided by the order Tailwind
// emitted its stylesheet in rather than the order the author wrote them. So the markup reads as
// though it says one thing and renders as though it says another, and editing the order of the
// class list does not change the outcome, which is what makes this genuinely confusing to debug.
//
// Three distinctions decide what conflicts, all measured against the real plugin rather than
// reasoned about, and each one inverts an answer if it is lost:
//
// Property names, not values. `w-8` and `h-8` declare the same value under `width` and `height` and
// do not conflict; they collapse into `size-8`, which is a different rule's finding. `px-4` and
// `px-8` declare different values under one property and do conflict.
//
// Shorthands are not normalised. `p-4` and `px-8` visually overlap and upstream reports no conflict,
// because `padding` and `padding-inline` are different property names. Normalising them here would
// invent a finding upstream does not report.
//
// Variants are part of the identity. `flex hover:block` is not a conflict, because the two apply in
// different states and both take effect in the state they belong to. `hover:flex hover:block` is a
// conflict. A port that compared bare class names would report the first and be wrong on correct
// code, which is how a rule gets disabled.
//
// Reported symmetrically, matching upstream: `flex block` produces two findings, one anchored at
// each class. A port emitting one finding per pair under-reports by half.
//
// The fix is a Suggestion rather than a Fix, deliberately. Removing either class changes what
// renders, and only the author knows which one was meant. The edit engine never applies
// suggestions, so this is the difference between a repair a human chooses and one that silently
// picks for them.
var NoConflictingClasses = rule.Rule{
	Name: "no-conflicting-classes",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.SourceFile == nil {
			return nil
		}

		settings := DefaultClassLiteralSettings()
		if configured, isConfigured := options.(NoConflictingClassesOptions); isConfigured {
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
				reportConflicts(ctx, literal)
			}
		}

		listeners := rule.Listeners{}
		for _, kind := range ListenerKinds() {
			listeners[kind] = report
		}
		return listeners
	},
}

// classFacts is one class resolved to everything that decides whether it can collide.
//
// Four things, and each was found by a false positive on the real tree rather than by reading
// upstream: the state it applies in, the element it lands on, what it sets, and whether it layers.
type classFacts struct {
	ClassName string
	// Variants decides which state the class applies in. `flex hover:block` is not a conflict.
	Variants string
	// SelectorShape decides which element it lands on. `divide-*` emits under
	// `:where(.CLASS > :not(:last-child))` and targets children, so it never collides with
	// `border-*` on the element itself, even though both declare `border-color`.
	SelectorShape string
	// Properties must match as a whole set, not merely overlap. `border` sets style plus width and
	// `border-dashed` sets style alone; they compose rather than collide.
	Properties []string
	// Composes marks a root whose utilities layer through custom properties rather than overwrite.
	// Every `shadow-*` and `ring-*` emits the same `box-shadow` var chain, so two of them never
	// conflict with each other.
	Composes bool
}

// reportConflicts finds classes in one literal that collide on the same element in the same state.
func reportConflicts(ctx rule.Context, literal ClassLiteral) {
	classes := SplitClasses(literal.Text)
	if len(classes) < 2 {
		return
	}

	// A repeated class is `no-duplicate-classes`'s finding; pairing it with itself would report
	// every duplicate in the tree as a conflict.
	seen := make(map[string]bool, len(classes))
	distinct := make([]string, 0, len(classes))
	for _, className := range classes {
		if seen[className] {
			continue
		}
		seen[className] = true
		distinct = append(distinct, className)
	}

	resolved := make([]classFacts, 0, len(distinct))
	for _, className := range distinct {
		facts, canResolve := resolveClassFacts(className)
		if !canResolve {
			continue
		}
		resolved = append(resolved, facts)
	}

	for index, subject := range resolved {
		if subject.Composes {
			continue
		}

		conflictingNames := make([]string, 0, len(resolved))
		conflictingProperties := map[string]bool{}

		for otherIndex, other := range resolved {
			if otherIndex == index || other.Composes {
				continue
			}
			if other.Variants != subject.Variants || other.SelectorShape != subject.SelectorShape {
				continue
			}

			// The whole property set must match, not merely overlap. Upstream bails when the two
			// declare different numbers of properties and again when any is missing from the other.
			shared := sharedProperties(subject.Properties, other.Properties)
			if len(shared) == 0 || len(shared) != len(subject.Properties) || len(shared) != len(other.Properties) {
				continue
			}

			conflictingNames = append(conflictingNames, other.ClassName)
			for _, property := range shared {
				conflictingProperties[property] = true
			}
		}

		if len(conflictingNames) == 0 {
			continue
		}

		properties := make([]string, 0, len(conflictingProperties))
		for property := range conflictingProperties {
			properties = append(properties, property)
		}
		sort.Strings(properties)

		// Reported per class rather than per pair, which is what makes the output symmetric and
		// matches upstream: `flex block` produces two findings, one anchored at each class.
		ctx.ReportRange(literal.Range, messageConflictingClasses(subject.ClassName, conflictingNames, properties))
	}
}

// resolveClassFacts reads a class's four deciding facts out of the generated tables.
func resolveClassFacts(className string) (classFacts, bool) {
	variants, base, _ := dissectClass(className)

	if properties, isStatic := tailwindengine.StaticDeclaredProperties[base]; isStatic {
		return classFacts{
			ClassName:     className,
			Variants:      variants,
			SelectorShape: selectorShapeOf(base),
			Properties:    properties,
			Composes:      tailwindengine.ComposingRoots[base],
		}, true
	}

	root := functionalRootOf(base)
	if root == "" {
		return classFacts{}, false
	}

	properties := tailwindengine.RootDeclaredProperties[root]

	// A root's entry is the common case, and two kinds of class take a different reading. A color
	// value, handled per root because the color scale is large and uniform. And a handful of named
	// values whose properties simply differ: `font-medium` declares `font-weight` while `font-mono`
	// declares `font-family`, and both parse as root `font`. Taking the root's reading for those
	// reported `font-medium font-mono` as a conflict on correct code.
	if classProperties, hasClassReading := tailwindengine.ClassDeclaredProperties[base]; hasClassReading {
		properties = classProperties
	} else if colorProperties, hasColorReading := tailwindengine.RootColorProperties[root]; hasColorReading && valueIsColor(base, root) {
		properties = colorProperties
	}
	if len(properties) == 0 {
		return classFacts{}, false
	}

	return classFacts{
		ClassName:     className,
		Variants:      variants,
		SelectorShape: selectorShapeOf(root),
		Properties:    properties,
		Composes:      tailwindengine.ComposingRoots[root],
	}, true
}

// selectorShapeOf returns the selector a utility emits under, defaulting to a bare class.
//
// Only non-default shapes are stored, so an absent entry means the utility lands on the element
// itself rather than on a descendant or a pseudo-element.
func selectorShapeOf(rootOrName string) string {
	if shape, hasShape := tailwindengine.RootSelectorShapes[rootOrName]; hasShape {
		return shape
	}
	return ".CLASS"
}

// valueIsColor reports whether a class's value names one of the theme's colors.
//
// Read from the generated palette rather than guessed, because a project that customises its colors
// has different names and hardcoding Tailwind's defaults would be wrong for it. An arbitrary value
// is judged by its shape: `[#fff]` and `[red]` are colors while `[3px]` is a length, which matches
// what the engine does with them.
func valueIsColor(base string, root string) bool {
	if len(base) <= len(root) {
		return false
	}
	value := strings.TrimPrefix(base[len(root):], "-")

	// An opacity modifier is part of the color, not of the name: `white/30` is `white`.
	if slash := strings.Index(value, "/"); slash >= 0 {
		value = value[:slash]
	}

	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		inner := strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
		// A length carries a unit or is bare digits; anything else in brackets reads as a color.
		return !looksLikeLength(inner)
	}

	return tailwindengine.ColorNames[value]
}

// looksLikeLength reports whether an arbitrary value is a measurement rather than a color.
func looksLikeLength(value string) bool {
	if value == "" {
		return false
	}
	if value[0] == '#' {
		return false
	}
	// Leading digit or sign covers `3px`, `0`, `-2rem`, `50%`.
	first := value[0]
	return first == '-' || first == '.' || (first >= '0' && first <= '9')
}

// functionalRootOf finds the longest registered root that prefixes a class base.
//
// Longest wins, and that is the whole reason this is not a dash-split: `border-l-4` has root
// `border-l` rather than `border`, and taking the shorter match would give it `border-width`
// instead of `border-left-width`, which changes what it conflicts with. The same mistake in a
// different guise is what silently stopped `border-x` being reported during the migration.
func functionalRootOf(base string) string {
	longest := ""
	for root := range tailwindengine.RootDeclaredProperties {
		if !strings.HasPrefix(base, root) {
			continue
		}
		// The root must be followed by a value separator, so `p` does not match `px-4`.
		if len(base) > len(root) && base[len(root)] != '-' {
			continue
		}
		if len(root) > len(longest) {
			longest = root
		}
	}
	return longest
}

// sharedProperties returns the properties two classes both declare.
func sharedProperties(left []string, right []string) []string {
	shared := make([]string, 0, len(left))
	for _, property := range left {
		for _, other := range right {
			if property == other {
				shared = append(shared, property)
				break
			}
		}
	}
	return shared
}
