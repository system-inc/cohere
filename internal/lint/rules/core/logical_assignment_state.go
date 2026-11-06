package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/ecmascript/react"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// logicalAssignmentState carries the two things every judgment needs.
type logicalAssignmentState struct {
	ruleContext rule.Context
	// isStrict is the file-level answer to upstream's `sourceCode.getScope(ast).isStrict`, which is
	// consulted only to decide whether a `with` block can be in play.
	isStrict bool
	// skipCompiledFunctions silences every finding inside a function React Compiler compiles.
	skipCompiledFunctions bool
}

// logicalAssignmentIsUndefined is upstream's `isUndefined`: the identifier `undefined` resolving to
// the global, or a `void` expression over the literal zero.
//
// Upstream calls `isReferenceToGlobalVariable` on the identifier arm, which asks its scope analysis
// whether anything shadows the name. That question is answered here by asking whether the name has
// a declaration in this file: the real `undefined` global is declared in the default library rather
// than in source, so a symbol whose declarations all sit outside this file is the global. Written
// as the complement the brief prescribes, because `resolvesToAGlobal` returns false for a symbol
// with zero declarations and `undefined` is exactly that shape.
func (state *logicalAssignmentState) isUndefined(expression *ast.Node) bool {
	unwrapped := logicalAssignmentUnwrapParentheses(expression)
	if unwrapped == nil {
		return false
	}

	if unwrapped.Kind == ast.KindIdentifier && unwrapped.Text() == "undefined" {
		return !state.isShadowedInThisFile(unwrapped)
	}

	if unwrapped.Kind != ast.KindVoidExpression {
		return false
	}
	argument := logicalAssignmentUnwrapParentheses(unwrapped.AsVoidExpression().Expression)
	return argument != nil && argument.Kind == ast.KindNumericLiteral && argument.Text() == "0"
}

// isShadowedInThisFile answers whether a name is declared in the file being linted.
//
// This is the complement of upstream's `isReferenceToGlobalVariable`, and the complement is the
// direction that works. The brief records that `resolvesToAGlobal` answers false for every symbol
// with no declarations, and the real `undefined` has none, so asking "is this the global" directly
// would answer no on exactly the inputs this rule has to accept. Asking "is this declared here"
// answers no for the global and yes for a local shadow, which is the discrimination upstream makes.
//
// A nil checker returns false, which keeps the rule reading a bare `undefined` as the global. That
// is the answer for source that does not shadow it, which is nearly all source, so the untyped
// harness agrees with the typed one on everything except a deliberate shadow.
func (state *logicalAssignmentState) isShadowedInThisFile(identifier *ast.Node) bool {
	if state.ruleContext.TypeChecker == nil {
		return false
	}
	symbol := state.ruleContext.TypeChecker.GetSymbolAtLocation(identifier)
	if symbol == nil {
		return false
	}
	return rule.IsDeclaredInFile(state.ruleContext.SourceFile, symbol)
}

// cannotBeGetter is upstream's function of the same name: is this reference safe to read once
// instead of twice.
//
// An identifier is safe, because a variable read has no side effect. Inside a non-strict `with`
// block it is not safe, because the name may resolve to a property of the scrutinee, which may be
// a getter. Anything that is not an identifier is unsafe here, and the second shape widens that
// separately through accessesSingleProperty.
func (state *logicalAssignmentState) cannotBeGetter(node *ast.Node) bool {
	if node == nil || node.Kind != ast.KindIdentifier {
		return false
	}
	return state.isStrict || !logicalAssignmentIsInsideWithBlock(node)
}

