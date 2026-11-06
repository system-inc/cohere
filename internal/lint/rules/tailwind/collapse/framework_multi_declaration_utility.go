package tailwind

// FrameworkMultiDeclarationUtility is one root whose reading was measured rather than derived.
//
// The value axes (theme keys, bare-value predicate, default) are here for the same reason they are in
// FrameworkFunctionalUtility: they decide whether a class resolves at all.
//
// Reading no longer decides what a candidate reads. The ported handle bodies in
// frameworkmultihandlers.go emit declarations and `readingOf` runs PropertySort over them, the same
// two steps the engine takes. Reading stays because it is the independent measurement the acceptance
// test compares those emitters against: corrupting one row fails that test while leaving the
// differential at full agreement, which is what makes the two sides genuinely separate claims.
type FrameworkMultiDeclarationUtility struct {
	// Reading is what this root reads for any value that resolves, as measured against the engine.
	//
	// Read by the acceptance test rather than by the resolution path. See the type's doc comment.
	Reading Reading
	// LiteralReadings are the root-defined keywords the handler answers directly.
	//
	// Most read exactly as their root does and are carried so the resolution path knows the keyword
	// is answered without a theme lookup. Four read differently: `ease-initial` reads `[]#1` where
	// every other `ease-*` reads `[354]#2`, and `divide-none`, `transition-none` and `translate-none`
	// each emit a shorter list than their root's ordinary path. Those four have an entry in
	// frameworkMultiLiteralEmitters; the rest deliberately do not.
	LiteralReadings map[string]Reading

	ThemeKeys        []string
	SupportsNegative bool
	// SupportsFractions lets `basis-1/2` resolve without `1/2` being a theme key.
	SupportsFractions bool
	// RefusesEmptyValue marks a root that compiles to nothing when written with no value.
	//
	// Measured per root rather than derived from the other fields: `blur` has no default value
	// and reads bare through a theme lookup, `gap` has no default and reads nothing. See ReadingFor.
	RefusesEmptyValue bool
	// RefusesArbitraryValue marks a root that takes only named values.
	//
	// `max-w-screen` is the one: it reads `max-w-screen-sm` through the `--breakpoint` namespace and
	// compiles `max-w-screen-[3px]` to nothing, because the registration accepts a theme key rather
	// than a value. Without this the table invents a reading for every arbitrary form.
	RefusesArbitraryValue bool
	// AcceptsColorKeywords marks a root that answers `current`, `inherit` and `transparent`.
	AcceptsColorKeywords bool
	DefaultValue         string
	DefaultValuePresent  bool
	BareValue            BareValueKind
	BareValueSuffix      string
}

// Description converts a row into the shape ResolveFunctionalUtilityValue takes.
func (utility FrameworkMultiDeclarationUtility) Description() *FunctionalUtilityDescription {
	description := &FunctionalUtilityDescription{
		ThemeKeys:           utility.ThemeKeys,
		SupportsNegative:    utility.SupportsNegative,
		SupportsFractions:   utility.SupportsFractions,
		DefaultValue:        utility.DefaultValue,
		DefaultValuePresent: utility.DefaultValuePresent,
	}
	// Shared with FrameworkFunctionalUtility rather than duplicated, so the two tables cannot drift
	// about what a BareValueKind means. An inline copy here is what left `aspect` with a nil handler
	// after BareValueFraction was added: the kind existed, the table row named it, and this function
	// had never heard of it.
	description.HandleBareValue = bareValueHandler(utility.BareValue, utility.BareValueSuffix)
	return description
}

