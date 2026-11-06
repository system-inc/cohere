package tailwind

// FrameworkFunctionalUtility is one single-declaration root.
type FrameworkFunctionalUtility struct {
	// Property is the declaration the root's handle body emits.
	Property string
	// ThemeKeys are the namespaces the root consults, in order.
	ThemeKeys []string
	// SupportsNegative registers a second root with a leading dash.
	SupportsNegative bool
	// DefaultValue is what a valueless candidate resolves to. Absent and empty differ, so the
	// presence flag is separate rather than encoded as the empty string.
	DefaultValue        string
	DefaultValuePresent bool
	// BareValue names the shared predicate this root uses, if any.
	BareValue BareValueKind
	// BareValueSuffix is appended to a bare value the predicate accepted.
	//
	// The second axis of the bare handler, and it is why 8 more roots are a table row rather than a
	// port: `delay-150` is `150ms`, `outline-offset-2` is `2px`, `zoom-50` is `50%`, and the only
	// difference between them and the pass-through roots is this string. Empty means pass through.
	BareValueSuffix string
	// StaticValues are the named values the root answers directly.
	StaticValues []FrameworkStaticValue
}

// Description converts a table row into the shape ResolveFunctionalUtilityValue takes.
//
// The conversion is here rather than in the table so the table stays data. It is also where the
// shared predicate becomes a closure, which is the one place a `BareValueKind` turns back into code.
func (utility FrameworkFunctionalUtility) Description() *FunctionalUtilityDescription {
	description := &FunctionalUtilityDescription{
		ThemeKeys:           utility.ThemeKeys,
		SupportsNegative:    utility.SupportsNegative,
		DefaultValue:        utility.DefaultValue,
		DefaultValuePresent: utility.DefaultValuePresent,
	}

	description.HandleBareValue = bareValueHandler(utility.BareValue, utility.BareValueSuffix)

	if len(utility.StaticValues) > 0 {
		description.StaticValueNames = make(map[string]bool, len(utility.StaticValues))
		for _, static := range utility.StaticValues {
			description.StaticValueNames[static.Name] = true
		}
	}
	return description
}

// Reading returns what a candidate reads against this root, and whether it reads anything.
//
// Computed rather than named. The root's ported handle body emits declarations and `PropertySort`
// reads them, which is the same two steps the engine takes, so the reading here is a consequence of
// the emission rather than a second claim about it. That is what makes the acceptance test in
// frameworkhandlers_test.go worth running: `Property` on the row and the emitter in
// frameworkhandlers.go are two independent statements of the same fact, and this method uses only
// one of them.
//
// The root name is a parameter because the emitters are keyed by root, the way the table is. It is
// not derivable from the row: two roots can declare the same property, so a reverse lookup by
// property would be ambiguous.
//
// An unported root emits nothing and reads nothing, rather than falling back to `Property`. A silent
// fallback would make a missing emitter look like a working one at every call site and leave the
// acceptance test as the only thing that could ever notice.
func (utility FrameworkFunctionalUtility) Reading(
	root string,
	candidate *ParsedCandidate,
	theme *Theme,
	negative bool,
) (Reading, bool) {
	resolved, produced := ResolveFunctionalUtilityValue(candidate, utility.Description(), theme, negative)
	if !produced {
		return Reading{}, false
	}

	nodes := utility.Emit(root, resolved)
	if len(nodes) == 0 {
		return Reading{}, false
	}

	sorted := PropertySort(nodes)
	return Reading(sorted), true
}

// staticValueEmitter is the `staticValues` half of a root's registration.
//
// Upstream a static value is a declaration list stored under a name, returned directly without the
// handle body ever running. Here it is the root's static entry rebuilt as that list, which is why
// the entry carries a property of its own: the two can differ, and inheriting the root's would be an
// assumption nothing checks.
//
// Returns nil when the name is not one of the root's static values, which is the caller's signal to
// take the ordinary path.
func (utility FrameworkFunctionalUtility) staticValueEmitter(name string) []*Node {
	for _, static := range utility.StaticValues {
		if static.Name == name {
			return []*Node{Declaration(static.Property, static.Value)}
		}
	}
	return nil
}

// Emit returns the declarations this root's handle body produces for a resolved value.
//
// The static branch runs first, matching upstream: `staticValues` is consulted before `handle` and
// returns its stored list without the handle body ever seeing the value.
//
// Returns nil for a root with no ported emitter, which is a root present in the table and absent
// from `frameworkEmitters`. Nil rather than a fabricated declaration, so a caller computing a
// reading from it gets an empty reading instead of a plausible wrong one.
func (utility FrameworkFunctionalUtility) Emit(root string, resolved ResolvedUtilityValue) []*Node {
	if resolved.IsStaticValue {
		if nodes := utility.staticValueEmitter(resolved.StaticValueName); nodes != nil {
			return nodes
		}
	}
	emitter, ported := frameworkEmitters[root]
	if !ported {
		return nil
	}
	return emitter(resolved)
}
