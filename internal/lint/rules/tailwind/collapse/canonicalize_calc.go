// Operand order for commutative calc expressions: `calc(1rem + var(--foo))` and
// `calc(var(--foo) + 1rem)` are one value, so both are written one way before two values are
// compared.
//
// Ported from `src/canonicalize-calc-expressions.ts` at Tailwind 4.3.3. Like the constant folder, it
// assumes a `calc()` already spaced around its operators, and reads a binary expression as a
// function of exactly five arguments.
package tailwind

import "cmp"

// canonicalizeCalcExpressions is upstream's function of the same name: the reordered value, or input
// itself when nothing moved.
func canonicalizeCalcExpressions(input string) string {
	canonicalized, valueAst := canonicalizeCalcExpressionsAst(ParseValue(input))
	if canonicalized {
		return ValueToCss(valueAst)
	}
	return input
}

// canonicalizeCalcExpressionsAst is upstream's function of the same name, returning upstream's
// `[canonicalized, ast]`. It rewrites nodes in place, as upstream's walk does, and returns them.
//
// Only `*` and `+` in a `calc()` or a bare `(…)` are reordered, innermost first, so
// `calc(2rem * calc(3px * var(--foo)))` becomes `calc(calc(var(--foo) * 3px) * 2rem)`. `-` and `/`
// are not commutative and are left as written.
func canonicalizeCalcExpressionsAst(nodes []ValueNode) (bool, []ValueNode) {
	canonicalized := false

	walkValueExit(nodes, nil, func(valueNode *ValueNode, _ *ValueNode) (ValueNode, bool) {
		if valueNode.Kind != ValueNodeKindFunction {
			return ValueNode{}, false
		}
		if valueNode.Value != "calc" && valueNode.Value != "" {
			return ValueNode{}, false
		}
		if len(valueNode.Nodes) != 5 || valueNode.Nodes[2].Kind != ValueNodeKindWord {
			return ValueNode{}, false
		}
		if valueNode.Nodes[2].Value != "*" && valueNode.Nodes[2].Value != "+" {
			return ValueNode{}, false
		}

		if !shouldSwapCalcOperands(valueNode.Nodes[0], valueNode.Nodes[4]) {
			return ValueNode{}, false
		}

		canonicalized = true
		// The operands trade places and the two separators and the operator stay where they are.
		return ValueNode{
			Kind:  ValueNodeKindFunction,
			Value: valueNode.Value,
			Nodes: []ValueNode{valueNode.Nodes[4], valueNode.Nodes[1], valueNode.Nodes[2], valueNode.Nodes[3], valueNode.Nodes[0]},
		}, true
	})

	return canonicalized, nodes
}

// shouldSwapCalcOperands is upstream's shouldSwap: whether rhs belongs before lhs.
//
// Anything that is not a dimension comes first, then dimensions with a unit, then unitless numbers.
// Two dimensions of the same kind sort by value, then by unit, and two operands that are not
// dimensions sort by their printed text. So `1rem + var(--foo)` becomes `var(--foo) + 1rem`,
// `1 * 1rem` becomes `1rem * 1`, and `10 + 2` becomes `2 + 10`.
//
// The text comparisons are upstream's `localeCompare`, which collates rather than comparing code
// units: see javaScriptLocaleCompare for what that means here and for the one case this declines.
func shouldSwapCalcOperands(lhs ValueNode, rhs ValueNode) bool {
	lhsDimension := wordDimension(lhs)
	rhsDimension := wordDimension(rhs)

	if lhsDimension != nil && rhsDimension == nil {
		return true
	}
	if lhsDimension == nil && rhsDimension != nil {
		return false
	}

	if lhsDimension != nil && rhsDimension != nil {
		// A unit-bearing value sorts ahead of a unitless number, so `1rem` comes before `1`.
		if lhsDimension.unit == "" && rhsDimension.unit != "" {
			return true
		}
		if lhsDimension.unit != "" && rhsDimension.unit == "" {
			return false
		}

		if lhsDimension.value != rhsDimension.value {
			return lhsDimension.value-rhsDimension.value > 0
		}

		if lhsDimension.unit != rhsDimension.unit {
			order, collated := javaScriptLocaleCompare(lhsDimension.unit, rhsDimension.unit)
			return collated && order > 0
		}
	}

	order, collated := javaScriptLocaleCompare(ValueToCss([]ValueNode{lhs}), ValueToCss([]ValueNode{rhs}))
	return collated && order > 0
}

