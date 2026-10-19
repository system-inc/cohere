// Constant folding of a declaration value: `calc(0.25rem * 4)` read as `1rem`, `-0px` as `0`, `1in`
// as `96px`, so two spellings of one value compare equal.
//
// Ported from `src/constant-fold-declaration.ts` at Tailwind 4.3.3. Upstream folds before it compares
// signatures, and a signature is compared as a string, so every output here is upstream's output
// byte for byte: numbers print the way JavaScript's template literals print them
// (javaScriptNumberString), and the arithmetic runs in upstream's order of operations, since
// `value * 96 / 2.54` and `value * (96 / 2.54)` can differ in the last bit and therefore in the
// printed digits.
//
// Upstream assumes a somewhat valid `calc()`, already spaced around its operators: it sees
// `calc(1 + 1)`, never `calc(1+1)`. A binary expression is a function of exactly five arguments,
// operand, separator, operator, separator, operand, and anything else is left as written.
package tailwind

import (
	"math"
	"strings"
)

// constantFoldDeclaration is upstream's function of the same name: the folded value, or input itself
// when nothing folded.
//
// Upstream's third parameter, normalizeUnit, defaults to true and every caller of this string form
// leaves it there, so it is not a parameter here; constantFoldDeclarationAst takes it, since
// upstream's signature code calls that form with false.
func constantFoldDeclaration(input string, rem *float64) string {
	folded, valueAst := constantFoldDeclarationAst(ParseValue(input), rem, true)
	if folded {
		return ValueToCss(valueAst)
	}
	return input
}

// constantFoldDeclarationAst is upstream's function of the same name, returning upstream's
// `[folded, ast]`. It rewrites nodes in place, as upstream's walk does, and returns them.
//
// rem is upstream's `number | null`: the root font size in pixels, which converts `rem` to `px`, or
// nil to leave `rem` as written. normalizeUnit false keeps every unit as written, so a zero keeps
// its unit and `1in` stays `1in`.
func constantFoldDeclarationAst(nodes []ValueNode, rem *float64, normalizeUnit bool) (bool, []ValueNode) {
	folded := false

	walkValueExit(nodes, nil, func(valueNode *ValueNode, parent *ValueNode) (ValueNode, bool) {
		// Canonicalize dimensions to their simplest form: `-0`, `+0` and `0.0` become `0`, `-0px`,
		// `+0em` and `0.0rem` become `0` or `0<unit>`, and units convert to an equivalent unit.
		// A word that is already `0` has nothing to do.
		if valueNode.Kind == ValueNodeKindWord && valueNode.Value != "0" {
			canonical, isDimension := canonicalizeDimension(valueNode.Value, rem, normalizeUnit)
			if !isDimension || canonical == valueNode.Value {
				return ValueNode{}, false
			}

			// `0<unit>` can only become `0` for a `<length>`, and only outside a function: inside
			// `calc(…)` dropping the unit can change validity either way. `calc(0px + 1rem)` would
			// go from valid to the invalid `calc(0 + 1rem)`, and `calc(0px * 1rem)` from invalid to
			// valid. So inside a function the zero keeps its unit. Upstream counts that as folded
			// even when the word was already spelled `0px`, which reprints it unchanged.
			if canonical == "0" && parent != nil && parent.Kind == ValueNodeKindFunction {
				withUnit, isDimension := canonicalizeDimension(valueNode.Value, rem, false)
				if !isDimension {
					return ValueNode{}, false
				}
				folded = true
				return valueWord(withUnit), true
			}

			folded = true
			return valueWord(canonical), true
		}

		// Fold a `calc()` or bare `(…)` of two operands and one operator.
		if valueNode.Kind != ValueNodeKindFunction || (valueNode.Value != "calc" && valueNode.Value != "") {
			return ValueNode{}, false
		}
		// The five arguments of `calc(0.25rem * 256)`: the word `0.25rem`, a separator, the word
		// `*`, a separator, and the word `256`.
		if len(valueNode.Nodes) != 5 || valueNode.Nodes[2].Kind != ValueNodeKindWord {
			return ValueNode{}, false
		}

		operator := valueNode.Nodes[2].Value
		lhsNode := valueNode.Nodes[0]
		rhsNode := valueNode.Nodes[4]
		lhs := wordDimension(lhsNode)
		rhs := wordDimension(rhsNode)

		if operator == "*" {
			// Multiplying by a unitless `0` nullifies the whole expression, `calc(0 * 100vw)` to
			// `0`. Upstream leaves a TODO here about checking that this is safe for the data types.
			if (lhs != nil && lhs.value == 0 && lhs.unit == "") || (rhs != nil && rhs.value == 0 && rhs.unit == "") {
				folded = true
				return valueWord("0"), true
			}

			// `0<unit>` times a unitless operand, on either side, is `0<unit>` inside a function and
			// `0` outside one.
			if lhs != nil && lhs.value == 0 && lhs.unit != "" && rhs != nil && rhs.unit == "" {
				folded = true
				if parent != nil && parent.Kind == ValueNodeKindFunction {
					return valueWord("0" + lhs.unit), true
				}
				return valueWord("0"), true
			}
			if rhs != nil && rhs.value == 0 && rhs.unit != "" && lhs != nil && lhs.unit == "" {
				folded = true
				if parent != nil && parent.Kind == ValueNodeKindFunction {
					return valueWord("0" + rhs.unit), true
				}
				return valueWord("0"), true
			}

			// Multiplying by a unitless `1` unwraps the other side, even an operand such as
			// `var(--foo)` that cannot fold.
			if lhs != nil && lhs.value == 1 && lhs.unit == "" {
				folded = true
				return rhsNode, true
			}
			if rhs != nil && rhs.value == 1 && rhs.unit == "" {
				folded = true
				return lhsNode, true
			}
		}

		if operator == "*" || operator == "+" {
			if replacement, reassociated := reassociateNestedExpression(valueNode, operator, lhs, rhs); reassociated {
				folded = true
				return replacement, true
			}
		}

		// Not two dimensions, so nothing to fold.
		if lhs == nil || rhs == nil {
			return ValueNode{}, false
		}

		switch operator {
		case "*":
			// The same unit, `1rem * 2rem` and `8 * 6`, or exactly one unit, `2 * 1rem` and
			// `1rem * 2`. Upstream folds two equal units too, `1rem * 2rem` to `2rem`.
			if lhs.unit == rhs.unit || (lhs.unit == "" && rhs.unit != "") || (lhs.unit != "" && rhs.unit == "") {
				folded = true
				return valueWord(javaScriptNumberString(lhs.value*rhs.value) + firstUnit(lhs.unit, rhs.unit)), true
			}

		case "+":
			// The same unit or both unitless, `1rem + 2rem` and `8 + 6`.
			if lhs.unit == rhs.unit {
				folded = true
				return valueWord(javaScriptNumberString(lhs.value+rhs.value) + lhs.unit), true
			}

		case "-":
			// The same unit or both unitless, `2rem - 1rem` and `8 - 6`.
			if lhs.unit == rhs.unit {
				folded = true
				return valueWord(javaScriptNumberString(lhs.value-rhs.value) + lhs.unit), true
			}

		case "/":
			// Never by zero, and only by a unitless divisor: `8 / 2` and `1rem / 2`.
			if rhs.value != 0 && ((lhs.unit == "" && rhs.unit == "") || (lhs.unit != "" && rhs.unit == "")) {
				computed := lhs.value / rhs.value

				// Only a result exact to two decimals folds. `100% / 3.5` is `28.571428571428573%`,
				// correct and unfriendly, so `w-[calc(100%/3.5)]` stays as written. The product is
				// converted explicitly so Go rounds it to a float64 before javaScriptRound reads
				// it, as JavaScript does, rather than fusing it into the subtraction inside.
				if javaScriptRound(float64(computed*100))/100 != computed {
					break
				}

				folded = true
				return valueWord(javaScriptNumberString(computed) + lhs.unit), true
			}
		}

		return ValueNode{}, false
	})

	return folded, nodes
}

