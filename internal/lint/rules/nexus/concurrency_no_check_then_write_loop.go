package nexus

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/ecmascript/reference"
	"github.com/system-inc/cohere/internal/lint/ecmascript/scope"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// concurrencyNoCheckThenWriteLoop is the analysis of one loop.
type concurrencyNoCheckThenWriteLoop struct {
	ruleContext rule.Context
	loop        *ast.Node
	// function is the function-like node holding the loop, or nil at module scope.
	function *ast.Node
	// check is the existence check the loop exits on, and checkStatement the `try` holding it, or nil
	// when the check is the loop's condition.
	check          *ast.Node
	checkStatement *ast.Node
	// bodyDeclarations are the `const` statements in the loop body before the `try`, the only ones
	// inside the loop that may be read through.
	bodyDeclarations map[*ast.Node]bool
	varying          map[*ast.Symbol]bool
	writes           map[*ast.Symbol][]*ast.Node
}

// findCheck finds the existence check the loop exits on, in either of the two shapes the doc comment
// names.
func (analysis *concurrencyNoCheckThenWriteLoop) findCheck() bool {
	var condition *ast.Node
	var body *ast.Node
	switch analysis.loop.Kind {
	case ast.KindWhileStatement:
		condition = analysis.loop.AsWhileStatement().Expression
		body = analysis.loop.AsWhileStatement().Statement
	case ast.KindDoStatement:
		condition = analysis.loop.AsDoStatement().Expression
		body = analysis.loop.AsDoStatement().Statement
	case ast.KindForStatement:
		condition = analysis.loop.AsForStatement().Condition
		body = analysis.loop.AsForStatement().Statement
	}
	if condition != nil {
		call := ast.SkipParentheses(condition)
		if name, promise, matched := analysis.nodeFileSystemFunction(
			call,
		); matched && !promise && name == "existsSync" &&
			concurrencyNoCheckThenWriteHasOneArgument(call) {
			analysis.check = call
			return true
		}
	}
	if body == nil || body.Kind != ast.KindBlock {
		return false
	}
	statements := body.AsBlock().Statements.Nodes
	for index, statement := range statements {
		if check := analysis.throwingCheckOf(statement); check != nil {
			analysis.check = check
			analysis.checkStatement = statement
			analysis.bodyDeclarations = map[*ast.Node]bool{}
			for _, earlier := range statements[:index] {
				if earlier.Kind == ast.KindVariableStatement {
					analysis.bodyDeclarations[earlier] = true
				}
			}
			return true
		}
	}
	return false
}

// throwingCheckOf returns the check in `try { check; counter++; } catch { break; }`, or nil when the
// statement is not exactly that shape.
func (analysis *concurrencyNoCheckThenWriteLoop) throwingCheckOf(statement *ast.Node) *ast.Node {
	if statement.Kind != ast.KindTryStatement {
		return nil
	}
	tryStatement := statement.AsTryStatement()
	if tryStatement.FinallyBlock != nil || tryStatement.CatchClause == nil {
		return nil
	}
	catchStatements := tryStatement.CatchClause.AsCatchClause().Block.AsBlock().Statements.Nodes
	if len(catchStatements) != 1 || catchStatements[0].Kind != ast.KindBreakStatement ||
		catchStatements[0].AsBreakStatement().Label != nil {
		return nil
	}
	tryStatements := tryStatement.TryBlock.AsBlock().Statements.Nodes
	if len(tryStatements) == 0 || tryStatements[0].Kind != ast.KindExpressionStatement {
		return nil
	}
	for _, following := range tryStatements[1:] {
		if following.Kind != ast.KindExpressionStatement ||
			concurrencyNoCheckThenWriteCounterOf(following.AsExpressionStatement().Expression) == nil {
			return nil
		}
	}
	call := ast.SkipParentheses(tryStatements[0].AsExpressionStatement().Expression)
	awaited := call.Kind == ast.KindAwaitExpression
	if awaited {
		call = ast.SkipParentheses(call.AsAwaitExpression().Expression)
	}
	name, promise, matched := analysis.nodeFileSystemFunction(call)
	if !matched || promise != awaited || !concurrencyNoCheckThenWriteThrowingChecks[promise][name] ||
		!concurrencyNoCheckThenWriteHasOneArgument(call) {
		return nil
	}
	return call
}

