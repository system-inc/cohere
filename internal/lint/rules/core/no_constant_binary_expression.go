package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

var messageConstantShortCircuit = rule.Message{
	Id: "constantShortCircuit",
	Description: "The left side of this short-circuit always has the same truthiness or " +
		"nullishness, so the operator never chooses: one side always wins and the other is dead. " +
		"The usual cause is a guard written against the wrong thing, such as testing an object " +
		"that is always present rather than the field inside it.",
}

var messageConstantBinaryOperand = rule.Message{
	Id: "constantBinaryOperand",
	Description: "This comparison always gives the same answer, because one side can never equal " +
		"the other. Nothing reports it: the code compiles and the branch it guards simply never " +
		"changes. It is usually a comparison against the wrong thing, such as testing an array or " +
		"a function for null when neither can be.",
}

var messageAlwaysNew = rule.Message{
	Id: "alwaysNew",
	Description: "This side of the comparison builds a fresh object every time it runs, and " +
		"strict equality on objects compares identity rather than contents, so the comparison is " +
		"always false. The intent is nearly always to compare contents, which needs an explicit " +
		"check rather than an operator.",
}

var messageBothAlwaysNew = rule.Message{
	Id: "bothAlwaysNew",
	Description: "Both sides build a fresh object, and two fresh objects are never the same " +
		"object, so this comparison is always false even under loose equality: two objects are " +
		"compared by identity whichever equality operator is used.",
}

var messageConstantRelationalComparison = rule.Message{
	Id: "constantRelationalComparison",
	Description: "Both sides of this comparison are literals, so the answer is fixed and could be " +
		"written as true or false. A comparison that decides nothing is usually a leftover from " +
		"code that used to compare something real.",
}

// NoConstantBinaryExpressionOptions configures the relational arm.
type NoConstantBinaryExpressionOptions struct {
	// CheckRelationalComparisons turns on reporting `1 < 2`.
	//
	// Off in this tree, matching ESLint's default. Implemented anyway rather than skipped, because
	// a rule that silently lacks an option is indistinguishable from one whose option is off, and
	// the difference only surfaces when someone turns it on.
	CheckRelationalComparisons bool `json:"checkRelationalComparisons"`
}

