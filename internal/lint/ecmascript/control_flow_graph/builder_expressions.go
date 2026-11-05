package control_flow_graph

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
)

func (builder *Builder[E]) expr(node *ast.Node) {
	if node == nil {
		return
	}
	if builder.hooks.Expression != nil {
		builder.hooks.Expression(builder, node)
	}

	switch node.Kind {
	case ast.KindIdentifier:
		builder.read(node)
		if isThrowableIdentifier(node) {
			builder.firstThrowableFork()
		}

	case ast.KindParenthesizedExpression:
		builder.expr(node.AsParenthesizedExpression().Expression)

	case ast.KindBinaryExpression:
		builder.binaryExpression(node)

	case ast.KindConditionalExpression:
		builder.conditionalExpression(node)

	case ast.KindPrefixUnaryExpression:
		unary := node.AsPrefixUnaryExpression()
		if unary.Operator == ast.KindPlusPlusToken || unary.Operator == ast.KindMinusMinusToken {
			builder.updateExpression(unary.Operand)
			return
		}
		builder.expr(unary.Operand)

	case ast.KindPostfixUnaryExpression:
		builder.updateExpression(node.AsPostfixUnaryExpression().Operand)

	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression,
		ast.KindCallExpression, ast.KindNewExpression, ast.KindTaggedTemplateExpression:
		builder.accessOrCall(node)

	case ast.KindYieldExpression:
		builder.expr(node.AsYieldExpression().Expression)
		builder.makeYield()

	case ast.KindClassExpression:
		builder.classLike(node)

	default:
		if IsRoot(node) {
			builder.nestedFunction(node)
			return
		}
		node.ForEachChild(func(child *ast.Node) bool {
			builder.visitUnknown(child)
			return false
		})
	}
}

// condition evaluates node while keeping its truthy and falsy continuations
// separate. A plain expr may merge short-circuit branches because its value is
// all the consumer needs; a control-flow condition cannot merge them before it
// decides which statement executes, or impossible paths can appear across the
// join (for example, the short branch of `a || b` reaching an `else` body).
func (builder *Builder[E]) condition(node *ast.Node, whenTrue, whenFalse *Block[E]) {
	if node == nil {
		builder.link(builder.current, whenTrue)
		builder.link(builder.current, whenFalse)
		return
	}

	switch node.Kind {
	case ast.KindParenthesizedExpression:
		if builder.hooks.Expression != nil {
			builder.hooks.Expression(builder, node)
		}
		builder.condition(node.AsParenthesizedExpression().Expression, whenTrue, whenFalse)

	case ast.KindPrefixUnaryExpression:
		unary := node.AsPrefixUnaryExpression()
		if unary.Operator != ast.KindExclamationToken {
			builder.expr(node)
			builder.link(builder.current, whenTrue)
			builder.link(builder.current, whenFalse)
			return
		}
		if builder.hooks.Expression != nil {
			builder.hooks.Expression(builder, node)
		}
		builder.condition(unary.Operand, whenFalse, whenTrue)

	case ast.KindBinaryExpression:
		binary := node.AsBinaryExpression()
		operator := binary.OperatorToken.Kind
		if operator != ast.KindAmpersandAmpersandToken &&
			operator != ast.KindBarBarToken &&
			operator != ast.KindQuestionQuestionToken {
			builder.expr(node)
			builder.link(builder.current, whenTrue)
			builder.link(builder.current, whenFalse)
			return
		}
		if builder.hooks.Expression != nil {
			builder.hooks.Expression(builder, node)
		}
		right := builder.newBlock()
		switch operator {
		case ast.KindAmpersandAmpersandToken:
			builder.condition(binary.Left, right, whenFalse)
		case ast.KindBarBarToken:
			builder.condition(binary.Left, whenTrue, right)
		case ast.KindQuestionQuestionToken:
			// A non-nullish left operand may itself be truthy or falsy; a
			// nullish one evaluates the right operand.
			builder.expr(binary.Left)
			builder.link(builder.current, whenTrue)
			builder.link(builder.current, whenFalse)
			builder.link(builder.current, right)
		}
		builder.enter(right)
		builder.condition(binary.Right, whenTrue, whenFalse)

	case ast.KindConditionalExpression:
		if builder.hooks.Expression != nil {
			builder.hooks.Expression(builder, node)
		}
		conditional := node.AsConditionalExpression()
		trueEntry := builder.newBlock()
		falseEntry := builder.newBlock()
		builder.condition(conditional.Condition, trueEntry, falseEntry)
		builder.enter(trueEntry)
		builder.condition(conditional.WhenTrue, whenTrue, whenFalse)
		builder.enter(falseEntry)
		builder.condition(conditional.WhenFalse, whenTrue, whenFalse)

	default:
		builder.expr(node)
		builder.link(builder.current, whenTrue)
		builder.link(builder.current, whenFalse)
	}
}