// accessesSingleProperty is upstream's function of the same name.
//
// It widens the safe set for the second and third shapes to a member access reading exactly one
// property off a simple base: the object must be an identifier, `this` or `super`, and a computed
// key must not itself be an access. `a.b` qualifies and `a.b.c` does not, because evaluating `a.b`
// twice is not knowably the same as evaluating it once.
//
// The non-strict `with` branch inverts the whole test, matching upstream: inside a `with` an
// identifier is the only thing that reads a single property, because a member access there starts
// from a name that may itself be a scrutinee property.
func (state *logicalAssignmentState) accessesSingleProperty(node *ast.Node) bool {
	if node == nil {
		return false
	}
	if !state.isStrict && logicalAssignmentIsInsideWithBlock(node) {
		return node.Kind == ast.KindIdentifier
	}

	switch node.Kind {
	case ast.KindPropertyAccessExpression:
		access := node.AsPropertyAccessExpression()
		return logicalAssignmentIsSimpleBase(access.Expression)
	case ast.KindElementAccessExpression:
		access := node.AsElementAccessExpression()
		if !logicalAssignmentIsSimpleBase(access.Expression) {
			return false
		}
		// Upstream refuses a computed key that is itself a member expression or a chain, because
		// the key is then a second reference being evaluated twice alongside the first. An
		// optional access is upstream's ChainExpression, which is a QuestionDotToken here.
		argument := logicalAssignmentUnwrapParentheses(access.ArgumentExpression)
		if argument == nil {
			return false
		}
		if logicalAssignmentIsAccess(argument) {
			return false
		}
		return !logicalAssignmentContainsOptionalChain(argument)
	}
	return false
}

// report routes a finding to either the fix path or the suggestion path.
//
// This is upstream's `createConditionalFixer`, and the branch is the whole getter judgment: the
// same repair is either applied unattended or offered to a human, decided by whether collapsing the
// two reads can change what runs. A rule that always fixed would rewrite getters; one that always
// suggested would leave the common safe case to be applied by hand.
//
// A nil repair means the fixer itself declined, which upstream spells as a generator that yields
// nothing. In that case the finding is reported bare, with neither a fix nor a suggestion, which is
// what the corpus's `output: null` with an empty `suggestions` array asserts.
func (state *logicalAssignmentState) report(
	node *ast.Node,
	message rule.Message,
	suggestionMessage rule.Message,
	fixes []rule.Fix,
	shouldBeFixed bool,
) {
	// Asked here, at a finding, rather than on every node: the ancestor walk is paid only where the
	// rule already has something to say, which is the cheap test first and the expensive one after.
	if state.skipCompiledFunctions && react.IsInsideComponentOrHook(node) {
		return
	}
	if len(fixes) == 0 {
		state.ruleContext.ReportNode(node, message)
		return
	}
	if shouldBeFixed {
		state.ruleContext.ReportNodeWithFixes(node, message, fixes...)
		return
	}
	state.ruleContext.ReportNodeWithSuggestions(node, message,
		rule.Suggestion{Message: suggestionMessage, Fixes: fixes})
}