// NoConstantBinaryExpression flags comparisons and short-circuits whose result cannot vary.
//
//	valid:   a ?? b
//	valid:   a === b
//	invalid: [] ?? a                  an array is never nullish, so ?? never chooses
//	invalid: a === []                 nothing equals a fresh array
//	invalid: [] == true               an empty array's boolean comparison is fixed
//	invalid: {} === {}                two fresh objects are never the same object
//
// The judgment is per operator and per side rather than one notion of constant, which is what makes
// this rule long. `??` asks whether the left side's nullishness is fixed, `&&` asks whether its
// truthiness is, `==` against a boolean asks whether the other side's loose coercion is fixed, and
// `===` against an object asks whether either side is freshly built. Four different questions, and
// collapsing them would report correct code.
//
// ESLint asks its scope analysis whether `undefined`, `Boolean`, `String`, and the built-in
// constructors are the globals rather than local shadows, and so does this, through the checker:
// a name declared in no source file of the program is the global. `function foo(undefined) {
// undefined ?? bar }` and a local `function Boolean(n)` are ordinary values, and reading the names
// as the globals reported 9 of ESLint's corpus rows that it pins as clean (#jjfa7qb).
//
// The relational arm is off in this tree, so it cannot be validated against the differential. It is
// implemented because a missing option and a disabled one look identical until someone enables it.
var NoConstantBinaryExpression = rule.Rule[NoConstantBinaryExpressionOptions]{
	Name: "no-constant-binary-expression",

	// Whether `undefined`, `Boolean` and the built-in constructors are the globals
	NeedsTypeChecker: true,

	Run: func(ruleContext rule.Context, options rule.Given[NoConstantBinaryExpressionOptions]) rule.Listeners {
		judge := constantJudge{isGlobal: func(identifier *ast.Node) bool {
			return ruleContext.TypeChecker == nil ||
				!rule.IsDeclaredInASourceFile(ruleContext.TypeChecker.GetSymbolAtLocation(identifier))
		}}
		checkRelationalComparisons := false
		if parsed, ok := options.Read(); ok {
			checkRelationalComparisons = parsed.CheckRelationalComparisons
		}

		return rule.Listeners{
			ast.KindBinaryExpression: func(node *ast.Node) {
				binary := node.AsBinaryExpression()
				if binary.OperatorToken == nil {
					return
				}
				left := ast.SkipParentheses(binary.Left)
				right := ast.SkipParentheses(binary.Right)
				if left == nil || right == nil {
					return
				}

				switch binary.OperatorToken.Kind {
				case ast.KindAmpersandAmpersandToken, ast.KindBarBarToken:
					// The left side alone decides whether the operator ever chooses.
					if isConstantExpression(left, true) {
						ruleContext.ReportNode(left, messageConstantShortCircuit)
					}

				case ast.KindQuestionQuestionToken:
					if judge.hasConstantNullishness(left, false) {
						ruleContext.ReportNode(left, messageConstantShortCircuit)
					}

				case ast.KindEqualsEqualsEqualsToken, ast.KindExclamationEqualsEqualsToken:
					if operand := judge.constantComparisonOperand(left, right, true); operand != nil {
						ruleContext.ReportNode(operand, messageConstantBinaryOperand)
					} else if operand := judge.constantComparisonOperand(right, left, true); operand != nil {
						ruleContext.ReportNode(operand, messageConstantBinaryOperand)
					} else if judge.isAlwaysNew(left) {
						ruleContext.ReportNode(left, messageAlwaysNew)
					} else if judge.isAlwaysNew(right) {
						ruleContext.ReportNode(right, messageAlwaysNew)
					}

				case ast.KindEqualsEqualsToken, ast.KindExclamationEqualsToken:
					if operand := judge.constantComparisonOperand(left, right, false); operand != nil {
						ruleContext.ReportNode(operand, messageConstantBinaryOperand)
					} else if operand := judge.constantComparisonOperand(right, left, false); operand != nil {
						ruleContext.ReportNode(operand, messageConstantBinaryOperand)
					} else if judge.isAlwaysNew(left) && judge.isAlwaysNew(right) {
						// Both sides only. Under loose equality a single fresh object can still
						// equal a primitive through coercion, so one is not enough.
						ruleContext.ReportNode(left, messageBothAlwaysNew)
					}

				case ast.KindLessThanToken, ast.KindLessThanEqualsToken,
					ast.KindGreaterThanToken, ast.KindGreaterThanEqualsToken:
					if checkRelationalComparisons && judge.isStaticLiteral(left) && judge.isStaticLiteral(right) {
						ruleContext.ReportNode(node, messageConstantRelationalComparison)
					}
				}
			},
		}
	},
}

// isNumericOrStringBinaryOperator reports the operators that always yield a number or a string.
//
// Deliberately excludes the comparison and logical operators, which can yield a boolean, and `in`
// and `instanceof`, which always do.
func isNumericOrStringBinaryOperator(operator ast.Kind) bool {
	switch operator {
	case ast.KindMinusToken, ast.KindPlusToken, ast.KindSlashToken, ast.KindAsteriskToken,
		ast.KindPercentToken, ast.KindBarToken, ast.KindCaretToken, ast.KindAmpersandToken,
		ast.KindAsteriskAsteriskToken, ast.KindLessThanLessThanToken,
		ast.KindGreaterThanGreaterThanToken, ast.KindGreaterThanGreaterThanGreaterThanToken:
		return true
	}
	return false
}

// ecmaScriptGlobalConstructors are the built-ins whose `new` always yields a fresh object.
//
// The boxed primitives are the ones worth catching: `new Number(1) === 1` is false, which surprises
// people, and it is exactly the mistake this arm exists for.
var ecmaScriptGlobalConstructors = map[string]bool{
	"Array": true, "ArrayBuffer": true, "BigInt64Array": true, "BigUint64Array": true,
	"Boolean": true, "DataView": true, "Date": true, "Error": true, "EvalError": true,
	"Float32Array": true, "Float64Array": true, "Function": true, "Int8Array": true,
	"Int16Array": true, "Int32Array": true, "Map": true, "Number": true, "Object": true,
	"Promise": true, "Proxy": true, "RangeError": true, "ReferenceError": true, "RegExp": true,
	"Set": true, "String": true, "SyntaxError": true, "TypeError": true, "URIError": true,
	"Uint8Array": true, "Uint8ClampedArray": true, "Uint16Array": true, "Uint32Array": true,
	"WeakMap": true, "WeakRef": true, "WeakSet": true,
}

func isEcmaScriptGlobalConstructor(name string) bool {
	return ecmaScriptGlobalConstructors[name]
}