// visitUnknown routes a child of a node the builder has no dedicated handler
// for. Statements and expressions are handled by their own walkers so control
// flow inside them is still modelled.
func (builder *Builder[E]) visitUnknown(node *ast.Node) {
	if node == nil {
		return
	}
	if ast.IsStatement(node) {
		builder.statement(node)
		return
	}
	builder.expr(node)
}

func (builder *Builder[E]) binaryExpression(node *ast.Node) {
	binary := node.AsBinaryExpression()
	operator := binary.OperatorToken.Kind

	switch {
	case operator == ast.KindAmpersandAmpersandToken ||
		operator == ast.KindBarBarToken ||
		operator == ast.KindQuestionQuestionToken:
		builder.expr(binary.Left)
		join := builder.newBlock()
		builder.link(builder.current, join)
		right := builder.newBlock()
		builder.link(builder.current, right)
		builder.enter(right)
		builder.expr(binary.Right)
		builder.link(builder.current, join)
		builder.enter(join)

	case ast.IsLogicalOrCoalescingAssignmentOperator(operator):
		// `a ||= b` reads a, then conditionally evaluates b and writes a.
		builder.patternReads(binary.Left)
		join := builder.newBlock()
		builder.link(builder.current, join)
		right := builder.newBlock()
		builder.link(builder.current, right)
		builder.enter(right)
		builder.expr(binary.Right)
		builder.patternWrites(binary.Left)
		builder.link(builder.current, join)
		builder.enter(join)

	case operator == ast.KindEqualsToken:
		if isDestructuringTarget(binary.Left) {
			// Run-time evaluation order: the right-hand side produces the
			// value first, then the pattern binds element by element.
			builder.expr(binary.Right)
			builder.patternBind(binary.Left)
			return
		}
		builder.patternReads(binary.Left)
		builder.expr(binary.Right)
		builder.patternWrites(binary.Left)

	case ast.IsAssignmentOperator(operator):
		// Compound assignment reads the target before writing it.
		builder.patternReads(binary.Left)
		builder.expr(binary.Right)
		builder.patternWrites(binary.Left)

	default:
		builder.expr(binary.Left)
		builder.expr(binary.Right)
	}
}

func (builder *Builder[E]) conditionalExpression(node *ast.Node) {
	cond := node.AsConditionalExpression()
	builder.expr(cond.Condition)
	testEnd := builder.current
	join := builder.newBlock()

	whenTrue := builder.newBlock()
	builder.link(testEnd, whenTrue)
	builder.enter(whenTrue)
	builder.expr(cond.WhenTrue)
	builder.link(builder.current, join)

	whenFalse := builder.newBlock()
	builder.link(testEnd, whenFalse)
	builder.enter(whenFalse)
	builder.expr(cond.WhenFalse)
	builder.link(builder.current, join)

	builder.enter(join)
}

func (builder *Builder[E]) updateExpression(operand *ast.Node) {
	builder.patternReads(operand)
	builder.patternWrites(operand)
}