// resolve returns the symbol an identifier names, aliases followed.
func (analysis *concurrencyNoCheckThenWriteLoop) resolve(node *ast.Node) *ast.Symbol {
	symbol := analysis.ruleContext.TypeChecker.GetSymbolAtLocation(node)
	if symbol == nil {
		return nil
	}
	return checker.SkipAlias(symbol, analysis.ruleContext.TypeChecker)
}

// calleeSymbol returns the symbol a call's callee resolves to, for a callee written as a name or a
// non-optional property access, and nil for anything else.
func (analysis *concurrencyNoCheckThenWriteLoop) calleeSymbol(call *ast.Node) *ast.Symbol {
	if call == nil || call.Kind != ast.KindCallExpression || call.AsCallExpression().QuestionDotToken != nil {
		return nil
	}
	callee := ast.SkipParentheses(call.AsCallExpression().Expression)
	switch callee.Kind {
	case ast.KindIdentifier:
		return analysis.resolve(callee)
	case ast.KindPropertyAccessExpression:
		if callee.AsPropertyAccessExpression().QuestionDotToken != nil {
			return nil
		}
		return analysis.resolve(callee.AsPropertyAccessExpression().Name())
	}
	return nil
}

// nodeFileSystemFunction returns the name of the Node file system function a call resolves to and
// whether it is the promise API.
func (analysis *concurrencyNoCheckThenWriteLoop) nodeFileSystemFunction(call *ast.Node) (string, bool, bool) {
	symbol := analysis.calleeSymbol(call)
	module, matched := concurrencyNoCheckThenWriteDeclaredIn(symbol, concurrencyNoCheckThenWriteNodeModules)
	if !matched {
		return "", false, false
	}
	return symbol.Name, concurrencyNoCheckThenWriteNodeModules[module], true
}

// isPathJoin says whether a call is Node's `path.join`, which is pure: the same arguments give the
// same path.
func (analysis *concurrencyNoCheckThenWriteLoop) isPathJoin(call *ast.Node) (*ast.Symbol, bool) {
	symbol := analysis.calleeSymbol(call)
	if _, matched := concurrencyNoCheckThenWriteDeclaredIn(
		symbol,
		concurrencyNoCheckThenWritePathModules,
	); !matched || symbol.Name != "join" {
		return nil, false
	}
	return symbol, true
}

// isSharpToFile says whether a call is sharp's `toFile`: the method resolves to a declaration in the
// file the compiler resolved this file's own import of the package `sharp` to.
func (analysis *concurrencyNoCheckThenWriteLoop) isSharpToFile(call *ast.Node) bool {
	symbol := analysis.calleeSymbol(call)
	if symbol == nil || symbol.Name != "toFile" || len(symbol.Declarations) == 0 {
		return false
	}
	sharpFiles := concurrencyNoCheckThenWriteSharpFiles(analysis.ruleContext)
	for _, declaration := range symbol.Declarations {
		if declaration.Kind != ast.KindMethodSignature ||
			!sharpFiles[ast.GetSourceFileOfNode(declaration).FileName().AsString()] {
			return false
		}
	}
	return true
}

// readsAny says whether an expression reads, by identifier, any binding in a
// set.
func (analysis *concurrencyNoCheckThenWriteLoop) readsAny(expression *ast.Node, symbols map[*ast.Symbol]bool) bool {
	found := false
	concurrencyNoCheckThenWriteForEachOwnNode(expression, func(node *ast.Node) {
		if !found && node.Kind == ast.KindIdentifier {
			if symbol := analysis.resolve(node); symbol != nil && symbols[symbol] {
				found = true
			}
		}
	})
	return found
}

