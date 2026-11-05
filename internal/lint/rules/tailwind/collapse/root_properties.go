package tailwind

import "strings"

// RootProperties is one root's property universe, split by the arm a class takes.
type RootProperties struct {
	// Colour is what a named value a colour namespace resolves declares.
	Colour []string
	// Other is what any other named value, or the bare root, declares.
	Other []string
	// Arbitrary is what an arbitrary value was seen to declare.
	Arbitrary []string
}

// armFor is the part of the universe a class can reach: a named value a colour namespace resolves, or a
// colour keyword, reaches the colour arm, since a colour-bearing root tries its colour lookup first; any
// other named value and the bare root reach the rest; an arbitrary value, whose arm its inferred type
// picks, is given all three.
func (universe RootProperties) armFor(candidate *ParsedCandidate, theme *Theme) []string {
	if candidate.Value == nil {
		return universe.Other
	}
	if candidate.Value.Kind != ParsedValueKindNamed {
		return append(append(append([]string(nil), universe.Colour...), universe.Other...), universe.Arbitrary...)
	}
	if isColourKeyword(candidate.Value.Value) {
		return universe.Colour
	}
	if _, isColour := theme.Resolve(
		candidate.Value.Value,
		true,
		colourNamespacesOf(strings.TrimPrefix(candidate.Root, "-")),
		0,
	); isColour {
		return universe.Colour
	}
	return universe.Other
}