// collationPrimaryOrder is the printable ASCII characters and the whitespace controls in the order
// ICU's root collation sorts them, measured from Node's `localeCompare` (Node 24, locale `en-US`),
// with each letter standing for both of its cases. It is not code point order: punctuation comes
// before digits and digits before letters, `_` sorts before `-`, and `a` sorts before `B`.
const collationPrimaryOrder = "\t\n\v\f\r _-,;:!?.'\"()[]{}@*/\\&#%`^+<=>|~$0123456789abcdefghijklmnopqrstuvwxyz"

// collationPrimaryWeights maps each ASCII byte to its position in collationPrimaryOrder plus one, an
// uppercase letter to its lowercase letter's, and leaves zero for the controls ICU ignores entirely.
var collationPrimaryWeights = func() [128]int {
	var weights [128]int
	for position := range len(collationPrimaryOrder) {
		character := collationPrimaryOrder[position]
		weights[character] = position + 1
		if character >= 'a' && character <= 'z' {
			weights[character-'a'+'A'] = position + 1
		}
	}
	return weights
}()

// javaScriptLocaleCompare is the sign of `left.localeCompare(right)` under the default collator V8
// uses, ICU's root collation, for ASCII text. The second result is false when either string holds a
// byte outside ASCII.
//
// ICU compares in levels. First the whole strings by primary weight, the order in
// collationPrimaryOrder with case ignored and the other controls skipped as if absent, a shorter
// prefix sorting first. Only on a tie does it compare case, lowercase before uppercase, position by
// position. Past that the strings are equal, so `a\u0001` and `a` compare as 0. Every ASCII rule
// here was checked against Node on every single character and on mixed strings.
//
// Outside ASCII the order needs the Unicode collation tables, which this package does not carry.
// The caller then keeps the operands as written, so a pair upstream would reorder is left as is and
// its two spellings compare unequal, rather than reordering a pair upstream would keep.
func javaScriptLocaleCompare(left string, right string) (int, bool) {
	for index := range len(left) {
		if left[index] >= 0x80 {
			return 0, false
		}
	}
	for index := range len(right) {
		if right[index] >= 0x80 {
			return 0, false
		}
	}

	leftWeights := collationWeights(left)
	rightWeights := collationWeights(right)
	for index := 0; index < len(leftWeights) && index < len(rightWeights); index++ {
		if leftWeights[index].primary != rightWeights[index].primary {
			return cmp.Compare(leftWeights[index].primary, rightWeights[index].primary), true
		}
	}
	if len(leftWeights) != len(rightWeights) {
		return cmp.Compare(len(leftWeights), len(rightWeights)), true
	}
	for index := range leftWeights {
		if leftWeights[index].uppercase != rightWeights[index].uppercase {
			if leftWeights[index].uppercase {
				return 1, true
			}
			return -1, true
		}
	}
	return 0, true
}

// collationWeight is one character's weights at the two levels ASCII text differs on.
type collationWeight struct {
	primary   int
	uppercase bool
}

// collationWeights is the weight of each character of an ASCII string that ICU does not ignore.
func collationWeights(text string) []collationWeight {
	weights := make([]collationWeight, 0, len(text))
	for index := range len(text) {
		character := text[index]
		if collationPrimaryWeights[character] == 0 {
			continue
		}
		weights = append(weights, collationWeight{
			primary:   collationPrimaryWeights[character],
			uppercase: character >= 'A' && character <= 'Z',
		})
	}
	return weights
}
