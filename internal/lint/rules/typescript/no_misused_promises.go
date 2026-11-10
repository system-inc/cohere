package typescript

import (
	"fmt"
	"slices"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/rule"
)

func buildConditionalMessage() rule.Message {
	return rule.Message{
		Id:          "conditional",
		Description: "Expected non-Promise value in a boolean conditional.",
	}
}
func buildPredicateMessage() rule.Message {
	return rule.Message{
		Id:          "predicate",
		Description: "Expected a non-Promise value to be returned.",
	}
}
func buildSpreadMessage() rule.Message {
	return rule.Message{
		Id:          "spread",
		Description: "Expected a non-Promise value to be spreaded in an object.",
	}
}
func buildVoidReturnArgumentMessage() rule.Message {
	return rule.Message{
		Id:          "voidReturnArgument",
		Description: "Promise returned in function argument where a void return was expected.",
	}
}
func buildVoidReturnAttributeMessage() rule.Message {
	return rule.Message{
		Id:          "voidReturnAttribute",
		Description: "Promise-returning function provided to attribute where a void return was expected.",
	}
}
func buildVoidReturnInheritedMethodMessage(heritageTypeName string) rule.Message {
	return rule.Message{
		Id: "voidReturnInheritedMethod",
		Description: fmt.Sprintf(
			"Promise-returning method provided where a void return was expected by extended/implemented type '%v'.",
			heritageTypeName,
		),
	}
}
func buildVoidReturnPropertyMessage() rule.Message {
	return rule.Message{
		Id:          "voidReturnProperty",
		Description: "Promise-returning function provided to property where a void return was expected.",
	}
}
func buildVoidReturnReturnValueMessage() rule.Message {
	return rule.Message{
		Id:          "voidReturnReturnValue",
		Description: "Promise-returning function provided to return value where a void return was expected.",
	}
}
func buildVoidReturnVariableMessage() rule.Message {
	return rule.Message{
		Id:          "voidReturnVariable",
		Description: "Promise-returning function provided to variable where a void return was expected.",
	}
}

type NoMisusedPromisesChecksVoidReturnOptions struct {
	Arguments        *bool `json:"arguments"`
	Attributes       *bool `json:"attributes"`
	InheritedMethods *bool `json:"inheritedMethods"`
	Properties       *bool `json:"properties"`
	Returns          *bool `json:"returns"`
	Variables        *bool `json:"variables"`
}

