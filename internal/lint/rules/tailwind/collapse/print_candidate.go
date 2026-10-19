package tailwind

import "strings"

// PrintCandidate writes a parsed candidate back as Tailwind spells it, which is upstream's
// `printCandidate` (candidate.ts, 4.3.3), beside PrintVariant and over the same arbitrary-value printer.
//
// Like PrintVariant it is not the inverse of parsing, and that is what makes it a canonicalization in its
// own right: upstream's canonicalizer reports a class whenever printing its parse gives different text,
// with no pass changing anything. `bg-[var(--a)]` prints as `bg-(--a)` and `!outline` as `outline!`, and
// better-tailwindcss reports both as simplifiable (#15swfzq).
//
// The variants come out in source order. The parser stores them in the order the compiler consumes them,
// innermost first, so they are put back in front one at a time, as upstream's `parts.unshift` does.
func PrintCandidate(candidate ParsedCandidate, prefix string) string {
	parts := make([]string, 0, len(candidate.Variants)+2)
	if prefix != "" {
		parts = append(parts, prefix)
	}
	for index := len(candidate.Variants) - 1; index >= 0; index-- {
		parts = append(parts, PrintVariant(candidate.Variants[index]))
	}

	var base strings.Builder
	switch candidate.Kind {
	case ParsedCandidateKindStatic:
		base.WriteString(candidate.Root)
	case ParsedCandidateKindFunctional:
		base.WriteString(candidate.Root)
		if candidate.Value != nil {
			switch candidate.Value.Kind {
			case ParsedValueKindArbitrary:
				base.WriteString("-" + printArbitraryCandidateValue(candidate.Value))
			case ParsedValueKindNamed:
				base.WriteString("-" + candidate.Value.Value)
			}
		}
	case ParsedCandidateKindArbitrary:
		base.WriteString("[" + candidate.Property + ":" + printArbitraryValue(candidate.PropertyValue) + "]")
	}

	if candidate.Kind == ParsedCandidateKindArbitrary || candidate.Kind == ParsedCandidateKindFunctional {
		base.WriteString(printModifier(candidate.Modifier))
	}
	if candidate.Important {
		base.WriteString("!")
	}

	parts = append(parts, base.String())
	return strings.Join(parts, ":")
}

// printArbitraryCandidateValue is the bracketed half of a functional candidate with an arbitrary value:
// `[3px]`, `(--a)` for a lone `var()`, and either with its typehint, `[color:red]` or `(color:--a)`.
//
// Apart from `bracketed` only because of the typehint, which goes inside whichever brackets the value
// takes. A variant value never carries one, so the variant printer has no need of it.
func printArbitraryCandidateValue(value *ParsedValue) string {
	text := value.Value
	open, close := "[", "]"
	if isLoneVar(text) {
		text = text[len("var(") : len(text)-1]
		open, close = "(", ")"
	}
	if value.DataType != "" {
		return open + value.DataType + ":" + printArbitraryValue(text) + close
	}
	return open + printArbitraryValue(text) + close
}
