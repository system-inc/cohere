// Live conflict resolution: what a class declares, asked of this repository's own `@utility` blocks
// first and of the framework's generated tables second.
//
// This is `no-conflicting-classes`'s half of the seam #3r6cxrb crosses, and it is the one where the
// answer is a split rather than a swap. The seam is drawn where it is because it was measured, and
// the measurement is below rather than in a commit message so the next person does not have to
// re-run it to know whether to trust the boundary.
//
// # What moved
//
// The class is now dissected by `ParseCandidate` against the live design system rather than by a
// prefix walk over `RootDeclaredProperties`. That fixes the same defect
// `enforce-canonical-classes` had: the walk re-derived where a root ends, and which roots exist is a
// question about this repository's `@utility` blocks as much as about the framework.
//
// The repository's own utilities are then answered by compiling them. `UtilityEvaluator.Compile`
// returns the declaration list of an `@utility` block, and its properties are the real CSS property
// names the class emits. Measured on the corpus: 85 classes are answered this way, every one an
// ahra or www-connected-app theme utility such as `background--0` or `content--placeholder`, and
// every one of them a class the framework tables can only answer because the generator was run
// against a repository that declared it.
//
// # What did not move, and why a reading is not a substitute
//
// The framework's own utilities stay on `RootDeclaredProperties` and its four companions. That is
// not reluctance; it is that nothing in shipped Go can produce the answer. `px-4` is not an
// `@utility` block, so the evaluator declines it — measured: `evaluatorHasRoot=false`,
// `compiledProps=[]`. Compiling it needs the framework's own utility handlers, which is
// `utilities.ts`, 6,827 lines, and porting it was rejected twice on this project already.
//
// The tempting shortcut is `Table.Lookup`, which is per-repository, already built, and returns
// `Order` — indices into `PropertyOrder`, invertible to property names. It gets the right answer on
// 1,355 of the corpus's 1,530 resolvable classes, including the two cases the conflict tables carry
// special entries for: `font-medium` reads `font-weight` and `font-mono` reads `font-family`, which
// is exactly what `ClassDeclaredProperties` exists to record.
//
// It is still the wrong source, and the 75 it gets wrong say why:
//
//   - **The two table families answer deliberately different questions.**
//     `OrderingPropertiesByRoot`'s own comment states it: the ordering tables keep `--tw-*` custom
//     properties because they are what separates `shadow-lg` from `ring-1` in the sort, and the
//     conflict tables strip them because two classes both setting `--tw-border-style` are not in
//     conflict about anything an author sees. Stripping them here recovers `[box-shadow]` for both,
//     which is right, and leaves `outline-none` and `ring-neutral-800` with nothing at all.
//   - **A `--tw-sort` override replaces the reading entirely.** `divide-y` reads `divide-y-width`,
//     which is not a CSS property and is not what the class declares; the class emits
//     `border-top-width`, `border-top-style` and their bottom pair. `PropertySort`'s own comment
//     records the latch that produces this. 8 corpus classes, all `divide-*`.
//   - **A reading is a sorted set of positions, not a declaration list.** `backdrop-blur-sm`
//     declares `-webkit-backdrop-filter` and `backdrop-filter`; only one has a position.
//
// So a class's sort position and a class's declared properties are two different facts that happen
// to coincide most of the time, and a rule built on the coincidence is wrong in the direction that
// invents conflicts between classes that compose. `no-conflicting-classes`'s own doc comment says a
// port that got this wrong "would report the first and be wrong on correct code, which is how a rule
// gets disabled."
//
// # The honest state of the boundary
//
// Five of this rule's seven tables remain framework data consulted through a repository-aware split.
// `ColorNames` is the one that is genuinely per-repository and still generated, and it is a real gap
// rather than a settled seam: a repository that defines its own palette has different color names,
// and this rule reads Tailwind's. It is narrowed rather than closed here — the theme is asked first,
// so a repository's own colors are recognised — and what remains is the framework's default palette
// as a fallback for a repository that did not override it.
package tailwind

import (
	"strings"

	tailwindengine "github.com/system-inc/verify/internal/tailwind"
)