// checkAssignment is the first shape: `a = a || b`.
//
// Anchored on a KindBinaryExpression whose operator is `=` and whose right side is a logical
// expression, which is upstream's
// `AssignmentExpression[operator='='][right.type='LogicalExpression']` selector.
func (state *logicalAssignmentState) checkAssignment(node *ast.Node) {
	assignment := node.AsBinaryExpression()
	if assignment.OperatorToken == nil || assignment.OperatorToken.Kind != ast.KindEqualsToken {
		return
	}

	// The right side is unwrapped, and this is the parenthesis divergence in its subtle direction.
	//
	// Upstream's selector tests `right.type === "LogicalExpression"`, and its parser records the
	// parentheses around a whole expression in `extra` rather than as a node, so a fully
	// parenthesized right side still matches there. Ours wraps it, so without this unwrap
	// `a = (a || b || c)` finds nothing. Measured against the installed build: it reports and
	// fixes to `a ||= (b || c)`, keeping the parentheses that survive the deleted prefix.
	//
	// This is the opposite of the stop inside logicalAssignmentLeftmostOperand, and the pair is
	// what the corpus separates: the outer parentheses are transparent, while parentheses around an
	// inner operand are a deliberate grouping the rule refuses to re-associate across. `a = (a || b)
	// || c` is clean for that second reason while `a = (a || b || c)` reports for this first one.
	right := logicalAssignmentUnwrapParentheses(assignment.Right)
	if right == nil || right.Kind != ast.KindBinaryExpression {
		return
	}
	logical := right.AsBinaryExpression()
	if logical.OperatorToken == nil {
		return
	}
	operator, isLogical := logicalAssignmentShorthand[logical.OperatorToken.Kind]
	if !isLogical || !logicalAssignmentIsLogicalOperator(logical.OperatorToken.Kind) {
		return
	}

	leftOperand := logicalAssignmentLeftmostOperand(state.ruleContext.SourceFile, right)
	if !logicalAssignmentIsSameReference(assignment.Left, leftOperand) {
		return
	}

	fixes := state.assignmentFix(node, assignment, leftOperand)
	state.report(node, logicalAssignmentAssignmentMessage(operator),
		logicalAssignmentUseLogicalOperatorMessage(operator), fixes,
		state.cannotBeGetter(logicalAssignmentUnwrapParentheses(assignment.Left)))
}

// assignmentFix builds the first shape's repair: `a = a || b` becomes `a ||= b`.
//
// Upstream writes the logical operator in front of the `=` and then deletes from the start of the
// logical expression through the start of its right operand. Both edits are ranges into existing
// text rather than re-rendered constructs, so a type annotation or an assertion inside either
// surviving slice is carried through as bytes. That is the property the brief names twice: a fixer
// that re-renders loses whatever it did not think to re-render.
func (state *logicalAssignmentState) assignmentFix(
	node *ast.Node,
	assignment *ast.BinaryExpression,
	leftOperand *ast.Node,
) []rule.Fix {
	nodeRange := rule.TokenRange(state.ruleContext.SourceFile, node)
	if logicalAssignmentCommentsInside(state.ruleContext, nodeRange.Pos(), node.End()) {
		return nil
	}

	logical := leftOperand.Parent
	if logical == nil || logical.Kind != ast.KindBinaryExpression {
		return nil
	}
	logicalOperator := logical.AsBinaryExpression().OperatorToken
	if logicalOperator == nil {
		return nil
	}
	operatorText, known := logicalAssignmentShorthand[logicalOperator.Kind]
	if !known {
		return nil
	}

	equalsRange := rule.TokenRange(state.ruleContext.SourceFile, assignment.OperatorToken)
	rightOperand := logical.AsBinaryExpression().Right
	if rightOperand == nil {
		return nil
	}

	return []rule.Fix{
		// `a = a || b` -> `a ||= a || b`. The operator text drops its trailing `=`, since the `=`
		// already in the source is what it is written in front of.
		rule.ReplaceRange(core.NewTextRange(equalsRange.Pos(), equalsRange.Pos()),
			operatorText[:len(operatorText)-1]),
		// `a ||= a || b` -> `a ||= b`. The deletion runs from the start of the logical expression
		// to the start of its right operand, which takes the repeated target and the operator with
		// it. The right operand's own leading trivia is preserved by ending at its token start
		// rather than at its node position.
		rule.RemoveRange(core.NewTextRange(
			rule.TokenRange(state.ruleContext.SourceFile, logical).Pos(),
			rule.TokenRange(state.ruleContext.SourceFile, rightOperand).Pos())),
	}
}