// accessOrCall walks a member access or call and forks around each `?.` link so
// the rest of the chain can be skipped.
func (builder *Builder[E]) accessOrCall(node *ast.Node) {
	outermost := ast.IsOptionalChain(node) && ast.IsOutermostOptionalChain(node)
	if outermost {
		builder.chainJoins = append(builder.chainJoins, builder.newBlock())
	}

	switch node.Kind {
	case ast.KindPropertyAccessExpression:
		builder.expr(node.AsPropertyAccessExpression().Expression)
		builder.forkOptionalChain(node)
	case ast.KindElementAccessExpression:
		access := node.AsElementAccessExpression()
		builder.expr(access.Expression)
		builder.forkOptionalChain(node)
		builder.expr(access.ArgumentExpression)
	case ast.KindCallExpression:
		call := node.AsCallExpression()
		builder.expr(call.Expression)
		builder.forkOptionalChain(node)
		builder.typeArguments(node)
		if call.Arguments != nil {
			for _, argument := range call.Arguments.Nodes {
				builder.expr(argument)
			}
		}
	case ast.KindNewExpression:
		newExpr := node.AsNewExpression()
		builder.expr(newExpr.Expression)
		builder.typeArguments(node)
		if newExpr.Arguments != nil {
			for _, argument := range newExpr.Arguments.Nodes {
				builder.expr(argument)
			}
		}
	case ast.KindTaggedTemplateExpression:
		tagged := node.AsTaggedTemplateExpression()
		builder.expr(tagged.Tag)
		builder.typeArguments(node)
		builder.expr(tagged.Template)
	}

	builder.firstThrowableFork()

	if outermost {
		join := builder.chainJoins[len(builder.chainJoins)-1]
		builder.chainJoins = builder.chainJoins[:len(builder.chainJoins)-1]
		builder.link(builder.current, join)
		builder.enter(join)
	}
}

func (builder *Builder[E]) forkOptionalChain(node *ast.Node) {
	if len(builder.chainJoins) == 0 || !ast.IsOptionalChainRoot(node) {
		return
	}
	builder.link(builder.current, builder.chainJoins[len(builder.chainJoins)-1])
	next := builder.newBlock()
	builder.link(builder.current, next)
	builder.enter(next)
}

// typeArguments walks an explicit `<T>` argument list, so a `typeof x` inside
// it still counts as a read even though nothing in it runs.
func (builder *Builder[E]) typeArguments(node *ast.Node) {
	for _, typeArgument := range node.TypeArguments() {
		builder.expr(typeArgument)
	}
}

// typeParameters walks a `<T extends U = D>` declaration list, so a `typeof x`
// inside a constraint or default still counts as a read even though nothing in
// it runs.
func (builder *Builder[E]) typeParameters(node *ast.Node) {
	for _, typeParameter := range node.TypeParameters() {
		parameter := typeParameter.AsTypeParameterDeclaration()
		if parameter == nil {
			continue
		}
		builder.expr(parameter.Constraint)
		builder.expr(parameter.DefaultType)
	}
}

// nestedFunction keeps evaluating the parts of a nested function that belong to
// the enclosing code path (a computed member name and decorators) while its
// parameters and body form their own code path root.
func (builder *Builder[E]) nestedFunction(node *ast.Node) {
	builder.memberHeader(node)
}

func (builder *Builder[E]) classLike(node *ast.Node) {
	class := node.ClassLikeData()
	if class == nil {
		return
	}
	builder.decorators(node)
	builder.typeParameters(node)
	if class.HeritageClauses != nil {
		for _, clause := range class.HeritageClauses.Nodes {
			heritage := clause.AsHeritageClause()
			if heritage == nil || heritage.Types == nil {
				continue
			}
			for _, typeNode := range heritage.Types.Nodes {
				builder.expr(typeNode)
			}
		}
	}
	if class.Members != nil {
		for _, member := range class.Members.Nodes {
			builder.memberHeader(member)
		}
	}
}

// memberHeader evaluates the parts of a class or object member that belong to
// the enclosing code path: its decorators, its computed key, and its type
// annotation. A member with a body of its own (a method, accessor, or
// property initializer) forms its own code path, so its parameters and
// return type are walked there instead; an index signature has no body, so
// its parameter and return types are walked here.
func (builder *Builder[E]) memberHeader(node *ast.Node) {
	builder.decorators(node)
	if ast.IsFunctionLikeDeclaration(node) {
		for _, parameter := range node.Parameters() {
			builder.decorators(parameter)
		}
	}
	name := node.Name()
	if name != nil && name.Kind == ast.KindComputedPropertyName {
		builder.expr(name.AsComputedPropertyName().Expression)
	}
	switch node.Kind {
	case ast.KindPropertyDeclaration:
		builder.expr(node.AsPropertyDeclaration().Type)
	case ast.KindIndexSignature:
		for _, parameter := range node.Parameters() {
			builder.expr(parameter.Type())
		}
		builder.expr(node.Type())
	}
}

func (builder *Builder[E]) decorators(node *ast.Node) {
	for _, decorator := range node.Decorators() {
		builder.expr(decorator.AsDecorator().Expression)
	}
}