// resolveClassFactsIn reads a class's four deciding facts against a live design system.
//
// Two sources, in the order upstream registers them: the repository's `@utility` blocks first,
// because `Utilities.set` overwrites and a repository redefining a framework root wins, then the
// framework's tables. That order is the same one `LoadedDesignSystem.HasUtility` and
// `class_order_key.go`'s `readingFor` use, so all three agree about who answers for a root.
//
// Returns false for a class neither source can read, which is a decline rather than a failure:
// `reportConflicts` skips it, so it participates in no pair. Measured on the corpus, 16 classes
// reach that state — `container`, the gradient stops, `[font:inherit]` and the two
// `data-[show=*]:fade-*` classes — each one a class whose properties no per-root table can state,
// and each unchanged by this migration.
func resolveClassFactsIn(className string, designSystem DesignSystemResult) (classFacts, bool) {
	if designSystem.System == nil {
		return classFacts{}, false
	}

	variants, base, _ := dissectClass(className)

	// The repository's own blocks, compiled. This is the half that could not be a table: an
	// `@utility` block is this repository's, and a generated table only knows the one it was
	// generated from.
	if facts, isRepositoryUtility := repositoryClassFacts(className, variants, designSystem); isRepositoryUtility {
		return facts, true
	}

	// The framework's half. Still the generated tables, for the reason in the file comment: nothing
	// in shipped Go compiles a framework utility, so there is no live source to prefer.
	//
	// The class is dissected by the live parser rather than by a prefix walk, so which root the
	// tables are consulted FOR is a repository-aware answer even though the tables are not.
	root, readsFunctionally := functionalRootIn(className, designSystem.System)

	// A static's properties are keyed on its whole name rather than on a root, so it is answered
	// before the root lookup and only when the parser did not read the class functionally. The order
	// matters on the names that are both: `flex` is the static `display: flex` AND the root of
	// `flex-4`, and taking the static entry for `flex-4` would give it `display`.
	if !readsFunctionally {
		properties, isStatic := tailwindengine.StaticDeclaredProperties[base]
		if !isStatic {
			return classFacts{}, false
		}
		return classFacts{
			ClassName:     className,
			Variants:      variants,
			SelectorShape: selectorShapeOf(base),
			Properties:    properties,
			Composes:      tailwindengine.ComposingRoots[base],
		}, true
	}

	properties := tailwindengine.RootDeclaredProperties[root]

	// A root's entry is the common case, and two kinds of class take a different reading. A color
	// value, handled per root because the color scale is large and uniform. And a handful of named
	// values whose properties simply differ: `font-medium` declares `font-weight` while `font-mono`
	// declares `font-family`, and both parse as root `font`. Taking the root's reading for those
	// reported `font-medium font-mono` as a conflict on correct code.
	if classProperties, hasClassReading := tailwindengine.ClassDeclaredProperties[base]; hasClassReading {
		properties = classProperties
	} else if colorProperties, hasColorReading := tailwindengine.RootColorProperties[root]; hasColorReading &&
		valueIsColorIn(base, root, designSystem.System) {
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

// repositoryClassFacts answers for a class the repository's own `@utility` blocks declare.
//
// Compiled rather than looked up, which is the whole reason this half can be live at all: the
// evaluator has the block's body and can say what it emits. `Compile` returns the declaration list,
// and its properties are real CSS property names.
//
// Custom properties are stripped, matching what the conflict tables do and for the same reason their
// doc comment gives: two classes both setting `--tw-border-style` are not in conflict about anything
// an author sees. A block that declares nothing else is therefore not resolvable here and falls
// through to the framework half, which is correct — `fade-in` is exactly that, an `@utility` block
// setting only `--enter-opacity`, and it has no visible property to collide on.
func repositoryClassFacts(
	className string,
	variants string,
	designSystem DesignSystemResult,
) (classFacts, bool) {
	evaluator := designSystem.System.Utilities()
	if evaluator == nil {
		return classFacts{}, false
	}

	parsed := tailwindengine.ParseCandidate(className, designSystem.System)
	if len(parsed) == 0 {
		return classFacts{}, false
	}
	// The first reading, matching every other consumer of the parser in this package. See
	// `gate.go:227` for the measurement that choice rests on.
	candidate := parsed[0]

	if !evaluator.Has(candidate.Root) {
		return classFacts{}, false
	}

	nodes, didCompile := evaluator.Compile(&candidate)
	if !didCompile {
		return classFacts{}, false
	}

	properties := make([]string, 0, len(nodes))
	seen := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		if node.Property == "" || strings.HasPrefix(node.Property, "--") {
			continue
		}
		if seen[node.Property] {
			continue
		}
		seen[node.Property] = true
		properties = append(properties, node.Property)
	}
	if len(properties) == 0 {
		return classFacts{}, false
	}

	return classFacts{
		ClassName: className,
		Variants:  variants,
		// A repository `@utility` block emits under a bare class selector. The non-default shapes
		// the framework has — `divide-*` under `:where(.CLASS > :not(:last-child))` — come from its
		// own handlers, and a repository block has no way to declare one: `@utility` takes a
		// declaration list, not a selector.
		SelectorShape: ".CLASS",
		Properties:    properties,
		Composes:      tailwindengine.ComposingRoots[candidate.Root],
	}, true
}

// functionalRootIn returns the root a class reads as, when the design system reads it functionally.
//
// This replaces `functionalRootOf`'s longest-prefix walk over `RootDeclaredProperties`. The walk's
// own comment records why longest-wins was needed — `border-l-4` has root `border-l` rather than
// `border`, and the shorter match gives it `border-width` instead of `border-left-width`, which
// changes what it conflicts with — and the parser answers the same question without re-deriving it.
//
// Returns false when the class reads as static or arbitrary, which the caller handles separately:
// a static's properties are keyed on its whole name rather than on a root.
func functionalRootIn(className string, system *tailwindengine.LoadedDesignSystem) (string, bool) {
	parsed := tailwindengine.ParseCandidate(className, system)
	if len(parsed) == 0 {
		return "", false
	}
	candidate := parsed[0]
	if candidate.Kind != tailwindengine.ParsedCandidateKindFunctional {
		return "", false
	}
	return candidate.Root, true
}

// valueIsColorIn reports whether a class's value names a color, asking this repository's theme first.
//
// The shipped version read `ColorNames`, generated from one repository, and its own comment already
// said why that is the wrong shape: "a project that customises its colors has different names and
// hardcoding Tailwind's defaults would be wrong for it." It then hardcoded a generated set, which is
// the same defect one step removed.
//
// The theme is asked first, so a repository's own palette is recognised wherever it defines one. The
// generated set remains as the fallback, and that is the honest state of this seam rather than a
// finished one: a repository that defines no `--color-*` of its own inherits Tailwind's, and this
// still reads them from a table rather than from the theme it inherited them into.
func valueIsColorIn(base string, root string, system *tailwindengine.LoadedDesignSystem) bool {
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

	// This repository's own palette, which is the half a table cannot carry.
	if system != nil {
		if _, isThemeColor := system.Theme().Get([]string{"--color-" + value}); isThemeColor {
			return true
		}
	}

	return tailwindengine.ColorNames[value]
}