// checkLogical is the second shape: `a || (a = b)`.
//
// Upstream's selector is `LogicalExpression[right.type="AssignmentExpression"][right.operator="="]`,
// and its own comment explains the right side must be parenthesized or the source would not parse.
// Ours keeps that parenthesis as a node, so the unwrap below is what makes the shape reachable at
// all. Without it this arm finds nothing, silently.
func (state *logicalAssignmentState) checkLogical(node *ast.Node) {
	logical := node.AsBinaryExpression()
	if logical.OperatorToken == nil {
		return
	}
	operator, isLogical := logicalAssignmentShorthand[logical.OperatorToken.Kind]
	if !isLogical || !logicalAssignmentIsLogicalOperator(logical.OperatorToken.Kind) {
		return
	}

	inner := logicalAssignmentUnwrapParentheses(logical.Right)
	if inner == nil || inner.Kind != ast.KindBinaryExpression {
		return
	}
	assignment := inner.AsBinaryExpression()
	if assignment.OperatorToken == nil || assignment.OperatorToken.Kind != ast.KindEqualsToken {
		return
	}

	// Upstream tests `isReference(logical.left)` before comparing, which excludes a bare
	// `undefined` on the left even though the comparison would accept it.
	if !logicalAssignmentIsReference(logicalAssignmentUnwrapParentheses(logical.Left)) {
		return
	}
	if !logicalAssignmentIsSameReference(logical.Left, assignment.Left) {
		return
	}

	fixes := state.logicalFix(node, inner)
	left := logicalAssignmentUnwrapParentheses(logical.Left)
	state.report(node, logicalAssignmentLogicalMessage(operator),
		logicalAssignmentConvertLogicalMessage(operator), fixes,
		state.cannotBeGetter(left) || state.accessesSingleProperty(left))
}

// logicalFix builds the second shape's repair: `a || (a = b)` becomes `a ||= b`.
//
// Three edits. The stretch from the start of the whole expression to the start of the inner
// assignment goes, which takes the repeated target, the operator and the opening parenthesis. The
// stretch after the assignment goes, which takes the closing parenthesis. The logical operator is
// written in front of the assignment's `=`.
//
// A fourth edit wraps the result in parentheses when the surrounding expression binds tighter than
// an assignment, because `a || (a = b)` is an operand where `a ||= b` would not be one.
func (state *logicalAssignmentState) logicalFix(node *ast.Node, inner *ast.Node) []rule.Fix {
	nodeRange := rule.TokenRange(state.ruleContext.SourceFile, node)
	if logicalAssignmentCommentsInside(state.ruleContext, nodeRange.Pos(), node.End()) {
		return nil
	}

	assignment := inner.AsBinaryExpression()
	operatorRange := rule.TokenRange(state.ruleContext.SourceFile, assignment.OperatorToken)
	logicalOperator := node.AsBinaryExpression().OperatorToken
	operatorText, known := logicalAssignmentShorthand[logicalOperator.Kind]
	if !known {
		return nil
	}

	innerRange := rule.TokenRange(state.ruleContext.SourceFile, inner)
	fixes := []rule.Fix{
		rule.RemoveRange(core.NewTextRange(nodeRange.Pos(), innerRange.Pos())),
		rule.RemoveRange(core.NewTextRange(inner.End(), node.End())),
		rule.ReplaceRange(core.NewTextRange(operatorRange.Pos(), operatorRange.Pos()),
			operatorText[:len(operatorText)-1]),
	}

	// Upstream's condition is `!isParenthesised(logical) && requiresOuterParenthesis`, and both
	// halves are needed. The token check is what makes a sole call argument keep its bare form.
	if !logicalAssignmentIsSurroundedByParentheses(
		state.ruleContext.SourceFile.Text(), nodeRange.Pos(), node.End()) &&
		state.logicalFixNeedsOuterParentheses(node) {
		fixes = append(fixes,
			rule.ReplaceRange(core.NewTextRange(nodeRange.Pos(), nodeRange.Pos()), "("),
			rule.ReplaceRange(core.NewTextRange(node.End(), node.End()), ")"))
	}
	return fixes
}

