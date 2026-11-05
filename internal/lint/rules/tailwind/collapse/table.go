package tailwind

import "strings"

// Table is a whole design system's descriptors.
//
// It is generated per repository and never checked in. `Namespaces` is keyed on this repository's
// own `@theme`, so a committed table would bake one repository's tokens into a file claiming to
// describe Tailwind, which is the bug this port exists to fix. Measured: generating against
// ~/Projects/ahra and www-connected-app produces byte-identical `roots` arrays but different
// namespace key sets, and a synthetic theme declaring `--color-weird-1` produces a `--color-weird`
// namespace that reads informatively on 20 roots. The structure is framework-invariant; which
// namespaces exist is not.
type Table struct {
	// TailwindVersion is the engine version the table was generated against, so a mismatch is a
	// loud failure rather than a subtly wrong sort.
	TailwindVersion string
	// Descriptors is every functional root, keyed by root.
	Descriptors map[string]*Descriptor
	// Statics is every static utility's constant reading. A static takes no value, so there is no
	// model, only a fact.
	Statics map[string]Reading
	// Namespaces is every theme namespace in the design system, longest first.
	//
	// The order is the precedence the engine applies and is part of the contract: `--drop-shadow`
	// must be tested before `--shadow`, or `drop-shadow-lg` resolves through the wrong namespace.
	Namespaces []string
	// KeysByNamespace is the set of keys in each namespace, which is what decides whether a bare
	// value is a theme key at all. Sets rather than slices because the lookup only ever asks for
	// membership, and the corpus has namespaces holding 558 keys.
	KeysByNamespace map[string]map[string]bool
	// theme is the design system's resolved theme, used by the ported registration tables.
	//
	// Unexported because it is an implementation detail of Lookup: the registration tables resolve a
	// candidate's value themselves, where the descriptor rows carry an already-resolved reading.
	theme *Theme
	// PropertyOrder maps a CSS property name to its index in Tailwind's global property order,
	// which is what an arbitrary property such as `[font:inherit]` needs and nothing else does.
	PropertyOrder map[string]int
}

// Lookup returns the reading for a parsed candidate, and whether the model can answer it.
//
// This is the whole descriptor model as executable code, and it is deliberately the same five steps
// the extractor's `predictReading` runs, in the same order, because that function is what was
// measured against the engine. A restructuring that is equivalent on the corpus and different in
// some untested corner would be a right answer for a wrong reason.
//
// It returns false rather than a guess in five situations, each of which the caller must escalate
// rather than trust: a nil or unparsed candidate, a static utility the table does not know, a root
// with no descriptor, a root whose arity is per-declaration, and a root that rejects the empty value
// form.
func (table *Table) Lookup(candidate *ParsedCandidate) (Reading, bool) {
	if candidate == nil {
		return Reading{}, false
	}

	// The ported registrations answer first, and the descriptor rows are what remains.
	//
	// M6 and M7 replaced 251 of the 315 base roots with tables built from upstream's own registration
	// shapes: a property name, the theme namespaces, a shared bare-value predicate. Those roots read
	// the same way for every value they accept, so a registration expresses them exactly.
	//
	// The 64 that remain partition their reading on the resolved type of the value: `border-[3px]` is
	// a width and `border-red-500` is a colour, and no registration shape carries both. That
	// partition is the descriptor model, so those rows are not leftovers, they are the part with no
	// simpler form.
	if reading, answered := table.frameworkReading(candidate); answered {
		return reading, true
	}

	switch candidate.Kind {
	case ParsedCandidateKindArbitrary:
		// Not a utility at all: a bare CSS declaration written as a class. Its reading is one
		// declaration whose property is the text before the colon, so there is nothing to look up.
		return table.ArbitraryPropertyReading(candidate.Property), true
	case ParsedCandidateKindStatic:
		reading, found := table.Statics[candidate.Root]
		return reading, found
	case ParsedCandidateKindFunctional:
	default:
		return Reading{}, false
	}

	descriptor, found := table.Descriptors[candidate.Root]
	if !found {
		return Reading{}, false
	}
	// The 18 known exceptions. See PerDeclaration: no row of this table can answer them, and a
	// reading returned here would be wrong in exactly the place the model knows its own boundary.
	if descriptor.PerDeclaration {
		return Reading{}, false
	}

	modifierAxis := ModifierAxisFor(candidate.Modifier)
	axis := descriptor.axisFor(modifierAxis)

	if candidate.Value == nil {
		if axis.Empty == nil {
			return Reading{}, false
		}
		return *axis.Empty, true
	}

	if candidate.Value.Kind == ParsedValueKindArbitrary {
		// An arbitrary value carries its own type when the author wrote one, as in
		// `bg-[color:var(--x)]`. The engine trusts that annotation rather than inferring, and the
		// parser has already removed it from Value, so inference below never sees it.
		if candidate.Value.DataType != "" {
			return axis.readingForType(DataType(candidate.Value.DataType)), true
		}
		inferred := InferDataType(candidate.Value.Value, descriptor.TypeList)
		if inferred == "" {
			return axis.Fallback, true
		}
		return axis.readingForType(inferred), true
	}

	return table.bareReading(descriptor, axis, candidate.Value.Value, modifierAxis), true
}

