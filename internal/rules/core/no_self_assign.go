package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/rule"
	"github.com/system-inc/cohere/internal/utilities/ecmascript/property"
)

var messageSelfAssignment = rule.Message{
	Id: "selfAssignment",
	Description: "This assigns a value to itself, so it does nothing. The statement is not merely " +
		"redundant: it is almost always a typo for an assignment that was meant to happen, so the " +
		"value the author intended to write is still missing and nothing reports that it is.",
}

// NoSelfAssign flags an assignment whose two sides are the same reference.
//
//	valid:   a = b
//	valid:   a.b = a.c
//	valid:   [a, b] = [b, a]
//	invalid: a = a
//	invalid: a.b = a.b
//	invalid: a.b = a?.b
//	invalid: a.b = a["b"]
//	invalid: [a, b] = [a, b]
//	invalid: ({ a, b } = { a, b })
//
// Destructuring is most of the work and the reason this is not a two-line rule. `[a, b] = [b, a]`
// is a swap and must stay silent, so the comparison is positional and elementwise rather than
// set-based, and a spread on either side truncates it because everything after a spread sits at an
// index nobody can name.
//
// The `props` option is on in this tree, so the member-expression half is implemented rather than
// skipped. That half needs a real reference comparison rather than token equality, because ESLint
// treats `a.b`, `a?.b`, and `a["b"]` as the same reference and their tokens differ. Shipping the
// syntactic half alone would agree with the gate on every fixture anyone would think to write and
// silently miss the shape the option exists for, which is the mistake use-isnan shipped with.
//
// Only the four assignment operators that write the whole value are considered: `=`, `&&=`, `||=`,
// and `??=`. `a += a` doubles a, which is a different statement and not this rule's business.
var NoSelfAssign = rule.Rule{
	Name: "no-self-assign",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		return rule.Listeners{
			ast.KindBinaryExpression: func(node *ast.Node) {
				binary := node.AsBinaryExpression()
				if binary.OperatorToken == nil {
					return
				}
				switch binary.OperatorToken.Kind {
				case ast.KindEqualsToken,
					ast.KindAmpersandAmpersandEqualsToken,
					ast.KindBarBarEqualsToken,
					ast.KindQuestionQuestionEqualsToken:
				default:
					return
				}

				reportSelfAssignments(ctx, binary.Left, binary.Right)
			},
		}
	},
}

// reportSelfAssignments walks a matched pair of assignment target and value.
//
// Recursive because a destructuring assignment is a tree of targets against a tree of values, and a
// self-assignment can sit at any depth: `[a, [b]] = [a, [b]]` is two of them.
func reportSelfAssignments(ctx rule.Context, left *ast.Node, right *ast.Node) {
	if left == nil || right == nil {
		return
	}
	left = ast.SkipParentheses(left)
	right = ast.SkipParentheses(right)
	if left == nil || right == nil {
		return
	}

	switch {
	case left.Kind == ast.KindIdentifier && right.Kind == ast.KindIdentifier:
		if left.Text() == right.Text() {
			ctx.ReportNode(right, messageSelfAssignment)
		}

	case left.Kind == ast.KindArrayLiteralExpression && right.Kind == ast.KindArrayLiteralExpression:
		reportArraySelfAssignments(ctx, left, right)

	case left.Kind == ast.KindObjectLiteralExpression && right.Kind == ast.KindObjectLiteralExpression:
		reportObjectSelfAssignments(ctx, left, right)

	case left.Kind == ast.KindSpreadElement && right.Kind == ast.KindSpreadElement:
		reportSelfAssignments(ctx, left.AsSpreadElement().Expression, right.AsSpreadElement().Expression)

	case ast.IsAccessExpression(left) && ast.IsAccessExpression(right):
		if isSameReference(left, right) {
			ctx.ReportNode(right, messageSelfAssignment)
		}

	case left.Kind == ast.KindThisKeyword && right.Kind == ast.KindThisKeyword:
		ctx.ReportNode(right, messageSelfAssignment)
	}
}

// reportArraySelfAssignments compares array destructuring positionally.
//
// Positional rather than by membership, which is what keeps `[a, b] = [b, a]` silent: a swap
// assigns each name a different value and is the reason anyone writes this shape at all.
func reportArraySelfAssignments(ctx rule.Context, left *ast.Node, right *ast.Node) {
	leftElements := left.AsArrayLiteralExpression().Elements.Nodes
	rightElements := right.AsArrayLiteralExpression().Elements.Nodes

	end := min(len(leftElements), len(rightElements))
	for index := range end {
		leftElement := ast.SkipParentheses(leftElements[index])
		rightElement := ast.SkipParentheses(rightElements[index])

		// A rest target absorbs everything remaining, so it only matches a spread that is also
		// last. `[...a] = [...a, 1]` assigns a a longer array than it had.
		if leftElement != nil && leftElement.Kind == ast.KindSpreadElement && index < len(rightElements)-1 {
			return
		}

		reportSelfAssignments(ctx, leftElement, rightElement)

		// After a spread on the value side, every later index is unknown, so no further position
		// can be claimed to match.
		if rightElement != nil && rightElement.Kind == ast.KindSpreadElement {
			return
		}
	}
}