// reassociateNestedExpression folds the known half of `x op (y op z)` when x and y are dimensions and
// z is not, the branch of upstream's exit hook for `*` and `+`:
//
//   - `calc(2rem * calc(3 * var(--foo)))` becomes `calc(6rem * var(--foo))`
//   - `calc(2rem + calc(3rem + var(--foo)))` becomes `calc(5rem + var(--foo))`
//
// lhs and rhs are the outer operands read as dimensions, and the nil one is the nested expression.
// The second result is false when the expression does not have this shape or its units do not allow
// the fold, and then upstream's hook goes on to its plain two-operand fold.
func reassociateNestedExpression(valueNode *ValueNode, operator string, lhs *dimension, rhs *dimension) (ValueNode, bool) {
	constant := lhs
	if constant == nil {
		constant = rhs
	}
	var nestedNode *ValueNode
	if lhs == nil {
		nestedNode = &valueNode.Nodes[0]
	} else if rhs == nil {
		nestedNode = &valueNode.Nodes[4]
	}

	if constant == nil || nestedNode == nil ||
		nestedNode.Kind != ValueNodeKindFunction ||
		(nestedNode.Value != "calc" && nestedNode.Value != "") ||
		len(nestedNode.Nodes) != 5 ||
		nestedNode.Nodes[2].Kind != ValueNodeKindWord ||
		nestedNode.Nodes[2].Value != operator {
		return ValueNode{}, false
	}

	nestedLhs := wordDimension(nestedNode.Nodes[0])
	nestedRhs := wordDimension(nestedNode.Nodes[4])
	known := nestedLhs
	if known == nil {
		known = nestedRhs
	}
	var unknown *ValueNode
	if nestedLhs == nil {
		unknown = &nestedNode.Nodes[0]
	} else if nestedRhs == nil {
		unknown = &nestedNode.Nodes[4]
	}
	if known == nil || unknown == nil {
		return ValueNode{}, false
	}

	var combined string
	switch operator {
	case "*":
		// Both unitless, or one unit between them. Two units multiplied is invalid: `2 * 3`,
		// `4rem * 5` and `6 * 7rem` fold, `8rem * 9rem` does not.
		if constant.unit != "" && known.unit != "" {
			return ValueNode{}, false
		}
		combined = javaScriptNumberString(constant.value*known.value) + firstUnit(constant.unit, known.unit)

	case "+":
		// The same unit, or both unitless. Mixed units are valid CSS but are not folded: `2 + 3` and
		// `8rem + 9rem` fold, `4rem + 5` and `6 + 7rem` do not.
		if constant.unit != known.unit {
			return ValueNode{}, false
		}
		combined = javaScriptNumberString(constant.value+known.value) + firstUnit(constant.unit, known.unit)
	}

	// `calc(-1 * calc(-1 * var(--foo)))` combines to a unitless 1, which unwraps entirely.
	if operator == "*" && combined == "1" {
		return *unknown, true
	}

	return ValueNode{
		Kind:  ValueNodeKindFunction,
		Value: valueNode.Value,
		Nodes: []ValueNode{valueWord(combined), valueNode.Nodes[1], valueNode.Nodes[2], valueNode.Nodes[3], *unknown},
	}, true
}