// logicalFixNeedsOuterParentheses answers upstream's `requiresOuterParenthesis`.
//
// An assignment binds looser than almost everything, so replacing a parenthesized guard with a bare
// assignment can be captured by the surrounding expression. Upstream compares the parent's
// precedence against an assignment's and wraps when the parent binds tighter, with an unknown
// parent treated as tighter so the fixer wraps rather than risks it.
//
// An expression statement is exempt, because a statement captures nothing.
func (state *logicalAssignmentState) logicalFixNeedsOuterParentheses(node *ast.Node) bool {
	parent := node.Parent
	if parent == nil {
		return false
	}
	if parent.Kind == ast.KindExpressionStatement {
		return false
	}
	parentPrecedence := operatorAssignmentPrecedence(parent)
	// Upstream's assignment precedence is 1, and an unrecognised parent is -1, which its comparison
	// treats as requiring the wrap.
	return parentPrecedence == -1 || 1 < parentPrecedence
}

// checkIfStatement is the third shape: `if (a) a = b`, which fires only under
// enforceForIfStatements.
//
// The condition has to be an existence check on the same reference the body assigns to, and
// upstream recognises five spellings of that check. An `if` with an `else` is excluded entirely,
// since the shorthand has nowhere to put the alternative.
func (state *logicalAssignmentState) checkIfStatement(node *ast.Node) {
	ifStatement := node.AsIfStatement()
	if ifStatement.ElseStatement != nil {
		return
	}

	hasBody := ifStatement.ThenStatement != nil && ifStatement.ThenStatement.Kind == ast.KindBlock
	body := ifStatement.ThenStatement
	if hasBody {
		statements := body.AsBlock().Statements
		if statements == nil || len(statements.Nodes) != 1 {
			return
		}
		body = statements.Nodes[0]
	}
	if body == nil || body.Kind != ast.KindExpressionStatement {
		return
	}

	// The body expression is unwrapped, because `if (a) (a = b)` is an assignment upstream sees
	// directly and we see through a parenthesis. Measured against the installed build: it reports
	// and fixes to `(a &&= b)`, keeping the parentheses the fix does not span.
	expression := logicalAssignmentUnwrapParentheses(body.AsExpressionStatement().Expression)
	if expression == nil || expression.Kind != ast.KindBinaryExpression {
		return
	}
	assignment := expression.AsBinaryExpression()
	if assignment.OperatorToken == nil || assignment.OperatorToken.Kind != ast.KindEqualsToken {
		return
	}

	reference, operator, found := state.existence(ifStatement.Expression)
	if !found {
		return
	}
	if !logicalAssignmentIsSameReference(reference, assignment.Left) {
		return
	}

	fixes := state.ifFix(node, ifStatement, body, expression, hasBody, operator)
	// Upstream additionally refuses the widened member-access allowance when the test is itself a
	// logical expression, because the two comparisons in `a === null || a === undefined` each read
	// the reference, so the collapse changes the read count by more than one.
	testIsLogical := logicalAssignmentTestIsLogical(ifStatement.Expression)
	shouldBeFixed := state.cannotBeGetter(reference) ||
		(!testIsLogical && state.accessesSingleProperty(reference))

	state.report(node, logicalAssignmentIfMessage(operator+"="),
		logicalAssignmentConvertIfMessage(operator+"="), fixes, shouldBeFixed)
}