// collectVarying finds the loop's counters, the variables it changes with `++`, `--`, `+=` or `-=`,
// and every variable assigned or declared in the loop from one of them.
func (analysis *concurrencyNoCheckThenWriteLoop) collectVarying() {
	analysis.varying = map[*ast.Symbol]bool{}
	type assignment struct {
		target *ast.Symbol
		value  *ast.Node
	}
	var assignments []assignment
	concurrencyNoCheckThenWriteForEachOwnNode(analysis.loop, func(node *ast.Node) {
		if counter := concurrencyNoCheckThenWriteCounterOf(node); counter != nil {
			if symbol := analysis.resolve(counter); symbol != nil {
				analysis.varying[symbol] = true
			}
			return
		}
		switch node.Kind {
		case ast.KindBinaryExpression:
			binary := node.AsBinaryExpression()
			left := ast.SkipParentheses(binary.Left)
			if binary.OperatorToken.Kind == ast.KindEqualsToken && left.Kind == ast.KindIdentifier {
				if symbol := analysis.resolve(left); symbol != nil {
					assignments = append(assignments, assignment{target: symbol, value: binary.Right})
				}
			}
		case ast.KindVariableDeclaration:
			declaration := node.AsVariableDeclaration()
			if declaration.Name().Kind == ast.KindIdentifier && declaration.Initializer != nil {
				if symbol := analysis.resolve(declaration.Name()); symbol != nil {
					assignments = append(assignments, assignment{target: symbol, value: declaration.Initializer})
				}
			}
		}
	})
	for changed := true; changed; {
		changed = false
		for _, candidate := range assignments {
			if !analysis.varying[candidate.target] && analysis.readsAny(candidate.value, analysis.varying) {
				analysis.varying[candidate.target] = true
				changed = true
			}
		}
	}
}

// readsVarying says whether the checked path reads a counter, through any `const` the comparison is
// allowed to read through.
func (analysis *concurrencyNoCheckThenWriteLoop) readsVarying(path *ast.Node) bool {
	found := false
	visited := map[*ast.Symbol]bool{}
	var visit func(node *ast.Node)
	visit = func(node *ast.Node) {
		concurrencyNoCheckThenWriteForEachOwnNode(node, func(inner *ast.Node) {
			if found || inner.Kind != ast.KindIdentifier {
				return
			}
			symbol := analysis.resolve(inner)
			if symbol == nil || visited[symbol] {
				return
			}
			visited[symbol] = true
			if analysis.varying[symbol] {
				found = true
				return
			}
			if initializer, _ := analysis.readThrough(symbol); initializer != nil {
				visit(initializer)
			}
		})
	}
	visit(path)
	return found
}

// followingStatements lists the statements that run after the loop in the same function, climbing out
// through blocks, branches and `try` but stopping at an enclosing loop or function.
func (analysis *concurrencyNoCheckThenWriteLoop) followingStatements() []*ast.Node {
	var following []*ast.Node
	current := analysis.loop
	for current.Parent != nil {
		parent := current.Parent
		if ast.IsFunctionLike(parent) || ast.IsIterationStatement(parent, false) ||
			parent.Kind == ast.KindClassStaticBlockDeclaration || parent.Kind == ast.KindModuleBlock {
			break
		}
		var statements []*ast.Node
		switch parent.Kind {
		case ast.KindBlock:
			statements = parent.AsBlock().Statements.Nodes
		case ast.KindCaseClause, ast.KindDefaultClause:
			statements = parent.AsCaseOrDefaultClause().Statements.Nodes
		case ast.KindSourceFile:
			statements = parent.AsSourceFile().Statements.Nodes
		}
		for index, statement := range statements {
			if statement == current {
				following = append(following, statements[index+1:]...)
				break
			}
		}
		current = parent
	}
	return following
}

// findWrite returns the first write in a statement that creates the checked path without exclusive
// create, with the name to show for it.
func (analysis *concurrencyNoCheckThenWriteLoop) findWrite(
	statement *ast.Node,
	checkedPath *ast.Node,
) (*ast.Node, string) {
	var found *ast.Node
	var name string
	concurrencyNoCheckThenWriteForEachOwnNode(statement, func(node *ast.Node) {
		if found != nil || node.Kind != ast.KindCallExpression {
			return
		}
		destination, writeName := analysis.nonExclusiveDestination(node)
		if destination == nil {
			return
		}
		if analysis.samePathAt(checkedPath, destination, node) {
			found = node
			name = writeName
		}
	})
	return found, name
}