// Reading returns what a candidate reads against this root, and whether it reads anything.
//
// The literal check runs before resolution, matching upstream: a root-defined keyword is answered by
// the handler directly and never reaches the theme. Checking after would let a repository declaring
// `--ease-initial` shadow the literal, which the engine does not allow.
func (utility FrameworkMultiDeclarationUtility) ReadingFor(
	root string,
	candidate *ParsedCandidate,
	theme *Theme,
	negative bool,
) (Reading, bool) {
	if candidate == nil {
		return Reading{}, false
	}
	if candidate.Value != nil && candidate.Value.Kind == ParsedValueKindNamed {
		if _, found := utility.LiteralReadings[candidate.Value.Value]; found {
			return utility.readingOf(root, candidate.Value.Value, ResolvedUtilityValue{})
		}
		// The universal colour keywords, which are in no theme namespace and infer as nothing.
		//
		// `accent-current` compiles and `current` is absent from `--color`: they are Tailwind's own
		// built-ins, resolved by the colour path before the theme is consulted at all. A colour root
		// therefore answers them at its ordinary reading, and without this the theme lookup fails and
		// the table declines eighteen classes the engine reads. Same bucket `bareReading` carries in
		// descriptor.go, for the same reason.
		if utility.AcceptsColorKeywords && colorKeywords[candidate.Value.Value] {
			return utility.readingOf(root, "", ResolvedUtilityValue{})
		}
	}
	// A valueless candidate is a per-root fact, not a consequence of the other fields.
	//
	// `blur` and `transition` read bare, through a theme lookup and a default respectively. `gap`,
	// `basis`, `size`, `leading`, `indent`, `space-x` and the border spacings compile to no CSS at
	// all: upstream's `spacingUtility` passes `defaultValue: null`, which is not the same as omitting
	// it, and the `--spacing` namespace answers a null candidate value, so the theme lookup succeeds
	// where the engine refuses.
	//
	// A first attempt keyed this on `DefaultValuePresent` and broke `blur`, which has no default and
	// answers anyway. So the flag is measured per root against the engine rather than derived from
	// the shape, which is the same lesson wave 2b's readings taught.
	if candidate.Value == nil && utility.RefusesEmptyValue {
		return Reading{}, false
	}
	if utility.RefusesArbitraryValue && candidate.Value != nil && candidate.Value.Kind == ParsedValueKindArbitrary {
		return Reading{}, false
	}
	resolved, produced := ResolveFunctionalUtilityValue(candidate, utility.Description(), theme, negative)
	if !produced {
		return Reading{}, false
	}
	return utility.readingOf(root, "", resolved)
}

// readingOf runs the root's ported handle body and sorts what it emits.
//
// The one place `Reading` is no longer read on the ordinary path. It stays on the row because it is
// the independent measurement frameworkmultihandlers_test.go compares the emitters against, and two
// statements of one fact are worth carrying only while something forces them to agree.
//
// An unported root reads nothing rather than falling back to the stored `Reading`, for the reason
// the single-declaration Emit gives: a fallback makes a missing emitter indistinguishable from a
// working one everywhere except the acceptance test.
func (utility FrameworkMultiDeclarationUtility) readingOf(
	root, literal string,
	resolved ResolvedUtilityValue,
) (Reading, bool) {
	nodes := utility.Emit(root, literal, resolved)
	if len(nodes) == 0 {
		return Reading{}, false
	}
	sorted := PropertySort(nodes)
	return Reading(sorted), true
}

// Emit returns the declarations this root's handle body produces for a resolved value.
//
// The literal branch runs first and on the candidate's own value rather than on the resolved one,
// matching ReadingFor and matching upstream: a root-defined keyword is answered by the handler
// directly and never reaches the theme. Checking after resolution would let a repository declaring
// `--ease-initial` shadow the literal, which the engine does not allow.
//
// Returns nil for a root with no ported emitter, for the same reason the single-declaration Emit
// does: a fallback would leave the acceptance test comparing a table against itself.
func (utility FrameworkMultiDeclarationUtility) Emit(
	root string,
	literal string,
	resolved ResolvedUtilityValue,
) []*Node {
	if literal != "" {
		if byLiteral, found := frameworkMultiLiteralEmitters[root]; found {
			if emitter, found := byLiteral[literal]; found {
				return emitter(resolved)
			}
		}
	}
	emitter, ported := frameworkMultiEmitters[root]
	if !ported {
		return nil
	}
	return emitter(resolved)
}