// existence is upstream's `getExistence`: which reference does this condition test, and which
// operator folds that test in.
//
// Five spellings, and the operator differs by which:
//
//	a                                       &&=   truthiness
//	!a                                      ||=   falsiness
//	!!a                                     &&=   double negation is truthiness again
//	Boolean(a)                              &&=   an explicit cast
//	!Boolean(a)                             ||=
//	a == null   /  a == void 0              ??=   loose equality catches both nullish values
//	a === null || a === undefined           ??=   and the explicit pair spells the same test
//
// The `!!a` arm returns `&&=` rather than `||=` even though the outer `!` is present, because the
// inner `!` cancels it. That is upstream's third case and it is easy to lose when flattening the
// switch.
func (state *logicalAssignmentState) existence(expression *ast.Node) (*ast.Node, string, bool) {
	// Unwrapped on the way in, because `if ((a)) a = b` reaches here as a parenthesized expression
	// where upstream sees the identifier. Measured against the installed build: it reports and
	// fixes to `a &&= b`, so the parentheses are transparent to the existence question.
	unwrapped := logicalAssignmentUnwrapParentheses(expression)
	if unwrapped == nil {
		return nil, "", false
	}

	isNegated := logicalAssignmentIsLogicalNot(unwrapped)
	base := unwrapped
	if isNegated {
		base = unwrapped.AsPrefixUnaryExpression().Operand
	}

	if logicalAssignmentIsReference(base) {
		if isNegated {
			return base, "||", true
		}
		return base, "&&", true
	}

	// `!!a`, which is a negation whose operand is itself a negation over a reference. Upstream
	// answers `&&` unconditionally here rather than consulting the outer negation, because the two
	// cancel.
	if logicalAssignmentIsLogicalNot(base) {
		inner := base.AsPrefixUnaryExpression().Operand
		if logicalAssignmentIsReference(inner) {
			return inner, "&&", true
		}
	}

	if argument, isCast := state.booleanCastArgument(base); isCast {
		if logicalAssignmentIsReference(argument) {
			if isNegated {
				return argument, "||", true
			}
			return argument, "&&", true
		}
	}

	// The two nullish spellings are read off the original expression rather than off `base`,
	// matching upstream, so a negated nullish comparison falls through to silence.
	if reference, found := state.implicitNullishComparison(unwrapped); found {
		return reference, "??", true
	}
	if reference, found := state.explicitNullishComparison(unwrapped); found {
		return reference, "??", true
	}
	return nil, "", false
}

// booleanCastArgument is upstream's `isBooleanCast`: a one-argument call to the global `Boolean`.
//
// The callee must resolve to the global rather than to a local of the same name, which upstream
// asks its scope analysis and this asks the same way `isUndefined` does, by the complement.
func (state *logicalAssignmentState) booleanCastArgument(node *ast.Node) (*ast.Node, bool) {
	if node == nil || node.Kind != ast.KindCallExpression {
		return nil, false
	}
	call := node.AsCallExpression()
	callee := call.Expression
	if callee == nil || callee.Kind != ast.KindIdentifier || callee.Text() != "Boolean" {
		return nil, false
	}
	if call.Arguments == nil || len(call.Arguments.Nodes) != 1 {
		return nil, false
	}
	if state.isShadowedInThisFile(callee) {
		return nil, false
	}
	return call.Arguments.Nodes[0], true
}

// implicitNullishComparison is upstream's function of the same name: `a == null` or `a == void 0`,
// in either operand order.
//
// Loose equality is what makes one comparison catch both nullish values, which is why `==` is
// required and `===` falls to the explicit form below.
func (state *logicalAssignmentState) implicitNullishComparison(
	expression *ast.Node,
) (*ast.Node, bool) {
	if expression == nil || expression.Kind != ast.KindBinaryExpression {
		return nil, false
	}
	binary := expression.AsBinaryExpression()
	if binary.OperatorToken == nil || binary.OperatorToken.Kind != ast.KindEqualsEqualsToken {
		return nil, false
	}

	// Upstream picks the side that is a reference and reads the other as the nullish literal,
	// defaulting to left when neither is. That default is why `null == null` falls through: the
	// left is taken as the reference, and it fails the reference test.
	reference, nullish := binary.Left, binary.Right
	if !logicalAssignmentIsReference(reference) {
		reference, nullish = binary.Right, binary.Left
	}
	if !logicalAssignmentIsReference(reference) {
		return nil, false
	}
	if logicalAssignmentIsNullLiteral(nullish) || state.isUndefined(nullish) {
		return reference, true
	}
	return nil, false
}