// nonExclusiveDestination returns the path a call creates or replaces when it is a write the rule
// knows and its source proves it is not exclusive, with the name to show for it.
func (analysis *concurrencyNoCheckThenWriteLoop) nonExclusiveDestination(call *ast.Node) (*ast.Node, string) {
	arguments := call.AsCallExpression().Arguments.Nodes
	for _, argument := range arguments {
		if argument.Kind == ast.KindSpreadElement {
			return nil, ""
		}
	}
	if analysis.isSharpToFile(call) {
		if len(arguments) == 0 {
			return nil, ""
		}
		return arguments[0], "toFile"
	}
	name, _, matched := analysis.nodeFileSystemFunction(call)
	if !matched {
		return nil, ""
	}
	write, known := concurrencyNoCheckThenWriteWrites[name]
	if !known || len(arguments) <= write.destinationIndex {
		return nil, ""
	}
	if write.optionsIndex >= 0 && len(arguments) > write.optionsIndex &&
		!concurrencyNoCheckThenWriteOptionsNotExclusive(arguments[write.optionsIndex], write.flagKey) {
		return nil, ""
	}
	return arguments[write.destinationIndex], name
}

// readThrough returns the initializer of a `const` the comparison may read through, and, for one
// declared in the loop body, the position of its statement.
func (analysis *concurrencyNoCheckThenWriteLoop) readThrough(symbol *ast.Symbol) (*ast.Node, int) {
	declaration := symbol.ValueDeclaration
	if declaration == nil || declaration.Kind != ast.KindVariableDeclaration || !ast.IsVarConst(declaration) {
		return nil, -1
	}
	variable := declaration.AsVariableDeclaration()
	if variable.Name().Kind != ast.KindIdentifier || variable.Initializer == nil ||
		ast.GetSourceFileOfNode(declaration) != analysis.ruleContext.SourceFile ||
		scope.EnclosingFunctionLike(declaration) != analysis.function {
		return nil, -1
	}
	if declaration.Pos() >= analysis.loop.End() {
		return variable.Initializer, -1
	}
	statement := declaration.Parent.Parent
	if analysis.bodyDeclarations[statement] {
		return variable.Initializer, statement.Pos()
	}
	return nil, -1
}

// samePathAt says whether a write's destination is proven to be the checked path when the write runs.
func (analysis *concurrencyNoCheckThenWriteLoop) samePathAt(
	checkedPath *ast.Node,
	destination *ast.Node,
	write *ast.Node,
) bool {
	comparison := &concurrencyNoCheckThenWriteComparison{bodyDeclarationStart: -1, bindings: map[*ast.Symbol]bool{}}
	if !analysis.same(comparison, checkedPath, destination) {
		return false
	}
	for symbol := range comparison.bindings {
		if !analysis.holdsStill(symbol, comparison.bodyDeclarationStart, write) {
			return false
		}
	}
	return true
}

// expand reads an identifier through a `const` the comparison may read through, recording a loop-body
// declaration's position, and returns the node to compare in its place.
//
// Mutates: comparison the walk lowers its bodyDeclarationStart to the earliest loop-body declaration it
// reads through, so one comparison carries the whole proof's earliest point
func (analysis *concurrencyNoCheckThenWriteLoop) expand(
	comparison *concurrencyNoCheckThenWriteComparison,
	node *ast.Node,
) *ast.Node {
	current := node
	for depth := 0; depth < 16; depth++ {
		current = ast.SkipParentheses(current)
		if current.Kind != ast.KindIdentifier {
			return current
		}
		symbol := analysis.resolve(current)
		if symbol == nil {
			return current
		}
		initializer, bodyPosition := analysis.readThrough(symbol)
		if initializer == nil {
			return current
		}
		if bodyPosition >= 0 &&
			(comparison.bodyDeclarationStart < 0 || bodyPosition < comparison.bodyDeclarationStart) {
			comparison.bodyDeclarationStart = bodyPosition
		}
		current = initializer
	}
	return current
}