// NoMisusedPromises flags a Promise used where the surrounding code cannot handle one: as a boolean
// condition, spread into an object, or passed somewhere a `void`-returning function was expected.
//
//	valid:   const p = Promise.resolve(); if (await p) {}
//	valid:   [1, 2].forEach(async () => {});          // forEach takes a void return AND a thenable
//	invalid: const p = Promise.resolve(); if (p) {}   // conditional, always truthy
//	invalid: [1, 2].filter(async n => n > 1);         // predicate, the array gets Promises, not booleans
//	invalid: declare function f(cb: () => void): void; f(async () => {});  // voidReturnArgument
//	invalid: const obj = { ...Promise.resolve({ a: 1 }) };                 // spread
//
// A Promise in a boolean position is always truthy, so `if (p)` takes the true branch whether the
// work succeeded or failed, and `arr.filter(async …)` filters on Promise objects rather than on what
// they resolve to. Both compile. Neither does what the author wrote it to do, and both are silent at
// runtime rather than throwing, which is what makes this worth a type-aware rule rather than review.
//
// # Absorbed from tsgolint, which is the source of record for this rule
//
// Provenance: tsgolint `internal/rules/no_misused_promises/no_misused_promises.go`, at commit
// `076823b`, absorbed onto cohere's own rule interface here. It reaches for `GetCallSignatures`,
// `GetConstructSignatures`, `IsThenableType`, `IsArrayMethodCallWithPredicate`, `GetHeritageClauses`,
// `IsRestParameterDeclaration`, `UnionTypeParts`, `IsTypeFlagSet`, `Some`, `Map`, `Flatten` and `Ref`
// because that is what upstream reaches for, and this note is why a reader finds those helpers in a
// file that otherwise looks native.
//
// The vendoring step is measurable and was measured: rewriting exactly four import paths
// (`microsoft/typescript-go/shim/` to `microsoft/TypeScript/tsc/shim/`, and
// `typescript-eslint/tsgolint/internal/{rule,utils}` to ours) produces a byte diff showing those four
// lines and nothing else. Every helper the rule calls was already on the shelf at
// `internal/utilities/typecheck/`, and every checker entry point it needs is reachable through
// our shim.
//
// tsgolint is not re-synced, so this file is now the only copy of the algorithm rather than a
// translation layer over a vendored one. The checker logic below is byte-identical to upstream's;
// what changed is the interface it speaks, plus the nil-checker guard described below. No predicate,
// no flag set, no defaulting block and no traversal was touched, and both options structs came across
// field for field rather than being re-declared.
//
// oxc declares this rule as a tsgolint delegate and carries no algorithm and no corpus, so the
// behavior oxlint exhibits is tsgolint's: the release binary shells out to a `tsgolint` executable
// and refuses to run the rule when that binary is absent, which is what it does on this machine
// (`Failed to find tsgolint executable`). That refusal is itself the proof, so oxlint cannot serve as
// ground truth here and tsgolint's own test file is the corpus instead.
//
// # three independent checks, and one of them is an object of sub-flags
//
// This is the most structurally complex rule in this family, and the shape is the thing to hold:
//
//	checksConditionals  default TRUE   a Promise in a boolean position, plus array predicates
//	checksVoidReturn    default TRUE   a Promise-returning function where void was expected
//	checksSpreads       default TRUE   a Promise spread into an object
//
// `checksConditionals` also takes upstream's object, `{flagUnions}`, which turns the check on and
// says how a union holding a thenable is judged: "none" (the default, and what `true` means) reports
// only a value every member of which is thenable, "all" any union holding one, and "strict" only
// `Promise<T> | T`, whose awaited types are the union's other members, each assignable both ways.
// Checked against upstream's 34 checksConditionals rows and edge rows in
// no_misused_promises_conditionals_corpus_test.go.
//
// `checksVoidReturn` is not one check. It gates six sub-flags, each of which is a distinct position
// in the syntax and a distinct message id, and each of which registers its own listeners:
//
//	arguments         voidReturnArgument         KindCallExpression, KindNewExpression
//	attributes        voidReturnAttribute        KindJsxAttribute
//	inheritedMethods  voidReturnInheritedMethod  KindClassDeclaration/Expression, KindInterfaceDeclaration
//	properties        voidReturnProperty         KindPropertyAssignment, KindMethodDeclaration,
//	                                             KindShorthandPropertyAssignment
//	returns           voidReturnReturnValue      KindReturnStatement
//	variables         voidReturnVariable         KindVariableDeclaration, and the assignment arm of
//	                                             KindBinaryExpression
//
// That set is established from upstream's struct and from the corpus rather than guessed: all six
// message ids appear in tsgolint's own test file, so all six positions demonstrably report
// (voidReturnArgument 32 times, voidReturnInheritedMethod 25, voidReturnProperty 15, voidReturnVariable
// 6, voidReturnAttribute 3, voidReturnReturnValue 2). A port that invented this set would have passed
// every fixture it also invented, which is why the count is recorded here.
//
// A captured rule inventory described the option surface for this rule and it was not to be trusted as the
// authority; upstream's struct is. That is the standing lesson of this lane rather than a remark
// about this entry.
//
// # The pointer fields are load-bearing and must not be flattened to plain bools
//
// Every option on this rule defaults to true: all three top-level checks and all six void-return
// sub-flags. That is nine fields whose default is not the zero value, so a plain `bool` could not
// tell "the user wrote false" from "the user wrote nothing", and the entire defaulting block at the
// top of `Run` reads exactly that distinction. Flattening any of them would silently turn the whole
// rule off for a config that merely omitted a key. `ChecksVoidReturnOpts` is a pointer to a struct
// for the same reason one level up.
//
// # Where the two references disagree, counted rather than assumed
//
// Listeners were counted on both sides rather than assumed, because a sibling in this family
// (`await-thenable`) had a fourth arm in `@typescript-eslint` that tsgolint lacks entirely. Here the
// two agree on every arm, and the two apparent differences are representational rather than
// behavioral:
//
//	SPREAD.       tsgolint registers KindSpreadElement AND KindSpreadAssignment; the plugin registers
//	              SpreadElement alone. That is because ESTree spells an object spread as a
//	              SpreadElement inside an ObjectExpression while typescript-go gives it its own kind.
//	              Same inputs, two spellings.
//
//	PREDICATES.   tsgolint registers KindPropertyAccessExpression and KindElementAccessExpression and
//	              then requires the parent to be a call; the plugin writes the selector
//	              `CallExpression > MemberExpression` and lets ESLint do that filtering. Same
//	              predicate, different layer.
//
//	VARIABLES.    tsgolint's assignment arm hangs off KindBinaryExpression guarded by
//	              `IsAssignmentExpression`; the plugin has a dedicated AssignmentExpression node.
//	              Same rule, different grammar.
//
// The one genuine, user-visible disagreement is message text on the spread id: the plugin renders
// "Expected a non-Promise value to be spread in an object" and tsgolint renders "…to be spreaded in
// an object". Upstream's wording is an error of English and it is reproduced here unrepaired, because
// oxlint runs tsgolint and the differential harness compares against oxlint, so correcting it would
// read as a difference the harness can see. A fixture asserts the exact rendered text so the next
// sync notices if upstream fixes it.
//
// # There are no fixes and no suggestions
//
// tsgolint ships neither for this rule, and neither does the corpus assert any. There is nothing for
// `rule_testing` to apply and nothing needing a hand-rolled suggestion applier. Upstream carries two
// `TODO(port)` markers at report sites where `@typescript-eslint` narrows the reported range to a
// function's head (`getFunctionHeadLoc`), and one at `getMemberIfExists` where it would escape leading
// underscores. All three gaps are carried across and stated at their sites, so a member named
// `__proto__` resolves through the un-escaped path exactly as it does upstream.
//
// # The checker, and the nil guard that is now reachable
//
// Every listener consults `ctx.TypeChecker`, so this needs the checker and its fixtures use
// `RunTyped`. While this family was adapted through `internal/rules/upstream`, the standing
// `if ctx.TypeChecker == nil` could not be written, because the listener bodies were upstream's and
// that adapter set `NeedsTypeChecker` unconditionally, which made the nil case unreachable. That
// adapter is gone. Absorbing the rule makes the nil case reachable, so the guard is written here.
//
// The direction matters more than usual. Under a checker-less Context this rule does not panic: our
// shim's `GetTypeAtLocation` and `GetSymbolAtLocation` return nil rather than crashing, so the rule
// would go silent, and silence makes every clean fixture pass having proven nothing, which for this
// rule is one hundred and twenty-three of its two hundred and thirteen cases. The guard sits at the
// top of `Run` rather than inside each of the fourteen listeners, which is strictly stronger: no
// listener is even registered, so no arm can be added later that forgets it.
// `TestNoMisusedPromisesRequiresTheTypedHarness` pins the declaration and the guard together so a
// later revert fails loudly instead of going vacuously green.
//
// # Cost
//
// This is the most expensive rule in the family and it should be read that way. It registers up to
// fourteen listeners, several on kinds that appear on nearly every line (call expressions, binary
// expressions, property access, variable declarations), and most arms reach the checker before they
// can decline. Upstream's `checkArguments` resolves the callee's type and then walks every signature's
// every parameter for every call in the file, and its JSX arm resolves every attribute's contextual
// type; both now ask the cheap half of their conjunction first, which is the one departure from
// upstream's order (#hekjpw3). The syntactic escape hatches upstream wrote (`isPossiblyFunctionType`
// on return statements and variable declarations, the `Arguments() == nil` early exit) exist for
// exactly that reason and are carried across rather than being treated as optional.
var NoMisusedPromises = rule.Rule[NoMisusedPromisesOptions]{
	Name: "@typescript-eslint/no-misused-promises",

	// Every listener consults the checker; see the note above on why the guard lives in Run.
	NeedsTypeChecker: true,
	TypeReach:        rule.TypeReachShapes,

	// ProgramReads is deliberately absent, and that is a measurement rather than an omission. The
	// program handle appears nowhere in this body: the rule asks the checker questions about nodes in
	// the file it was handed, and the checker resolves across module boundaries on its own without the
	// rule reaching past its own file. While this family was adapted, `upstream.Adapt` declared the
	// flag on every rule it wrapped by assumption; absorbing makes it answerable per rule, and the
	// honest answer here is no.
	//
	// Worth knowing for whoever writes the next one of these: the guard in `internal/dispatch` that
	// enforces the flag is textual, a grep for the selector over the rule's whole file, comments
	// included. So a doc comment that merely names the thing it is declining to use fails the guard
	// with a message asserting the opposite of the truth. This paragraph is worded around the token
	// for that reason, rather than because the phrasing reads better.

	Run: func(ruleContext rule.Context, options rule.Given[NoMisusedPromisesOptions]) rule.Listeners {
		if ruleContext.TypeChecker == nil {
			return nil
		}

		settings, ok := options.Read()
		if !ok {
			settings = NoMisusedPromisesOptions{}
		}
		if settings.ChecksConditionals == nil {
			settings.ChecksConditionals = type_checking.Reference(true)
		}
		if settings.ChecksConditionalsFlagUnions == "" {
			settings.ChecksConditionalsFlagUnions = "none"
		}
		if settings.ChecksSpreads == nil {
			settings.ChecksSpreads = type_checking.Reference(true)
		}
		if settings.ChecksVoidReturn == nil {
			settings.ChecksVoidReturn = type_checking.Reference(true)
		}
		if settings.ChecksVoidReturnOpts == nil {
			settings.ChecksVoidReturnOpts = type_checking.Reference(NoMisusedPromisesChecksVoidReturnOptions{})
		}
		if settings.ChecksVoidReturnOpts.Arguments == nil {
			settings.ChecksVoidReturnOpts.Arguments = type_checking.Reference(true)
		}
		if settings.ChecksVoidReturnOpts.Attributes == nil {
			settings.ChecksVoidReturnOpts.Attributes = type_checking.Reference(true)
		}
		if settings.ChecksVoidReturnOpts.InheritedMethods == nil {
			settings.ChecksVoidReturnOpts.InheritedMethods = type_checking.Reference(true)
		}
		if settings.ChecksVoidReturnOpts.Properties == nil {
			settings.ChecksVoidReturnOpts.Properties = type_checking.Reference(true)
		}
		if settings.ChecksVoidReturnOpts.Returns == nil {
			settings.ChecksVoidReturnOpts.Returns = type_checking.Reference(true)
		}
		if settings.ChecksVoidReturnOpts.Variables == nil {
			settings.ChecksVoidReturnOpts.Variables = type_checking.Reference(true)
		}

		anySignatureIsThenableType := func(
			node *ast.Node,
			subjectType *checker.Type,
		) bool {
			return type_checking.Some(type_checking.GetCallSignatures(
				ruleContext.TypeChecker,
				subjectType,
			), func(sig *checker.Signature) bool {
				return type_checking.IsThenableType(
					ruleContext.TypeChecker,
					node,
					checker.Checker_getReturnTypeOfSignature(ruleContext.TypeChecker, sig),
				)
			})
		}

		returnsThenable := func(node *ast.Node) bool {
			subjectType := checker.Checker_getApparentType(
				ruleContext.TypeChecker,
				ruleContext.TypeChecker.GetTypeAtLocation(node),
			)
			return type_checking.Some(type_checking.UnionTypeParts(subjectType), func(part *checker.Type) bool {
				return anySignatureIsThenableType(node, part)
			})
		}

		checkArrayPredicates := func(node *ast.Node) {
			parent := node.Parent

			if !ast.IsCallExpression(parent) {
				return
			}

			expression := parent.AsCallExpression()
			arguments := expression.Arguments.Nodes
			if len(arguments) == 0 {
				return
			}

			callback := arguments[0]

			if type_checking.IsArrayMethodCallWithPredicate(ruleContext.TypeChecker, expression) &&
				returnsThenable(callback) {
				ruleContext.ReportNode(callback, buildPredicateMessage())
			}
		}

		isFunctionParameter := func(
			parameter *ast.Symbol,
			node *ast.Node,
		) bool {
			subjectType := checker.Checker_getApparentType(
				ruleContext.TypeChecker,
				ruleContext.TypeChecker.GetTypeOfSymbolAtLocation(parameter, node),
			)
			if subjectType == nil {
				return false
			}
			return type_checking.Some(type_checking.UnionTypeParts(subjectType), func(part *checker.Type) bool {
				return len(type_checking.GetCallSignatures(ruleContext.TypeChecker, part)) != 0
			})
		}

		// Variation on the thenable check which requires all forms of the type (read:
		// alternates in a union) to be thenable. Otherwise, you might be trying to
		// check if something is defined or undefined and get caught because one of the
		// branches is thenable.
		isAlwaysThenable := func(node *ast.Node) bool {
			subjectType := ruleContext.TypeChecker.GetTypeAtLocation(node)

			for subType := range type_checking.UnionTypePartsSequence(
				checker.Checker_getApparentType(ruleContext.TypeChecker, subjectType),
			) {
				thenProperty := checker.Checker_getPropertyOfType(ruleContext.TypeChecker, subType, "then")

				// If one of the alternates has no then property, it is not thenable in all
				// cases.
				if thenProperty == nil {
					return false
				}

				// We walk through each variation of the then property. Since we know it
				// exists at this point, we just need at least one of the alternates to
				// be of the right form to consider it thenable.
				thenType := ruleContext.TypeChecker.GetTypeOfSymbolAtLocation(thenProperty, node)
				hasThenableSignature := false
				for subType := range type_checking.UnionTypePartsSequence(thenType) {
					for _, signature := range type_checking.GetCallSignatures(ruleContext.TypeChecker, subType) {
						parameters := checker.Signature_parameters(signature)
						if len(parameters) != 0 && isFunctionParameter(parameters[0], node) {
							hasThenableSignature = true
						}
					}

					// We only need to find one variant of the then property that has a
					// function signature for it to be thenable.
					if hasThenableSignature {
						break
					}
				}

				// If no flavors of the then property are thenable, we don't consider the
				// overall type to be thenable
				if !hasThenableSignature {
					return false
				}
			}

			// If all variants are considered thenable (i.e. haven't returned false), we
			// consider the overall type thenable
			return true
		}

		checkedNodes := map[*ast.Node](struct{}){}

		var checkConditional func(
			node *ast.Expression,
			isTestExpression bool,
		)
		checkConditional = func(
			node *ast.Expression,
			isTestExpression bool,
		) {
			if node == nil || ast.IsAssignmentExpression(node, false) {
				return
			}
			// prevent checking the same node multiple times
			if _, ok := checkedNodes[node]; ok {
				return
			}
			checkedNodes[node] = struct{}{}

			unparenthesized := ast.SkipParentheses(node)

			if ast.IsBinaryExpression(unparenthesized) && ast.IsLogicalExpression(unparenthesized) {
				expression := unparenthesized.AsBinaryExpression()
				// ignore the left operand for nullish coalescing expressions not in a context of a test expression
				if expression.OperatorToken.Kind != ast.KindQuestionQuestionToken || isTestExpression {
					checkConditional(expression.Left, isTestExpression)
				}
				// we ignore the right operand when not in a context of a test expression
				if isTestExpression {
					checkConditional(expression.Right, isTestExpression)
				}
				return
			}

			if isAlwaysThenable(unparenthesized) {
				ruleContext.ReportNode(unparenthesized, buildConditionalMessage())
				return
			}

			// flagUnions widens the check to a union holding a thenable: every one under "all", and
			// under "strict" only one whose awaited half is the rest, as in `Promise<T> | T`.
			switch settings.ChecksConditionalsFlagUnions {
			case "all":
				if isSometimesThenable(ruleContext.TypeChecker, unparenthesized) {
					ruleContext.ReportNode(unparenthesized, buildConditionalMessage())
				}
			case "strict":
				if hasMatchingPromiseTypeArgument(ruleContext.TypeChecker, unparenthesized) {
					ruleContext.ReportNode(unparenthesized, buildConditionalMessage())
				}
			}
		}

		getMemberIfExists := func(
			subjectType *checker.Type,
			memberName string,
		) *ast.Symbol {
			// Upstream escapes the name first, with `ts.escapeLeadingUnderscores`. This port does not, a gap
			// carried from the Go port it came from (see the doc above).
			symbol := checker.Type_symbol(subjectType)
			if symbol != nil {
				symbol = symbol.Members[memberName]
			}
			if symbol != nil {
				return symbol
			}

			return checker.Checker_getPropertyOfType(ruleContext.TypeChecker, subjectType, memberName)
		}

		isVoidReturningFunctionType := func(
			node *ast.Node,
			subjectType *checker.Type,
		) bool {
			hadVoidReturn := false
			for part := range type_checking.UnionTypePartsSequence(subjectType) {
				for _, sig := range type_checking.GetCallSignatures(ruleContext.TypeChecker, part) {
					returnType := checker.Checker_getReturnTypeOfSignature(ruleContext.TypeChecker, sig)
					// If a certain positional argument accepts both thenable and void returns,
					// a promise-returning function is valid
					if type_checking.IsThenableType(ruleContext.TypeChecker, node, returnType) {
						return false
					}

					hadVoidReturn = hadVoidReturn || type_checking.IsTypeFlagSet(returnType, checker.TypeFlagsVoid)
				}
			}
			return hadVoidReturn
		}

		/**
		 * Checks `heritageType` for a member named `memberName` that returns void; reports the
		 * 'voidReturnInheritedMethod' message if found.
		 * @param nodeMember Node member that returns a Promise
		 * @param heritageType Heritage type to check against
		 * @param memberName Name of the member to check for
		 */
		checkHeritageTypeForMemberReturningVoid := func(
			nodeMember *ast.Node,
			heritageType *checker.Type,
			memberName string,
		) {
			heritageMember := getMemberIfExists(heritageType, memberName)
			if heritageMember == nil {
				return
			}
			memberType := ruleContext.TypeChecker.GetTypeOfSymbolAtLocation(
				heritageMember,
				nodeMember,
			)
			if !isVoidReturningFunctionType(nodeMember, memberType) {
				return
			}
			ruleContext.ReportNode(
				nodeMember,
				buildVoidReturnInheritedMethodMessage(ruleContext.TypeChecker.TypeToString(heritageType)),
			)
		}

		checkJsxAttribute := func(node *ast.JsxAttribute) {
			if node.Initializer == nil || node.Initializer.Kind != ast.KindJsxExpression {
				return
			}
			expressionContainer := node.Initializer.AsJsxExpression()
			expression := expressionContainer.Expression
			// Upstream asks for the contextual type first and the expression's type last. The three
			// questions are a conjunction with no side effects, so the cheap one goes first: the
			// attribute's contextual type resolves the element's props for every attribute, and was
			// 1.2s of this rule's 2.5s of CPU on ahra, while the expression's own type was 0.2s and
			// is a function returning a thenable for almost no attribute (#hekjpw3).
			if !returnsThenable(expression) {
				return
			}
			contextualType := checker.Checker_getContextualType(
				ruleContext.TypeChecker,
				node.Initializer,
				checker.ContextFlagsNone,
			)
			if contextualType != nil && isVoidReturningFunctionType(node.Initializer, contextualType) {
				ruleContext.ReportNode(node.Initializer, buildVoidReturnAttributeMessage())
			}
		}

		checkSpread := func(node *ast.Node) {
			if type_checking.IsThenableType(ruleContext.TypeChecker, node.Expression(), nil) {
				ruleContext.ReportNode(node.Expression(), buildSpreadMessage())
			}
		}

		isThenableReturningFunctionType := func(
			node *ast.Node,
			subjectType *checker.Type,
		) bool {
			return type_checking.Some(type_checking.UnionTypeParts(subjectType), func(part *checker.Type) bool {
				return anySignatureIsThenableType(node, part)
			})
		}

		var checkThenableOrVoidArgument func(
			node *ast.Expression,
			subjectType *checker.Type,
			index int,
			thenableReturnIndices *[]int,
			voidReturnIndices *[]int,
		)
		checkThenableOrVoidArgument = func(
			node *ast.Expression,
			subjectType *checker.Type,
			index int,
			thenableReturnIndices *[]int,
			voidReturnIndices *[]int,
		) {
			if isThenableReturningFunctionType(node.Expression(), subjectType) {
				(*thenableReturnIndices) = append(*thenableReturnIndices, index)
			} else if isVoidReturningFunctionType(node.Expression(), subjectType) &&
				// If a certain argument accepts both thenable and void returns,
				// a promise-returning function is valid
				!slices.Contains(*thenableReturnIndices, index) {

				(*voidReturnIndices) = append(*voidReturnIndices, index)
			}
			contextualType := checker.Checker_getContextualTypeForArgumentAtIndex(ruleContext.TypeChecker, node, index)

			if contextualType != subjectType {
				checkThenableOrVoidArgument(
					node,
					contextualType,
					index,
					thenableReturnIndices,
					voidReturnIndices,
				)
			}
		}

		// Get the positions of arguments which are void functions (and not also
		// thenable functions). These are the candidates for the void-return check at
		// the current call site.
		// If the function parameters end with a 'rest' parameter, then we consider
		// the array type parameter (e.g. '...args:Array<SomeType>') when determining
		// if trailing arguments are candidates.
		voidFunctionArguments := func(
			node *ast.Expression,
		) []int {
			// 'new' can be used without any arguments, as in 'let b = new Object;'
			// In this case, there are no argument positions to check, so return early.
			if node.Arguments() == nil {
				return []int{}
			}
			thenableReturnIndices := []int{}
			voidReturnIndices := []int{}
			subjectType := ruleContext.TypeChecker.GetTypeAtLocation(node.Expression())

			// We can't use checker.getResolvedSignature because it prefers an early '() => void' over a later '() =>
			// Promise<void>' See https://github.com/microsoft/TypeScript/issues/48077

			for subType := range type_checking.UnionTypePartsSequence(subjectType) {
				// Standard function calls and `new` have two different types of signatures
				var signatures []*checker.Signature
				if ast.IsCallExpression(node) {
					signatures = type_checking.GetCallSignatures(ruleContext.TypeChecker, subType)
				} else {
					signatures = type_checking.GetConstructSignatures(ruleContext.TypeChecker, subType)
				}
				for _, signature := range signatures {
					for index, parameter := range checker.Signature_parameters(signature) {
						decl := parameter.ValueDeclaration
						subjectType := ruleContext.TypeChecker.GetTypeOfSymbolAtLocation(parameter, node.Expression())

						// If this is a array 'rest' parameter, check all of the argument indices
						// from the current argument to the end.
						if decl != nil && type_checking.IsRestParameterDeclaration(decl) {
							if checker.Checker_isArrayType(ruleContext.TypeChecker, subjectType) {
								// Unwrap 'Array<MaybeVoidFunction>' to 'MaybeVoidFunction',
								// so that we'll handle it in the same way as a non-rest
								// 'param: MaybeVoidFunction'
								subjectType = checker.Checker_getTypeArguments(ruleContext.TypeChecker, subjectType)[0]
								for argumentIndex := index; argumentIndex < len(node.Arguments()); argumentIndex++ {
									checkThenableOrVoidArgument(
										node,
										subjectType,
										argumentIndex,
										&thenableReturnIndices,
										&voidReturnIndices,
									)
								}
							} else if checker.IsTupleType(subjectType) {
								// Check each type in the tuple - for example, [boolean, () => void] would
								// add the index of the second tuple parameter to 'voidReturnIndices'
								typeArguments := checker.Checker_getTypeArguments(ruleContext.TypeChecker, subjectType)
								for argumentIndex := index; argumentIndex < len(node.Arguments()) &&
									argumentIndex-index < len(typeArguments); argumentIndex++ {
									checkThenableOrVoidArgument(
										node,
										typeArguments[argumentIndex-index],
										argumentIndex,
										&thenableReturnIndices,
										&voidReturnIndices,
									)
								}
							}
						} else {
							checkThenableOrVoidArgument(
								node,
								subjectType,
								index,
								&thenableReturnIndices,
								&voidReturnIndices,
							)
						}
					}
				}
			}

			for _, index := range thenableReturnIndices {
				at := slices.Index(voidReturnIndices, index)
				if at >= 0 {
					voidReturnIndices = slices.Delete(voidReturnIndices, at, at+1)
				}
			}

			return voidReturnIndices
		}

		// An argument is reported when its position takes a void-returning function and the argument
		// returns a thenable. Upstream resolves the positions first, which walks every parameter of
		// every signature and asks each its contextual type, for every call in the file, including
		// calls with no arguments at all. Asking each argument first is the same conjunction in the
		// other order: almost no argument returns a thenable, so almost no call resolves its
		// positions (#hekjpw3).
		checkArguments := func(
			node *ast.Expression,
		) {
			thenableArguments := []int{}
			for index, argument := range node.Arguments() {
				if returnsThenable(argument) {
					thenableArguments = append(thenableArguments, index)
				}
			}
			if len(thenableArguments) == 0 {
				return
			}

			voidArguments := voidFunctionArguments(node)
			if len(voidArguments) == 0 {
				return
			}

			for _, index := range thenableArguments {
				if slices.Contains(voidArguments, index) {
					ruleContext.ReportNode(node.Arguments()[index], buildVoidReturnArgumentMessage())
				}
			}
		}

		checkClassLikeOrInterfaceNode := func(
			node *ast.Node,
		) {
			heritageClauses := type_checking.GetHeritageClauses(node)
			if heritageClauses == nil || len(heritageClauses.Nodes) == 0 {
				return
			}

			heritageTypes := type_checking.Flatten(
				type_checking.Map(heritageClauses.Nodes, func(heritageClause *ast.Node) []*checker.Type {
					return type_checking.Map(heritageClause.AsHeritageClause().Types.Nodes, func(
						node *ast.Node,
					) *checker.Type {
						return ruleContext.TypeChecker.GetTypeAtLocation(node)
					})
				}),
			)

			for _, nodeMember := range node.Members() {
				if nodeMember.Name() == nil {
					// Call/construct/index signatures don't have names. TS allows call signatures to mismatch,
					// and construct signatures can't be async.
					// An index signature is not checked against its compatible index signatures in
					// `heritageTypes`, because that needs `checker.isTypeAssignableTo`, which
					// `@typescript-eslint` notes it cannot use before its v8.
					continue
				}
				if !(ast.IsIdentifier(nodeMember.Name()) ||
					ast.IsPrivateIdentifier(nodeMember.Name()) ||
					ast.IsStringLiteral(nodeMember.Name()) ||
					ast.IsNumericLiteral(nodeMember.Name()) ||
					ast.IsBigIntLiteral(nodeMember.Name())) {
					continue
				}
				memberName := nodeMember.Name().Text()
				if ast.IsStatic(nodeMember) {
					continue
				}
				if !returnsThenable(nodeMember) {
					continue
				}
				for _, heritageType := range heritageTypes {
					checkHeritageTypeForMemberReturningVoid(
						nodeMember,
						heritageType,
						memberName,
					)
				}
			}
		}

		checkProperty := func(node *ast.Node) {
			if ast.IsPropertyAssignment(node) {
				property := node.AsPropertyAssignment()
				contextualType := checker.Checker_getContextualType(
					ruleContext.TypeChecker,
					property.Initializer,
					checker.ContextFlagsNone,
				)

				if contextualType != nil && isVoidReturningFunctionType(
					property.Initializer,
					contextualType,
				) && returnsThenable(property.Initializer) {
					if ast.IsFunctionLike(property.Initializer) {
						returnType := property.Initializer.Type()
						if returnType != nil {
							ruleContext.ReportNode(returnType, buildVoidReturnPropertyMessage())
						} else {
							ruleContext.ReportNode(
								// Upstream narrows this to the function's head with `getFunctionHeadLoc`;
								// this port reports the whole initializer, the same finding over a wider range.
								property.Initializer,
								buildVoidReturnPropertyMessage(),
							)
						}
					} else {
						ruleContext.ReportNode(property.Initializer, buildVoidReturnPropertyMessage())
					}
				}
			} else if ast.IsShorthandPropertyAssignment(node) {
				contextualType := checker.Checker_getContextualType(
					ruleContext.TypeChecker,
					node.Name(),
					checker.ContextFlagsNone,
				)
				if contextualType != nil &&
					isVoidReturningFunctionType(node.Name(), contextualType) &&
					returnsThenable(node.Name()) {
					ruleContext.ReportNode(node.Name(), buildVoidReturnPropertyMessage())
				}
			} else if ast.IsMethodDeclaration(node) {
				if ast.IsComputedPropertyName(node.Name()) {
					return
				}
				object := node.Parent

				// Below condition isn't satisfied unless something goes wrong,
				// but is needed for type checking.
				// 'node' does not include class method declaration so 'obj' is
				// always an object literal expression, but after converting 'node'
				// to TypeScript AST, its type includes MethodDeclaration which
				// does include the case of class method declaration.
				if !ast.IsObjectLiteralExpression(object) {
					return
				}

				if !returnsThenable(node) {
					return
				}
				objectType := checker.Checker_getContextualType(
					ruleContext.TypeChecker,
					object,
					checker.ContextFlagsNone,
				)
				if objectType == nil {
					return
				}
				propertySymbol := checker.Checker_getPropertyOfType(
					ruleContext.TypeChecker,
					objectType,
					node.Name().Text(),
				)
				if propertySymbol == nil {
					return
				}

				contextualType := ruleContext.TypeChecker.GetTypeOfSymbolAtLocation(
					propertySymbol,
					node.Name(),
				)

				if isVoidReturningFunctionType(node.Name(), contextualType) {
					if ast.IsMethodDeclaration(node) {
					}

					if node.Type() != nil {
						ruleContext.ReportNode(node.Type(), buildVoidReturnPropertyMessage())
					} else {
						ruleContext.ReportNode(
							// Upstream narrows this to the function's head with `getFunctionHeadLoc`;
							// this port reports the whole node, the same finding over a wider range.
							node,
							buildVoidReturnPropertyMessage(),
						)
					}
				}
			}
		}

		/**
		 * A syntactic check to see if an annotated type is maybe a function type.
		 * This is a perf optimization to help avoid requesting types where possible
		 */
		isPossiblyFunctionType := func(node *ast.Node) bool {
			switch node.Kind {
			case ast.KindConditionalType,
				ast.KindConstructorType,
				ast.KindFunctionType,
				ast.KindImportType,
				ast.KindIndexedAccessType,
				ast.KindInferType,
				ast.KindIntersectionType,
				ast.KindQualifiedName,
				ast.KindThisType,
				ast.KindTypeOperator,
				ast.KindTypeQuery,
				ast.KindTypeReference,
				ast.KindUnionType:
				return true

			case ast.KindTypeLiteral:
				return type_checking.Some(node.AsTypeLiteralNode().Members.Nodes, func(member *ast.Node) bool {
					return member.Kind == ast.KindCallSignature || member.Kind == ast.KindConstructSignature
				})

			case ast.KindAbstractKeyword,
				ast.KindAnyKeyword,
				ast.KindArrayType,
				ast.KindAsyncKeyword,
				ast.KindBigIntKeyword,
				ast.KindBooleanKeyword,
				ast.KindDeclareKeyword,
				ast.KindExportKeyword,
				ast.KindIntrinsicKeyword,
				ast.KindLiteralType,
				ast.KindMappedType,
				ast.KindNamedTupleMember,
				ast.KindNeverKeyword,
				ast.KindNullKeyword,
				ast.KindNumberKeyword,
				ast.KindObjectKeyword,
				ast.KindOptionalType,
				ast.KindPrivateKeyword,
				ast.KindProtectedKeyword,
				ast.KindPublicKeyword,
				ast.KindReadonlyKeyword,
				ast.KindRestType,
				ast.KindStaticKeyword,
				ast.KindStringKeyword,
				ast.KindSymbolKeyword,
				ast.KindTemplateLiteralType,
				ast.KindTupleType,
				ast.KindTypePredicate,
				ast.KindUndefinedKeyword,
				ast.KindUnknownKeyword,
				ast.KindVoidKeyword:
				return false
			}
			return false
		}

		checkReturnStatement := func(node *ast.ReturnStatement) {
			if node.Expression == nil {
				return
			}

			// syntactically ignore some known-good cases to avoid touching type info
			functionNode := (func() *ast.Node {
				current := node.Parent
				for current != nil && !ast.IsFunctionLike(current) {
					current = current.Parent
				}
				return current
			})()
			// A `return` outside any function parses, and only the checker rejects it. Upstream throws
			// on the missing parent; passing over it keeps one file the compiler refuses from stopping
			// the whole run.
			if functionNode == nil {
				return
			}

			if functionNode.Type() != nil && !isPossiblyFunctionType(functionNode.Type()) {
				return
			}

			contextualType := checker.Checker_getContextualType(
				ruleContext.TypeChecker,
				node.Expression,
				checker.ContextFlagsNone,
			)
			if contextualType != nil &&
				isVoidReturningFunctionType(
					node.Expression,
					contextualType,
				) && returnsThenable(node.Expression) {
				ruleContext.ReportNode(node.Expression, buildVoidReturnReturnValueMessage())
			}
		}

		checkAssignment := func(node *ast.BinaryExpression) {
			varType := ruleContext.TypeChecker.GetTypeAtLocation(node.Left)
			if !isVoidReturningFunctionType(node.Left, varType) {
				return
			}

			if returnsThenable(node.Right) {
				ruleContext.ReportNode(node.Right, buildVoidReturnVariableMessage())
			}
		}

		checkVariableDeclaration := func(node *ast.VariableDeclaration) {
			if node.Initializer == nil ||
				node.Type == nil {
				return
			}

			// syntactically ignore some known-good cases to avoid touching type info
			if !isPossiblyFunctionType(node.Type) {
				return
			}

			varType := ruleContext.TypeChecker.GetTypeAtLocation(node.Name())
			if !isVoidReturningFunctionType(node.Initializer, varType) {
				return
			}

			if returnsThenable(node.Initializer) {
				ruleContext.ReportNode(node.Initializer, buildVoidReturnVariableMessage())
			}
		}

		listeners := rule.Listeners{
			ast.KindBinaryExpression: func(node *ast.Node) {
				if *settings.ChecksConditionals {
					checkConditional(node, false)
				}

				if *settings.ChecksVoidReturn &&
					*settings.ChecksVoidReturnOpts.Variables &&
					ast.IsAssignmentExpression(node, false) {
					checkAssignment(node.AsBinaryExpression())
				}
			},
		}
		if *settings.ChecksConditionals {
			listeners[ast.KindPropertyAccessExpression] = checkArrayPredicates
			listeners[ast.KindElementAccessExpression] = checkArrayPredicates

			listeners[ast.KindPrefixUnaryExpression] = func(node *ast.Node) {
				expression := node.AsPrefixUnaryExpression()
				if expression.Operator == ast.KindExclamationToken {
					checkConditional(expression.Operand, true)
				}
			}

			listeners[ast.KindConditionalExpression] = func(
				node *ast.Node,
			) {
				checkConditional(node.AsConditionalExpression().Condition, true)
			}
			listeners[ast.KindForStatement] = func(
				node *ast.Node,
			) {
				checkConditional(node.AsForStatement().Condition, true)
			}
			listeners[ast.KindDoStatement] = func(node *ast.Node) { checkConditional(node.Expression(), true) }
			listeners[ast.KindWhileStatement] = func(node *ast.Node) { checkConditional(node.Expression(), true) }
			listeners[ast.KindIfStatement] = func(node *ast.Node) { checkConditional(node.Expression(), true) }
		}

		if *settings.ChecksVoidReturn {
			if *settings.ChecksVoidReturnOpts.Arguments {
				listeners[ast.KindCallExpression] = checkArguments
				listeners[ast.KindNewExpression] = checkArguments
			}
			if *settings.ChecksVoidReturnOpts.Attributes {
				listeners[ast.KindJsxAttribute] = func(node *ast.Node) { checkJsxAttribute(node.AsJsxAttribute()) }
			}
			if *settings.ChecksVoidReturnOpts.InheritedMethods {
				listeners[ast.KindClassDeclaration] = checkClassLikeOrInterfaceNode
				listeners[ast.KindClassExpression] = checkClassLikeOrInterfaceNode
				listeners[ast.KindInterfaceDeclaration] = checkClassLikeOrInterfaceNode
			}
			if *settings.ChecksVoidReturnOpts.Properties {
				listeners[ast.KindPropertyAssignment] = checkProperty
				listeners[ast.KindMethodDeclaration] = checkProperty
				listeners[ast.KindShorthandPropertyAssignment] = checkProperty
			}
			if *settings.ChecksVoidReturnOpts.Returns {
				listeners[ast.KindReturnStatement] = func(
					node *ast.Node,
				) {
					checkReturnStatement(node.AsReturnStatement())
				}
			}
			if *settings.ChecksVoidReturnOpts.Variables {
				listeners[ast.KindVariableDeclaration] = func(
					node *ast.Node,
				) {
					checkVariableDeclaration(node.AsVariableDeclaration())
				}
			}

		}
		if *settings.ChecksSpreads {
			listeners[ast.KindSpreadElement] = checkSpread
			listeners[ast.KindSpreadAssignment] = checkSpread
		}

		return listeners

	},
}