// explicitNullishComparison is upstream's function of the same name:
// `a === null || a === undefined`, in either order and with either operand order inside each half.
func (state *logicalAssignmentState) explicitNullishComparison(
	expression *ast.Node,
) (*ast.Node, bool) {
	if !logicalAssignmentIsDoubleComparison(expression) {
		return nil, false
	}
	outer := expression.AsBinaryExpression()
	leftComparison := outer.Left.AsBinaryExpression()
	rightComparison := outer.Right.AsBinaryExpression()

	leftReference, leftNullish := leftComparison.Left, leftComparison.Right
	if !logicalAssignmentIsReference(leftReference) {
		leftReference, leftNullish = leftComparison.Right, leftComparison.Left
	}
	rightReference, rightNullish := rightComparison.Left, rightComparison.Right
	if !logicalAssignmentIsReference(rightReference) {
		rightReference, rightNullish = rightComparison.Right, rightComparison.Left
	}

	if !logicalAssignmentIsSameReference(leftReference, rightReference) {
		return nil, false
	}
	// One half must name `null` and the other `undefined`, in either order. Two halves naming the
	// same value are not a nullish test, which is what keeps `a === null || a === null` silent.
	if logicalAssignmentIsNullLiteral(leftNullish) && state.isUndefined(rightNullish) {
		return leftReference, true
	}
	if state.isUndefined(leftNullish) && logicalAssignmentIsNullLiteral(rightNullish) {
		return leftReference, true
	}
	return nil, false
}

// ifFix builds the third shape's repair: `if (a) a = b` becomes `a &&= b`.
//
// Four edits, plus a decline. The operator is written in front of the assignment's `=`, the `if`
// and its condition are deleted, anything after the body is deleted, and a semicolon is added when
// the braced form had none.
//
// The decline is an automatic semicolon insertion hazard and it is the subtlest part of this rule.
// Removing `if (a)` can leave the remaining expression glued to whatever preceded it: `fn()` on the
// line above followed by `if (a) (a) = b` becomes `fn()(a) &&= b`, which is a call rather than two
// statements. Upstream refuses when the previous token is neither `;` nor `{` and the body's first
// token is neither an identifier nor a keyword, because those are the shapes that cannot continue
// the previous expression. Four corpus cases pin it.
func (state *logicalAssignmentState) ifFix(
	node *ast.Node,
	ifStatement *ast.IfStatement,
	body *ast.Node,
	expression *ast.Node,
	hasBody bool,
	operator string,
) []rule.Fix {
	nodeRange := rule.TokenRange(state.ruleContext.SourceFile, node)
	if logicalAssignmentCommentsInside(state.ruleContext, nodeRange.Pos(), node.End()) {
		return nil
	}

	text := state.ruleContext.SourceFile.Text()
	bodyRange := rule.TokenRange(state.ruleContext.SourceFile, body)
	if logicalAssignmentIfFixWouldGlue(text, nodeRange.Pos(), bodyRange.Pos()) {
		return nil
	}

	assignment := expression.AsBinaryExpression()
	operatorRange := rule.TokenRange(state.ruleContext.SourceFile, assignment.OperatorToken)

	fixes := []rule.Fix{
		rule.ReplaceRange(core.NewTextRange(operatorRange.Pos(), operatorRange.Pos()), operator),
		rule.RemoveRange(core.NewTextRange(nodeRange.Pos(), bodyRange.Pos())),
		rule.RemoveRange(core.NewTextRange(body.End(), node.End())),
	}

	// The braced form swallows its own terminator, so `if (a) { a = b }` would become `a &&= b`
	// with nothing after it. Upstream re-adds the semicolon when the token after the assignment is
	// not already one.
	if hasBody && !logicalAssignmentEndsWithSemicolon(text, expression.End(), body.End()) {
		fixes = append(fixes, rule.ReplaceRange(core.NewTextRange(node.End(), node.End()), ";"))
	}
	return fixes
}

