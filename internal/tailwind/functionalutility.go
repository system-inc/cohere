// The value-resolution pipeline every framework functional utility runs.
//
// Ported from `functionalUtility` and its inner `handleFunctionalUtility` in `src/utilities.ts` at
// Tailwind 4.3.3, read at the pinned tag rather than from the minified bundle, so a disagreement
// with the engine is a finding rather than version skew.
//
// # What this is and what it deliberately is not
//
// Upstream's `functionalUtility` does two things: it turns a candidate's value into a resolved CSS
// value, and it hands that value to the root's own `handle` body which emits declarations. This file
// is the first half only. The `handle` bodies are 114 separate functions and they are #271785y.
//
// The split is not arbitrary. The resolution above is one function shared by every functional root,
// so an inexact port corrupts every reading downstream at once, in the direction of a plausible
// answer rather than an error. The handle bodies fail one root at a time and are visible against the
// engine per root. Testing them together would let a resolution defect hide behind a handler that
// happens to emit the same properties for two different values.
//
// # The order is the contract
//
// Five branches, and their order decides the answer rather than merely organising it:
//
//  1. No value at all: `defaultValue` when the descriptor defines one, otherwise a bare theme lookup.
//     A modifier with no value produces nothing, which is why `bg-red-500/50` resolves and `bg/50`
//     does not.
//  2. An arbitrary value: taken as written, carrying its typehint if the author gave one. A modifier
//     again produces nothing.
//  3. A named value: the theme first, by `theme.resolve(fraction ?? value, themeKeys)`. Theme before
//     inference is refinement 2 from Phase 0 and it is load-bearing: `bold` is a `--font-weight` key
//     that also satisfies `family-name`, so inferring first reads `font-bold` as a font family.
//  4. Fractions, when the root supports them and the theme did not answer. `w-1/2` becomes
//     `calc(1 / 2 * 100%)` without `1/2` existing as a theme value.
//  5. Bare values, negative first when the candidate was negative, then the ordinary handler, then
//     `staticValues` as a last resort.
//
// # Two details that are easy to port wrongly and produce plausible answers
//
// A bare handler that returns a value without a `/` cancels the whole utility when a modifier is
// present. Upstream spells this `if (!value?.includes('/') && candidate.modifier) return`, and it is
// what stops `grow-2/3` from reading as `grow` with a stray modifier. Dropping the test leaves those
// classes resolving to something, which sorts, which is why it would not surface as an error.
//
// A negative candidate wraps its resolved value in `calc(<value> * -1)` and routes it through
// `addWhitespaceAroundMathOperators`. Emitting `calc(<value>*-1)` instead is the same CSS and a
// different string, and since this port compares readings rather than text it would pass every
// assertion here while disagreeing with the engine anywhere the text reaches a data-type inference.
package tailwind

import (
	"strings"
)

// FunctionalUtilityDescription is upstream's `UtilityDescription`, minus the parts that emit.
//
// `handle`, `staticValues` bodies and the suggestion machinery are absent: this file resolves a
// value and never emits a declaration. `StaticValueNames` carries only the keys of `staticValues`,
// because the resolution pipeline needs to know whether a name is one, and what it expands to is the
// handle body's business.
type FunctionalUtilityDescription struct {
	// SupportsNegative registers a second root with a leading dash, and makes a resolved value be
	// negated rather than used as written.
	SupportsNegative bool
	// SupportsFractions lets `w-1/2` resolve without `1/2` being a theme key.
	SupportsFractions bool
	// ThemeKeys are the namespaces this root consults, in the order it consults them.
	ThemeKeys []string
	// DefaultValue is what a valueless candidate resolves to. Absent and empty are different: a root
	// with no default falls through to a bare theme lookup, so the presence flag is carried
	// separately rather than encoded as the empty string.
	DefaultValue        string
	DefaultValuePresent bool
	// HandleBareValue and HandleNegativeBareValue are the root's own bare-value readers. They are
	// modelled as predicates returning a value because the resolution pipeline only needs to know
	// what came back, not how the root computed it.
	HandleBareValue         func(value *ParsedValue) (string, bool)
	HandleNegativeBareValue func(value *ParsedValue) (string, bool)
	// StaticValueNames are the names `staticValues` defines, as a set.
	StaticValueNames map[string]bool
}

// ResolvedUtilityValue is what the pipeline produces for one candidate.
//
// `StaticValueName` is set when resolution ended at a `staticValues` entry rather than at a value,
// because that branch returns a declaration list directly upstream and never reaches `handle`. A
// caller emitting declarations must check it before using Value.
type ResolvedUtilityValue struct {
	Value           string
	DataType        string
	StaticValueName string
	IsStaticValue   bool
}