// reportObjectSelfAssignments compares object destructuring by property name.
//
// By name rather than positionally, because object properties are unordered and `{ a, b } = { b, a }`
// really is a self-assignment of both.
func reportObjectSelfAssignments(ctx rule.Context, left *ast.Node, right *ast.Node) {
	leftProperties := left.AsObjectLiteralExpression().Properties.Nodes
	rightProperties := right.AsObjectLiteralExpression().Properties.Nodes
	if len(rightProperties) == 0 {
		return
	}

	// Everything up to and including the last spread can be overwritten by it, so only properties
	// after it are safe to claim. `({ a } = { ...b, a })` is a self-assignment of a; `({ a } = { a, ...b })`
	// is not, since b may carry its own a.
	start := 0
	for index := len(rightProperties) - 1; index >= 0; index-- {
		if rightProperties[index].Kind == ast.KindSpreadAssignment {
			start = index + 1
			break
		}
	}

	for _, leftProperty := range leftProperties {
		leftName, leftOk := propertyName(leftProperty)
		if !leftOk {
			continue
		}
		for _, rightProperty := range rightProperties[start:] {
			// A method or accessor is a definition rather than a value read back, so it can never
			// be the same reference as the thing it is assigned to. Two things exclude them and
			// either one alone is sufficient: propertyName answers false for any kind other than a
			// property or shorthand assignment, and propertyValue answers nil for the same set.
			//
			// That redundancy is worth naming, because it is invisible to the sweep. Mutating
			// either guard alone leaves the fixtures green, since the other still stops the case;
			// mutating both together kills 3. So a single-mutation sweep reports two unmeasured
			// lines where the truth is one measured behavior held by two lines. The distinction
			// matters: "no fixture covers this" would have sent me writing a fixture that cannot
			// exist, and "this line does nothing" would have sent me deleting a guard that does.
			rightName, rightOk := propertyName(rightProperty)
			if !rightOk || leftName != rightName {
				continue
			}
			reportSelfAssignments(ctx, propertyValue(leftProperty), propertyValue(rightProperty))
		}
	}
}

// propertyName reads a property's static name, reporting false when it has none.
//
// A computed key is only static when it is a literal: `{ [k]: v }` names a property nobody can
// know at lint time, and treating it as matching would report an assignment that may not be one.
func propertyName(member *ast.Node) (string, bool) {
	var name *ast.Node
	switch member.Kind {
	case ast.KindPropertyAssignment:
		name = member.AsPropertyAssignment().Name()
	case ast.KindShorthandPropertyAssignment:
		name = member.AsShorthandPropertyAssignment().Name()
	default:
		return "", false
	}
	if name == nil {
		return "", false
	}

	// Templates are absent from the accept set deliberately: a bare `+"`"+`{ `+"`"+`k`+"`"+`: 1 }`+"`"+` is not valid
	// JavaScript, so a template can only reach a key through brackets, and this rule's computed arm
	// only ever met the string and numeric spellings.
	return property.Name(name, property.Named|property.Quoted|property.Numeric|property.Computed)
}

// propertyValue reads the value a property assigns, which for shorthand is the name itself.
func propertyValue(property *ast.Node) *ast.Node {
	switch property.Kind {
	case ast.KindPropertyAssignment:
		return property.AsPropertyAssignment().Initializer
	case ast.KindShorthandPropertyAssignment:
		return property.AsShorthandPropertyAssignment().Name()
	}
	return nil
}

// isSameReference reports whether two expressions name the same place in memory.
//
// Not token equality, and the difference is the whole reason the props option needs its own
// comparison. ESLint treats these as the same reference:
//
//	a.b  and  a?.b       optional chaining changes when the read happens, not what is read
//	x.y  and  x["y"]     a static computed key is the same property
//
// Their tokens differ, so a rule built on the token oracle would agree with the gate on every
// fixture anyone would think to write and miss exactly the shapes this option exists for.
//
// Deliberately conservative everywhere else. A non-static computed key (`a[i]`) returns false even
// against itself, since two reads of `a[i]` are the same reference only if i has not changed, and
// nothing here can know that. Calls are false for the same reason: `f().x = f().x` calls f twice.
func isSameReference(left *ast.Node, right *ast.Node) bool {
	left = ast.SkipParentheses(left)
	right = ast.SkipParentheses(right)
	if left == nil || right == nil {
		return false
	}

	if ast.IsAccessExpression(left) && ast.IsAccessExpression(right) {
		// `AccessedName` answers "b" for both `a.b` and `a['b']`, which is what makes the two
		// spellings compare as one reference, and answers nothing for `a[i]`, so a comparison
		// involving a variable subscript is false rather than optimistic.
		leftName, leftStatic := property.AccessedName(left, property.Static)
		rightName, rightStatic := property.AccessedName(right, property.Static)
		if !leftStatic || !rightStatic || leftName != rightName {
			return false
		}
		return isSameReference(accessedObject(left), accessedObject(right))
	}

	if left.Kind != right.Kind {
		return false
	}

	switch left.Kind {
	case ast.KindIdentifier, ast.KindPrivateIdentifier:
		return left.Text() == right.Text()
	case ast.KindThisKeyword, ast.KindSuperKeyword:
		return true
	case ast.KindStringLiteral, ast.KindNumericLiteral:
		return left.Text() == right.Text()
	}

	return false
}

// accessedObject reads the receiver of an access expression.
func accessedObject(node *ast.Node) *ast.Node {
	switch node.Kind {
	case ast.KindPropertyAccessExpression:
		return node.AsPropertyAccessExpression().Expression
	case ast.KindElementAccessExpression:
		return node.AsElementAccessExpression().Expression
	}
	return nil
}