// prohibit is the `never` arm: every logical assignment reports and expands.
func (state *logicalAssignmentState) prohibit(node *ast.Node) {
	assignment := node.AsBinaryExpression()
	if assignment.OperatorToken == nil {
		return
	}
	longForm, isLogicalAssignment := logicalAssignmentLongForm[assignment.OperatorToken.Kind]
	if !isLogicalAssignment {
		return
	}
	operator := logicalAssignmentShorthand[assignment.OperatorToken.Kind]

	fixes := state.prohibitFix(node, assignment, longForm)
	state.report(node, logicalAssignmentUnexpectedMessage(operator),
		logicalAssignmentSeparateMessage(), fixes,
		state.cannotBeGetter(logicalAssignmentUnwrapParentheses(assignment.Left)))
}

// prohibitFix builds the `never` repair: `a ||= b` becomes `a = a || b`.
//
// The operator token is replaced with `=`, and the target text plus the logical operator is written
// after it. The target text is copied from the source rather than re-rendered, so an assertion or
// an annotation inside it survives.
//
// The right operand is parenthesized when it does not bind tighter than the new logical operator,
// or when mixing `??` with `||`/`&&`, which the grammar forbids without explicit grouping even
// though the two share a precedence. That second condition is why a precedence comparison alone is
// not enough: `a ??= b || c` expands to `a = a ?? (b || c)`, and without the parentheses the result
// does not parse.
func (state *logicalAssignmentState) prohibitFix(
	node *ast.Node,
	assignment *ast.BinaryExpression,
	longForm string,
) []rule.Fix {
	nodeRange := rule.TokenRange(state.ruleContext.SourceFile, node)
	if logicalAssignmentCommentsInside(state.ruleContext, nodeRange.Pos(), node.End()) {
		return nil
	}

	text := state.ruleContext.SourceFile.Text()
	operatorRange := rule.TokenRange(state.ruleContext.SourceFile, assignment.OperatorToken)
	// The target text is read off the unwrapped target, because upstream's
	// `sourceCode.getText(assignment.left)` reads a node whose parentheses live in `extra` rather
	// than in the node's own range. Measured against the installed build: `(a) ||= b` expands to
	// `(a) = a || b`, so the parentheses survive in place on the left and are not repeated in the
	// copy. Reading the wrapped node would write `(a) = (a) || b`, which is the same program and
	// the wrong text.
	target := logicalAssignmentUnwrapParentheses(assignment.Left)
	if target == nil {
		return nil
	}
	leftRange := rule.TokenRange(state.ruleContext.SourceFile, target)
	targetText := text[leftRange.Pos():target.End()]

	fixes := []rule.Fix{
		rule.ReplaceRange(operatorRange, "= "+targetText+" "+longForm),
	}

	right := assignment.Right
	if right == nil {
		return fixes
	}
	if right.Kind == ast.KindParenthesizedExpression {
		return fixes
	}

	needsParentheses := operatorAssignmentPrecedence(right) <=
		logicalAssignmentOperatorPrecedence(assignment.OperatorToken.Kind)
	if !needsParentheses && assignment.OperatorToken.Kind == ast.KindQuestionQuestionEqualsToken {
		// `??` cannot sit beside `||` or `&&` without grouping, which is a grammar rule rather than
		// a precedence one, so it is tested separately.
		needsParentheses = right.Kind == ast.KindBinaryExpression &&
			logicalAssignmentIsMixableLogical(right.AsBinaryExpression().OperatorToken.Kind)
	}
	if !needsParentheses {
		return fixes
	}

	rightRange := rule.TokenRange(state.ruleContext.SourceFile, right)
	return append(fixes,
		rule.ReplaceRange(core.NewTextRange(rightRange.Pos(), rightRange.Pos()), "("),
		rule.ReplaceRange(core.NewTextRange(right.End(), right.End()), ")"))
}