// same compares two path expressions as the doc comment's "Same path, proven" describes.
func (analysis *concurrencyNoCheckThenWriteLoop) same(
	comparison *concurrencyNoCheckThenWriteComparison,
	leftPath *ast.Node,
	rightPath *ast.Node,
) bool {
	left := analysis.expand(comparison, leftPath)
	right := analysis.expand(comparison, rightPath)
	if left.Kind != right.Kind {
		return false
	}
	switch left.Kind {
	case ast.KindIdentifier:
		leftSymbol := analysis.resolve(left)
		if leftSymbol == nil || leftSymbol != analysis.resolve(right) {
			return false
		}
		if !concurrencyNoCheckThenWriteIsConstant(leftSymbol) {
			comparison.bindings[leftSymbol] = true
		}
		return true
	case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral, ast.KindNumericLiteral:
		return left.Text() == right.Text()
	case ast.KindTemplateExpression:
		leftTemplate := left.AsTemplateExpression()
		rightTemplate := right.AsTemplateExpression()
		if leftTemplate.Head.Text() != rightTemplate.Head.Text() ||
			len(leftTemplate.TemplateSpans.Nodes) != len(rightTemplate.TemplateSpans.Nodes) {
			return false
		}
		for index, leftSpan := range leftTemplate.TemplateSpans.Nodes {
			rightSpan := rightTemplate.TemplateSpans.Nodes[index]
			if leftSpan.AsTemplateSpan().Literal.Text() != rightSpan.AsTemplateSpan().Literal.Text() ||
				!analysis.same(
					comparison,
					leftSpan.AsTemplateSpan().Expression,
					rightSpan.AsTemplateSpan().Expression,
				) {
				return false
			}
		}
		return true
	case ast.KindBinaryExpression:
		leftBinary := left.AsBinaryExpression()
		rightBinary := right.AsBinaryExpression()
		return leftBinary.OperatorToken.Kind == ast.KindPlusToken &&
			rightBinary.OperatorToken.Kind == ast.KindPlusToken &&
			analysis.same(comparison, leftBinary.Left, rightBinary.Left) &&
			analysis.same(comparison, leftBinary.Right, rightBinary.Right)
	case ast.KindCallExpression:
		leftJoin, leftIsJoin := analysis.isPathJoin(left)
		rightJoin, rightIsJoin := analysis.isPathJoin(right)
		if !leftIsJoin || !rightIsJoin || leftJoin != rightJoin {
			return false
		}
		leftArguments := left.AsCallExpression().Arguments.Nodes
		rightArguments := right.AsCallExpression().Arguments.Nodes
		if len(leftArguments) != len(rightArguments) {
			return false
		}
		for index := range leftArguments {
			if leftArguments[index].Kind == ast.KindSpreadElement ||
				rightArguments[index].Kind == ast.KindSpreadElement ||
				!analysis.same(comparison, leftArguments[index], rightArguments[index]) {
				return false
			}
		}
		return true
	}
	return false
}

// holdsStill says whether a `let`, `var` or parameter keeps the value the check saw until the write
// runs: it is declared in this file, every write to it is in the loop's own function, none from a nested one, and none
// between the end of the loop and the write, nor between a loop-body `const` read through and the check.
func (analysis *concurrencyNoCheckThenWriteLoop) holdsStill(
	symbol *ast.Symbol,
	bodyDeclarationStart int,
	write *ast.Node,
) bool {
	declaration := symbol.ValueDeclaration
	if declaration == nil || ast.GetSourceFileOfNode(declaration) != analysis.ruleContext.SourceFile {
		// Another module's `let` can change while this function waits, and its writes are not in
		// this file to read.
		return false
	}
	if analysis.writes == nil {
		analysis.writes = map[*ast.Symbol][]*ast.Node{}
	}
	writes, cached := analysis.writes[symbol]
	if !cached {
		root := scope.EnclosingFunctionLike(declaration)
		if root == nil {
			root = analysis.ruleContext.SourceFile.AsNode()
		}
		var visit func(node *ast.Node) bool
		visit = func(node *ast.Node) bool {
			if node.Kind == ast.KindIdentifier && !ast.IsDeclarationName(node) &&
				reference.WritesToBinding(node) && analysis.resolve(node) == symbol {
				writes = append(writes, node)
			}
			node.ForEachChild(visit)
			return false
		}
		visit(root)
		analysis.writes[symbol] = writes
	}
	for _, written := range writes {
		if scope.EnclosingFunctionLike(written) != analysis.function {
			return false
		}
		if written.Pos() >= analysis.loop.End() && written.Pos() < write.Pos() {
			return false
		}
		if bodyDeclarationStart >= 0 &&
			written.Pos() >= bodyDeclarationStart &&
			written.Pos() < analysis.checkStatement.Pos() {
			return false
		}
	}
	return true
}