// isSometimesThenable reports whether any member of a value's union type is thenable, upstream's
// isSometimesThenable, which flagUnions "all" reports.
func isSometimesThenable(typeChecker *checker.Checker, node *ast.Node) bool {
	valueType := typeChecker.GetTypeAtLocation(node)
	for part := range type_checking.UnionTypePartsSequence(checker.Checker_getApparentType(typeChecker, valueType)) {
		if type_checking.IsThenableType(typeChecker, node, part) {
			return true
		}
	}
	return false
}

// hasMatchingPromiseTypeArgument is upstream's test of the same name, which flagUnions "strict"
// reports: a union holding thenables whose awaited types are, member for member, the union's other
// members, so `Promise<T> | T` reports and `Promise<T> | U` does not. Two types are the same when each
// is assignable to the other.
func hasMatchingPromiseTypeArgument(typeChecker *checker.Checker, node *ast.Node) bool {
	valueType := typeChecker.GetTypeAtLocation(node)
	var thenables []*checker.Type
	var others []*checker.Type
	for part := range type_checking.UnionTypePartsSequence(checker.Checker_getApparentType(typeChecker, valueType)) {
		if type_checking.IsThenableType(typeChecker, node, part) {
			thenables = append(thenables, part)
		} else {
			others = append(others, part)
		}
	}
	if len(thenables) == 0 {
		return false
	}
	var awaited []*checker.Type
	for _, thenable := range thenables {
		awaitedType := checker.Checker_getAwaitedType(typeChecker, thenable)
		if awaitedType == nil {
			return false
		}
		awaited = append(awaited, slices.Collect(type_checking.UnionTypePartsSequence(awaitedType))...)
	}
	equivalent := func(left *checker.Type, right *checker.Type) bool {
		return checker.Checker_isTypeAssignableTo(typeChecker, left, right) &&
			checker.Checker_isTypeAssignableTo(typeChecker, right, left)
	}
	matchesAny := func(candidate *checker.Type, among []*checker.Type) bool {
		return slices.ContainsFunc(among, func(other *checker.Type) bool { return equivalent(candidate, other) })
	}
	for _, other := range others {
		if !matchesAny(other, awaited) {
			return false
		}
	}
	for _, each := range awaited {
		if !matchesAny(each, others) {
			return false
		}
	}
	return true
}