// bareReading is the bare-value precedence, and the order of these five steps is the second
// refinement above.
//
// Each step exists because the one before it left a measurable population mispredicted, and the
// riskiest single assumption available here is that "bare" means "theme lookup only": bare values
// are inferred too, which is what makes `via-25%` a percentage.
func (table *Table) bareReading(
	descriptor *Descriptor,
	axis AxisReadings,
	value string,
	modifierAxis ModifierAxis,
) Reading {
	// 1. Root-defined literals, measured only on the unmodified axis.
	if modifierAxis == ModifierAbsent {
		if reading, found := descriptor.ByLiteral[value]; found {
			return reading
		}
	}

	// 2. The colour keywords, before anything can claim them.
	if reading, found := axis.ByNamespace[namespaceColorKeyword]; found && colorKeywords[value] {
		return reading
	}

	// 3. The theme namespaces, longest first, and before inference. `bold` is a `--font-weight` key
	//    that also satisfies `family-name`, so inferring first reads `font-bold` as a font family.
	for _, namespace := range table.Namespaces {
		reading, hasNamespace := axis.ByNamespace[namespace]
		if !hasNamespace {
			continue
		}
		if !table.KeysByNamespace[namespace][value] {
			continue
		}
		return reading
	}

	// 4. Inference, against the root's own ordered type list.
	if inferred := InferDataType(value, descriptor.TypeList); inferred != "" {
		if reading, found := axis.ByType[inferred]; found {
			return reading
		}
	}

	// 5. Everything else: bare numbers, fractions, root-defined keywords that read like the
	//    no-namespace reading. This is distinct from the axis fallback and differs from it on 20
	//    roots in this repository.
	if reading, found := axis.ByNamespace[namespaceNone]; found {
		return reading
	}
	return axis.Fallback
}

// ArbitraryPropertyReading is the reading for a bare CSS declaration written as a class, such as
// `[font:inherit]`.
//
// It is not a utility and has no root and no descriptor: its reading is one declaration whose
// property is the text before the colon, so there is nothing to look up. It gets a branch rather
// than a table row. An unknown property yields an empty order, which sorts last rather than first.
func (table *Table) ArbitraryPropertyReading(property string) Reading {
	index, found := table.PropertyOrder[property]
	if !found {
		return Reading{Order: nil, Count: 1}
	}
	return Reading{Order: []int{index}, Count: 1}
}

// frameworkReading answers from the ported registration tables, and reports whether it could.
//
// Negative roots are keyed on their undashed name with the negation carried as a flag, matching how
// upstream registers `-m` as `m` with `supportsNegative`. A root in neither table is not an error: it
// is one of the 64 the descriptor rows answer.
func (table *Table) frameworkReading(candidate *ParsedCandidate) (Reading, bool) {
	if candidate.Kind != ParsedCandidateKindFunctional || table.theme == nil {
		return Reading{}, false
	}

	root, negative := candidate.Root, false
	if trimmed, cut := strings.CutPrefix(root, "-"); cut {
		root, negative = trimmed, true
	}

	if utility, known := FrameworkFunctionalUtilities[root]; known {
		if negative && !utility.SupportsNegative {
			return Reading{}, false
		}
		return utility.Reading(root, candidate, table.theme, negative)
	}
	if utility, known := FrameworkMultiDeclarationUtilities[root]; known {
		if negative && !utility.SupportsNegative {
			return Reading{}, false
		}
		return utility.ReadingFor(root, candidate, table.theme, negative)
	}
	return Reading{}, false
}