// canonicalizeDimension is upstream's function of the same name: a dimension in its simplest
// spelling, or false for a word that is not a dimension.
//
// A zero length becomes `0`, or keeps its unit when normalizeUnit is false. Otherwise, when
// normalizeUnit is true, an absolute length becomes `px`, an angle `deg`, a time `s` and a
// frequency `hz`, and `rem` becomes `px` only when the root font size is known. The unit is matched
// case-insensitively and a unit with no conversion is kept as written.
func canonicalizeDimension(input string, rem *float64, normalizeUnit bool) (string, bool) {
	parsed := parseDimension(input)
	if parsed == nil {
		return "", false
	}

	if parsed.unit == "" {
		return javaScriptNumberString(parsed.value), true
	}

	// A zero `<length>`, and only a length: `0deg`, `0%`, `0s` and `0fr` keep their units, since a
	// bare `0` is not valid where those types are expected.
	if parsed.value == 0 && IsLength(input) {
		if normalizeUnit {
			return "0", true
		}
		return "0" + parsed.unit, true
	}

	if !normalizeUnit {
		return input, true
	}

	// Each conversion is written in upstream's order of operations, left to right.
	value := parsed.value
	switch strings.ToLower(parsed.unit) {
	// <length> to px, 1in = 96px.
	case "in":
		return javaScriptNumberString(value*96) + "px", true
	case "cm":
		return javaScriptNumberString(value*96/2.54) + "px", true
	case "mm":
		return javaScriptNumberString(value*96/2.54/10) + "px", true
	case "q":
		return javaScriptNumberString(value*96/2.54/10/4) + "px", true
	case "pc":
		return javaScriptNumberString(value*96/6) + "px", true
	case "pt":
		return javaScriptNumberString(value*96/72) + "px", true
	case "rem":
		if rem == nil {
			return "", false
		}
		return javaScriptNumberString(value**rem) + "px", true

	// <angle> to deg. math.Pi becomes the float64 nearest pi, which is JavaScript's Math.PI.
	case "grad":
		return javaScriptNumberString(value*0.9) + "deg", true
	case "rad":
		return javaScriptNumberString(value*180/math.Pi) + "deg", true
	case "turn":
		return javaScriptNumberString(value*360) + "deg", true

	// <time> to s.
	case "ms":
		return javaScriptNumberString(value/1000) + "s", true

	// <frequency> to hz.
	case "khz":
		return javaScriptNumberString(value*1000) + "hz", true
	}

	return javaScriptNumberString(value) + parsed.unit, true
}

// wordDimension is upstream's `node.kind === 'word' ? dimensions.get(node.value) : null`.
func wordDimension(node ValueNode) *dimension {
	if node.Kind != ValueNodeKindWord {
		return nil
	}
	return parseDimension(node.Value)
}

// firstUnit is upstream's `left ?? right`, falling back to the empty string, over two units where an
// empty unit is `null`.
func firstUnit(left string, right string) string {
	if left != "" {
		return left
	}
	return right
}

// valueWord is upstream's `ValueParser.word(value)`.
func valueWord(value string) ValueNode {
	return ValueNode{Kind: ValueNodeKindWord, Value: value}
}