// ResolveFunctionalUtilityValue runs the pipeline for one candidate against one root's description.
//
// Returns false where upstream returns without producing a value, which is a class that generates no
// CSS at all rather than one that generates an empty declaration. The distinction matters to every
// caller in this port: a class with no rules has no reading, and scoring that as agreement is the
// failure mode the differential harness carries a five-state lattice to prevent.
func ResolveFunctionalUtilityValue(
	candidate *ParsedCandidate,
	description *FunctionalUtilityDescription,
	theme *Theme,
	negative bool,
) (ResolvedUtilityValue, bool) {
	if candidate == nil || description == nil || theme == nil {
		return ResolvedUtilityValue{}, false
	}

	resolved := ResolvedUtilityValue{}

	switch {
	case candidate.Value == nil:
		// A modifier with no value is not a utility. `bg/50` reaches here and produces nothing.
		if candidate.Modifier != nil {
			return ResolvedUtilityValue{}, false
		}
		if description.DefaultValuePresent {
			resolved.Value = description.DefaultValue
			break
		}
		// The bare theme lookup: `rounded` resolving through `--radius`. Upstream passes `null` as
		// the candidate value, which is why the present flag is false rather than the value empty.
		value, found := theme.Resolve("", false, description.ThemeKeys, 0)
		if !found {
			return ResolvedUtilityValue{}, false
		}
		resolved.Value = value

	case candidate.Value.Kind == ParsedValueKindArbitrary:
		if candidate.Modifier != nil {
			return ResolvedUtilityValue{}, false
		}
		resolved.Value = candidate.Value.Value
		resolved.DataType = candidate.Value.DataType

	default:
		value, found := resolveNamedValue(candidate, description, theme, negative)
		if !found.produced {
			return ResolvedUtilityValue{}, false
		}
		if found.isStaticValue {
			return ResolvedUtilityValue{StaticValueName: value, IsStaticValue: true}, true
		}
		resolved.Value = value
	}

	if resolved.Value == "" && !resolved.IsStaticValue {
		return ResolvedUtilityValue{}, false
	}

	if negative {
		resolved.Value = addWhitespaceAroundMathOperators("calc(" + resolved.Value + " * -1)")
	}
	return resolved, true
}

// namedValueOutcome distinguishes the three ways the named branch can end.
//
// A bool pair rather than a sentinel string, because a static value name and a resolved CSS value
// are both strings and confusing them would emit a declaration whose value is a key.
type namedValueOutcome struct {
	produced      bool
	isStaticValue bool
}

// resolveNamedValue is branch 3 through 5, in upstream's order.
func resolveNamedValue(
	candidate *ParsedCandidate,
	description *FunctionalUtilityDescription,
	theme *Theme,
	negative bool,
) (string, namedValueOutcome) {
	// The theme, with the fraction spelling preferred when the parser produced one. `w-1/2` asks the
	// theme for `1/2` before it asks for `1`, so a theme declaring `--width-1\/2` wins over the
	// fraction arithmetic below.
	lookup := candidate.Value.Value
	if candidate.Value.Fraction != "" {
		lookup = candidate.Value.Fraction
	}
	if value, found := theme.Resolve(lookup, true, description.ThemeKeys, 0); found {
		return value, namedValueOutcome{produced: true}
	}

	// Fractions, only when the root opted in and only for two positive integers. `w-1/2` becomes
	// `calc(1 / 2 * 100%)`; `w-a/b` and `w-1.5/2` produce nothing rather than falling through, which
	// is upstream returning rather than continuing.
	if description.SupportsFractions && candidate.Value.Fraction != "" {
		parts := segment(candidate.Value.Fraction, '/')
		if len(parts) != 2 || !isPositiveInteger(parts[0]) || !isPositiveInteger(parts[1]) {
			return "", namedValueOutcome{}
		}
		return "calc(" + parts[0] + " / " + parts[1] + " * 100%)", namedValueOutcome{produced: true}
	}

	// The negative bare handler, which returns directly rather than continuing to the ordinary one.
	if negative && description.HandleNegativeBareValue != nil {
		value, found := description.HandleNegativeBareValue(candidate.Value)
		if found && !strings.Contains(value, "/") && candidate.Modifier != nil {
			return "", namedValueOutcome{}
		}
		if found {
			return value, namedValueOutcome{produced: true}
		}
	}

	if description.HandleBareValue != nil {
		value, found := description.HandleBareValue(candidate.Value)
		if found {
			if !strings.Contains(value, "/") && candidate.Modifier != nil {
				return "", namedValueOutcome{}
			}
			return value, namedValueOutcome{produced: true}
		}
	}

	// `staticValues` last, and never for a negative candidate or one carrying a modifier.
	if !negative && candidate.Modifier == nil && description.StaticValueNames != nil {
		if description.StaticValueNames[candidate.Value.Value] {
			return candidate.Value.Value, namedValueOutcome{produced: true, isStaticValue: true}
		}
	}

	return "", namedValueOutcome{}
}

// isPositiveInteger is upstream's `isPositiveInteger` from `src/utils/infer-data-type.ts`.
//
// Digits only: a leading sign, a decimal point and an empty string are all rejected, which is what
// keeps `w-1.5/2` and `w--1/2` out of the fraction branch.
func isPositiveInteger(input string) bool {
	if input == "" {
		return false
	}
	for index := 0; index < len(input); index++ {
		if input[index] < '0' || input[index] > '9' {
			return false
		}
	}
	return true
}
