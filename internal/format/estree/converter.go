package estree

import (
	"fmt"
	"sort"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/system-inc/cohere/internal/format/printing"
	ecmascripttext "github.com/system-inc/cohere/internal/lint/ecmascript/text"
)

// Converter turns one typescript-go source file into an ESTree Program.
type Converter struct {
	sourceFile   *ast.SourceFile
	text         string
	allowPattern bool
	scan         *scanner.Scanner
	unsupported  error

	// invalid is the first syntax error upstream's check-syntax-errors.js throws, which refuses the file the
	// way a parse error does: see checkSyntaxError.
	invalid error

	// typeCastCommentEnds are the ends of the file's type cast comments, in order, when converting
	// for the babel parser (ConvertForBabel). Nil converts for typescript.
	typeCastCommentEnds []int

	// nodes is where the tree's nodes come from, or nil for the heap.
	nodes *Arena
}

// followsTypeCastComment is postprocess's test for keeping a ParenthesizedExpression: the last type
// cast comment that ends at or before start is separated from it by whitespace alone.
func (converter *Converter) followsTypeCastComment(start int) bool {
	ends := converter.typeCastCommentEnds
	index := sort.SearchInts(ends, start+1) - 1
	return index >= 0 && TrimJavaScript(converter.text[ends[index]:start]) == ""
}

// getStart is node.getStart(ast): the node's start after its leading trivia.
func (converter *Converter) getStart(node *ast.Node) int {
	return scanner.GetTokenPosOfNode(node, converter.sourceFile, false)
}

// getRange is upstream's getRange.
func (converter *Converter) getRange(node *ast.Node) [2]int {
	return [2]int{converter.getStart(node), node.End()}
}

// getText is node.getText().
func (converter *Converter) getText(node *ast.Node) string {
	return converter.text[converter.getStart(node):node.End()]
}

// tokenAt scans the token whose full start is pos, which is upstream's findNextToken: the token
// starting at the end of the previous one, trivia included.
func (converter *Converter) tokenAt(position int) token {
	converter.scan.ResetPos(position)
	kind := converter.scan.Scan()
	return token{kind: kind, start: converter.scan.TokenStart(), end: converter.scan.TokenEnd()}
}

// firstToken is node.getFirstToken().
func (converter *Converter) firstToken(node *ast.Node) token {
	return converter.tokenAt(converter.getStart(node))
}

// createNode is upstream's createNode: the range defaults to the TypeScript node's.
func (converter *Converter) createNode(node *ast.Node, nodeType string, properties []Property) *Node {
	nodeRange := converter.getRange(node)
	return converter.nodes.node(nodeType, nodeRange[0], nodeRange[1], properties)
}

// createNodeWithRange is createNode with an explicit range, upstream's `range: [...]` in the data.
func (converter *Converter) createNodeWithRange(nodeType string, nodeRange [2]int, properties []Property) *Node {
	return converter.nodes.node(nodeType, nodeRange[0], nodeRange[1], properties)
}

// convertChild is upstream's convertChild: patterns are not allowed below it.
func (converter *Converter) convertChild(node *ast.Node, parent *ast.Node) *Node {
	return converter.converter(node, parent, false)
}

// convertPattern is upstream's convertPattern.
func (converter *Converter) convertPattern(node *ast.Node, parent *ast.Node) *Node {
	return converter.converter(node, parent, true)
}

// convertChildren is upstream's convertChildren. A nil result stays in the list, as upstream's map
// keeps it, which is how array holes survive.
func (converter *Converter) convertChildren(nodes []*ast.Node, parent *ast.Node) Nodes {
	result := make(Nodes, len(nodes))
	for index, node := range nodes {
		result[index] = converter.converter(node, parent, false)
	}
	return result
}

// converter is upstream's converter.
func (converter *Converter) converter(node *ast.Node, parent *ast.Node, allowPattern bool) *Node {
	if node == nil {
		return nil
	}
	pattern := converter.allowPattern
	converter.allowPattern = allowPattern
	parentNode := parent
	if parentNode == nil {
		parentNode = node.Parent
	}
	converter.checkSyntaxError(node)
	result := converter.convertNode(node, parentNode)
	converter.allowPattern = pattern
	return result
}

// checkSyntaxError is one case of upstream's checkSyntaxError, check-syntax-errors.js, which upstream asks of
// every node before converting it: `++` or `--` on an operand that cannot be assigned. TypeScript's parser
// accepts `1++` and leaves it to the checker, Prettier refuses it, and a formatter that printed it would format
// a file Prettier does not (#0yhe787). Only this case is ported: the others are untested against the fork, and
// each one ported unasked is a file the formatter might newly refuse.
//
// Upstream throws, and the conversion stops at the first node that fails. Recording the first and converting
// on gives the same refusal, since convert returns it before the tree is read.
func (converter *Converter) checkSyntaxError(node *ast.Node) {
	if converter.invalid != nil {
		return
	}
	var operatorKind ast.Kind
	var operand *ast.Node
	switch node.Kind {
	case ast.KindPrefixUnaryExpression:
		operatorKind, operand = node.AsPrefixUnaryExpression().Operator, node.AsPrefixUnaryExpression().Operand
	case ast.KindPostfixUnaryExpression:
		operatorKind, operand = node.AsPostfixUnaryExpression().Operator, node.AsPostfixUnaryExpression().Operand
	default:
		return
	}
	// ESTree uses UpdateExpression for ++/--, and an update's argument must be something assignable.
	if (operatorKind == ast.KindPlusPlusToken || operatorKind == ast.KindMinusMinusToken) &&
		!isValidAssignmentTarget(operand) {
		// Marked as a parse failure, which it is to Prettier, so the format phase skips the file as one that
		// does not parse rather than counting a broken formatter.
		converter.invalid = printing.Syntax(fmt.Errorf(
			"parse error at %d: Invalid left-hand side expression in unary operation",
			converter.getStart(operand),
		))
	}
}

// convertBindingNameWithTypeAnnotation is upstream's convertBindingNameWithTypeAnnotation.
func (converter *Converter) convertBindingNameWithTypeAnnotation(
	name *ast.Node,
	tsType *ast.Node,
	parent *ast.Node,
) *Node {
	id := converter.convertPattern(name, nil)
	if tsType != nil {
		annotation := converter.convertTypeAnnotation(tsType, parent)
		id.Set("typeAnnotation", annotation)
		fixParentLocation(id, annotation.Range)
	}
	return id
}

// convertBodyExpressions is upstream's convertBodyExpressions.
func (converter *Converter) convertBodyExpressions(nodes []*ast.Node, parent *ast.Node) Nodes {
	allowDirectives := canContainDirective(parent)
	result := make(Nodes, 0, len(nodes))
	for _, statement := range nodes {
		child := converter.convertChild(statement, nil)
		if allowDirectives {
			if child != nil && child.Child("expression") != nil && statement.Kind == ast.KindExpressionStatement &&
				statement.Expression().Kind == ast.KindStringLiteral {
				raw := child.Child("expression").String("raw")
				child.Set("directive", String(raw[1:len(raw)-1]))
				result = append(result, child)
				continue
			}
			allowDirectives = false
		}
		if child != nil {
			result = append(result, child)
		}
	}
	return result
}

// convertChainExpression is upstream's convertChainExpression.
func (converter *Converter) convertChainExpression(node *Node, tsNode *ast.Node) *Node {
	var child *Node
	isOptionalChain := false
	switch node.Type() {
	case "MemberExpression":
		child, isOptionalChain = node.Child("object"), node.Bool("optional")
	case "CallExpression":
		child, isOptionalChain = node.Child("callee"), node.Bool("optional")
	default:
		child = node.Child("expression")
	}

	// isChildUnwrappableOptionalChain: (x?.y).z is semantically different, so .z is not optional.
	isChildUnwrappable := child.Is("ChainExpression") && tsNode.Expression().Kind != ast.KindParenthesizedExpression
	if !isChildUnwrappable && !isOptionalChain {
		return node
	}
	if isChildUnwrappable && child.Is("ChainExpression") {
		newChild := child.Child("expression")
		switch node.Type() {
		case "MemberExpression":
			node.Set("object", newChild)
		case "CallExpression":
			node.Set("callee", newChild)
		default:
			node.Set("expression", newChild)
		}
	}
	return converter.createNode(tsNode, "ChainExpression", []Property{{"expression", node}})
}

// convertTypeAnnotation is upstream's convertTypeAnnotation: an intermediary TSTypeAnnotation whose
// range starts at the colon, or at the arrow in function and constructor types.
func (converter *Converter) convertTypeAnnotation(child *ast.Node, parent *ast.Node) *Node {
	offset := 1
	if parent != nil && (parent.Kind == ast.KindFunctionType || parent.Kind == ast.KindConstructorType) {
		offset = 2
	}
	start := child.Pos() - offset
	return New("TSTypeAnnotation", start, child.End(), []Property{
		{"typeAnnotation", converter.convertChild(child, nil)},
	})
}

// convertTypeArguments is upstream's convertTypeArguments.
func (converter *Converter) convertTypeArguments(node *ast.Node) *Node {
	list := node.TypeArgumentList()
	if list == nil {
		return nil
	}
	greaterThan := converter.tokenAt(list.End())
	return converter.createNodeWithRange(
		"TSTypeParameterInstantiation",
		[2]int{list.Pos() - 1, greaterThan.end},
		[]Property{{"params", converter.convertChildren(list.Nodes, nil)}},
	)
}

// convertTypeParameters is upstream's convertTypeParameters.
func (converter *Converter) convertTypeParameters(node *ast.Node) *Node {
	list := node.TypeParameterList()
	if list == nil {
		return nil
	}
	greaterThan := converter.tokenAt(list.End())
	return converter.createNodeWithRange(
		"TSTypeParameterDeclaration",
		[2]int{list.Pos() - 1, greaterThan.end},
		[]Property{{"params", converter.convertChildren(list.Nodes, nil)}},
	)
}

// convertParameters is upstream's convertParameters.
func (converter *Converter) convertParameters(parameters []*ast.Node) Nodes {
	result := make(Nodes, 0, len(parameters))
	for _, parameter := range parameters {
		converted := converter.convertChild(parameter, nil)
		converted.Set("decorators", converter.convertChildren(decorators(parameter), nil))
		result = append(result, converted)
	}
	return result
}

func (converter *Converter) parameterList(node *ast.Node) []*ast.Node {
	list := node.ParameterList()
	if list == nil {
		return nil
	}
	return list.Nodes
}

// returnType is upstream's `node.type && this.convertTypeAnnotation(node.type, node)`, which is
// undefined when there is no type.
func (converter *Converter) returnType(node *ast.Node) *Node {
	if node.Type() == nil {
		return nil
	}
	return converter.convertTypeAnnotation(node.Type(), node)
}

// convertImportAttributes is upstream's convertImportAttributes.
func (converter *Converter) convertImportAttributes(attributes *ast.Node) Nodes {
	if attributes == nil {
		return Nodes{}
	}
	return converter.convertChildren(attributes.AsImportAttributes().Attributes.Nodes, nil)
}

// convertJsxIdentifier is upstream's convertJsxIdentifier.
func (converter *Converter) convertJsxIdentifier(node *ast.Node) *Node {
	return converter.createNode(node, "JSXIdentifier", []Property{{"name", String(converter.getText(node))}})
}

// convertJsxNamespaceOrIdentifier is upstream's convertJsxNamespaceOrIdentifier, for TypeScript 5.1
// and later where JsxNamespacedName is its own node.
func (converter *Converter) convertJsxNamespaceOrIdentifier(node *ast.Node) *Node {
	if node.Kind == ast.KindJsxNamespacedName {
		namespaced := node.AsJsxNamespacedName()
		return converter.createNode(
			node,
			"JSXNamespacedName",
			[]Property{
				{"name", converter.createNode(namespaced.Name(), "JSXIdentifier", []Property{
					{"name", String(namespaced.Name().Text())},
				})},
				{"namespace", converter.createNode(namespaced.Namespace, "JSXIdentifier", []Property{
					{"name", String(namespaced.Namespace.Text())},
				})},
			},
		)
	}
	text := converter.getText(node)
	if colon := strings.Index(text, ":"); colon > 0 {
		nodeRange := converter.getRange(node)
		return converter.createNodeWithRange("JSXNamespacedName", nodeRange, []Property{
			{"name", converter.createNodeWithRange(
				"JSXIdentifier",
				[2]int{nodeRange[0] + colon + 1, nodeRange[1]},
				[]Property{
					{"name", String(text[colon+1:])},
				},
			)},
			{"namespace", converter.createNodeWithRange(
				"JSXIdentifier",
				[2]int{nodeRange[0], nodeRange[0] + colon},
				[]Property{
					{"name", String(text[:colon])},
				},
			)},
		})
	}
	return converter.convertJsxIdentifier(node)
}

// convertJsxTagName is upstream's convertJsxTagName.
func (converter *Converter) convertJsxTagName(node *ast.Node, parent *ast.Node) *Node {
	if node.Kind == ast.KindPropertyAccessExpression {
		access := node.AsPropertyAccessExpression()
		return converter.createNode(node, "JSXMemberExpression", []Property{
			{"object", converter.convertJsxTagName(access.Expression, parent)},
			{"property", converter.convertJsxIdentifier(access.Name())},
		})
	}
	return converter.convertJsxNamespaceOrIdentifier(node)
}

// convertMethodSignature is upstream's convertMethodSignature.
func (converter *Converter) convertMethodSignature(node *ast.Node) *Node {
	kind := "method"
	switch node.Kind {
	case ast.KindGetAccessor:
		kind = "get"
	case ast.KindSetAccessor:
		kind = "set"
	}
	return converter.createNode(node, "TSMethodSignature", []Property{
		{"accessibility", getTsNodeAccessibility(node)},
		{"computed", Boolean(isComputedProperty(node.Name()))},
		{"key", converter.convertChild(node.Name(), nil)},
		{"kind", String(kind)},
		{"optional", Boolean(isOptional(node))},
		{"params", converter.convertParameters(converter.parameterList(node))},
		{"readonly", Boolean(hasModifier(ast.KindReadonlyKeyword, node))},
		{"returnType", converter.returnType(node)},
		{"static", Boolean(hasModifier(ast.KindStaticKeyword, node))},
		{"typeParameters", converter.convertTypeParameters(node)},
	})
}

// fixExports is upstream's fixExports.
func (converter *Converter) fixExports(node *ast.Node, result *Node) *Node {
	isNamespaceNode := node.Kind == ast.KindModuleDeclaration && node.Name().Kind != ast.KindStringLiteral
	var nodeModifiers []*ast.Node
	if isNamespaceNode {
		nodeModifiers = getNamespaceModifiers(node)
	} else {
		nodeModifiers = modifiers(node)
	}
	if len(nodeModifiers) == 0 || nodeModifiers[0].Kind != ast.KindExportKeyword {
		return result
	}

	exportKeyword := nodeModifiers[0]
	var nextModifier *ast.Node
	if len(nodeModifiers) > 1 {
		nextModifier = nodeModifiers[1]
	}
	declarationIsDefault := nextModifier != nil && nextModifier.Kind == ast.KindDefaultKeyword
	var varToken token
	if declarationIsDefault {
		varToken = converter.tokenAt(nextModifier.End())
	} else {
		varToken = converter.tokenAt(exportKeyword.End())
	}
	result.Range[0] = varToken.start

	if declarationIsDefault {
		return converter.createNodeWithRange(
			"ExportDefaultDeclaration",
			[2]int{converter.getStart(exportKeyword), result.Range[1]},
			[]Property{
				{"declaration", result},
				{"exportKind", String("value")},
			},
		)
	}

	isType := result.Type() == "TSInterfaceDeclaration" || result.Type() == "TSTypeAliasDeclaration"
	isDeclare := result.Bool("declare")
	exportKind := "value"
	if isType || isDeclare {
		exportKind = "type"
	}
	return converter.createNodeWithRange(
		"ExportNamedDeclaration",
		[2]int{converter.getStart(exportKeyword), result.Range[1]},
		[]Property{
			{"attributes", noNodes},
			{"declaration", result},
			{"exportKind", String(exportKind)},
			{"source", nil},
			{"specifiers", noNodes},
		},
	)
}

// identifier builds upstream's Identifier object literal.
func (converter *Converter) identifierNode(nodeRange [2]int, name string) *Node {
	return converter.createNodeWithRange("Identifier", nodeRange, []Property{
		{"decorators", noNodes},
		{"name", String(name)},
		{"optional", Boolean(false)},
		{"typeAnnotation", nil},
	})
}

// convertNode is upstream's convertNode.
func (converter *Converter) convertNode(node *ast.Node, parent *ast.Node) *Node {
	switch node.Kind {
	case ast.KindSourceFile:
		// Prettier tries sourceType "module" first for a .ts or .tsx path, and typescript-estree then
		// sets every file's external module indicator, so the Program is always a module here.
		sourceFile := node.AsSourceFile()
		sourceType := "module"
		return converter.createNodeWithRange(
			"Program",
			[2]int{converter.getStart(node), sourceFile.EndOfFileToken.End()},
			[]Property{
				{"body", converter.convertBodyExpressions(sourceFile.Statements.Nodes, node)},
				{"comments", nil},
				{"sourceType", String(sourceType)},
				{"tokens", nil},
			},
		)

	case ast.KindBlock:
		return converter.createNode(node, "BlockStatement", []Property{
			{"body", converter.convertBodyExpressions(node.Statements(), node)},
		})

	case ast.KindIdentifier:
		if isThisInTypeQuery(node) {
			return converter.createNode(node, "ThisExpression", nil)
		}
		return converter.identifierNode(converter.getRange(node), node.Text())

	case ast.KindPrivateIdentifier:
		return converter.createNode(node, "PrivateIdentifier", []Property{{"name", String(node.Text()[1:])}})

	case ast.KindWithStatement:
		withStatement := node.AsWithStatement()
		return converter.createNode(node, "WithStatement", []Property{
			{"body", converter.convertChild(withStatement.Statement, nil)},
			{"object", converter.convertChild(withStatement.Expression, nil)},
		})

	case ast.KindReturnStatement:
		return converter.createNode(node, "ReturnStatement", []Property{
			{"argument", converter.convertChild(node.Expression(), nil)},
		})

	case ast.KindLabeledStatement:
		labeled := node.AsLabeledStatement()
		return converter.createNode(node, "LabeledStatement", []Property{
			{"body", converter.convertChild(labeled.Statement, nil)},
			{"label", converter.convertChild(labeled.Label, nil)},
		})

	case ast.KindContinueStatement:
		return converter.createNode(node, "ContinueStatement", []Property{
			{"label", converter.convertChild(node.AsContinueStatement().Label, nil)},
		})

	case ast.KindBreakStatement:
		return converter.createNode(node, "BreakStatement", []Property{
			{"label", converter.convertChild(node.AsBreakStatement().Label, nil)},
		})

	case ast.KindIfStatement:
		ifStatement := node.AsIfStatement()
		return converter.createNode(node, "IfStatement", []Property{
			{"alternate", converter.convertChild(ifStatement.ElseStatement, nil)},
			{"consequent", converter.convertChild(ifStatement.ThenStatement, nil)},
			{"test", converter.convertChild(ifStatement.Expression, nil)},
		})

	case ast.KindSwitchStatement:
		switchStatement := node.AsSwitchStatement()
		return converter.createNode(node, "SwitchStatement", []Property{
			{"cases", converter.convertChildren(switchStatement.CaseBlock.AsCaseBlock().Clauses.Nodes, nil)},
			{"discriminant", converter.convertChild(switchStatement.Expression, nil)},
		})

	case ast.KindCaseClause, ast.KindDefaultClause:
		clause := node.AsCaseOrDefaultClause()
		var test *Node
		if node.Kind == ast.KindCaseClause {
			test = converter.convertChild(clause.Expression, nil)
		}
		return converter.createNode(node, "SwitchCase", []Property{
			{"consequent", converter.convertChildren(clause.Statements.Nodes, nil)},
			{"test", test},
		})

	case ast.KindThrowStatement:
		return converter.createNode(node, "ThrowStatement", []Property{
			{"argument", converter.convertChild(node.Expression(), nil)},
		})

	case ast.KindTryStatement:
		tryStatement := node.AsTryStatement()
		return converter.createNode(node, "TryStatement", []Property{
			{"block", converter.convertChild(tryStatement.TryBlock, nil)},
			{"finalizer", converter.convertChild(tryStatement.FinallyBlock, nil)},
			{"handler", converter.convertChild(tryStatement.CatchClause, nil)},
		})

	case ast.KindCatchClause:
		catchClause := node.AsCatchClause()
		var parameter *Node
		if catchClause.VariableDeclaration != nil {
			declaration := catchClause.VariableDeclaration
			parameter = converter.convertBindingNameWithTypeAnnotation(declaration.Name(), declaration.Type(), nil)
		}
		return converter.createNode(node, "CatchClause", []Property{
			{"body", converter.convertChild(catchClause.Block, nil)},
			{"param", parameter},
		})

	case ast.KindWhileStatement:
		whileStatement := node.AsWhileStatement()
		return converter.createNode(node, "WhileStatement", []Property{
			{"body", converter.convertChild(whileStatement.Statement, nil)},
			{"test", converter.convertChild(whileStatement.Expression, nil)},
		})

	case ast.KindDoStatement:
		doStatement := node.AsDoStatement()
		return converter.createNode(node, "DoWhileStatement", []Property{
			{"body", converter.convertChild(doStatement.Statement, nil)},
			{"test", converter.convertChild(doStatement.Expression, nil)},
		})

	case ast.KindForStatement:
		forStatement := node.AsForStatement()
		return converter.createNode(node, "ForStatement", []Property{
			{"body", converter.convertChild(forStatement.Statement, nil)},
			{"init", converter.convertChild(forStatement.Initializer, nil)},
			{"test", converter.convertChild(forStatement.Condition, nil)},
			{"update", converter.convertChild(forStatement.Incrementor, nil)},
		})

	case ast.KindForInStatement:
		forIn := node.AsForInOrOfStatement()
		return converter.createNode(node, "ForInStatement", []Property{
			{"body", converter.convertChild(forIn.Statement, nil)},
			{"left", converter.convertPattern(forIn.Initializer, nil)},
			{"right", converter.convertChild(forIn.Expression, nil)},
		})

	case ast.KindForOfStatement:
		forOf := node.AsForInOrOfStatement()
		return converter.createNode(node, "ForOfStatement", []Property{
			{"await", Boolean(forOf.AwaitModifier != nil && forOf.AwaitModifier.Kind == ast.KindAwaitKeyword)},
			{"body", converter.convertChild(forOf.Statement, nil)},
			{"left", converter.convertPattern(forOf.Initializer, nil)},
			{"right", converter.convertChild(forOf.Expression, nil)},
		})

	case ast.KindFunctionDeclaration:
		function := node.AsFunctionDeclaration()
		nodeType := "FunctionDeclaration"
		if function.Body == nil {
			nodeType = "TSDeclareFunction"
		}
		result := converter.createNode(node, nodeType, []Property{
			{"async", Boolean(hasModifier(ast.KindAsyncKeyword, node))},
			{"body", converter.convertChild(function.Body, nil)},
			{"declare", Boolean(hasModifier(ast.KindDeclareKeyword, node))},
			{"expression", Boolean(false)},
			{"generator", Boolean(function.AsteriskToken != nil)},
			{"id", converter.convertChild(node.Name(), nil)},
			{"params", converter.convertParameters(converter.parameterList(node))},
			{"returnType", converter.returnType(node)},
			{"typeParameters", converter.convertTypeParameters(node)},
		})
		return converter.fixExports(node, result)

	case ast.KindVariableDeclaration:
		declaration := node.AsVariableDeclaration()
		init := converter.convertChild(declaration.Initializer, nil)
		id := converter.convertBindingNameWithTypeAnnotation(node.Name(), declaration.Type, node)
		return converter.createNode(node, "VariableDeclarator", []Property{
			{"definite", Boolean(declaration.ExclamationToken != nil)},
			{"id", id},
			{"init", init},
		})

	case ast.KindVariableStatement:
		declarationList := node.AsVariableStatement().DeclarationList
		declarations := declarationList.AsVariableDeclarationList().Declarations.Nodes
		result := converter.createNode(
			node,
			"VariableDeclaration",
			[]Property{
				{"declarations", converter.convertChildren(declarations, nil)},
				{"declare", Boolean(hasModifier(ast.KindDeclareKeyword, node))},
				{"kind", String(getDeclarationKind(declarationList))},
			},
		)
		return converter.fixExports(node, result)

	case ast.KindVariableDeclarationList:
		return converter.createNode(node, "VariableDeclaration", []Property{
			{"declarations", converter.convertChildren(node.AsVariableDeclarationList().Declarations.Nodes, nil)},
			{"declare", Boolean(false)},
			{"kind", String(getDeclarationKind(node))},
		})

	case ast.KindExpressionStatement:
		return converter.createNode(node, "ExpressionStatement", []Property{
			{"directive", nil},
			{"expression", converter.convertChild(node.Expression(), nil)},
		})

	case ast.KindThisKeyword:
		return converter.createNode(node, "ThisExpression", nil)

	case ast.KindArrayLiteralExpression:
		elements := node.AsArrayLiteralExpression().Elements.Nodes
		if converter.allowPattern {
			patterns := make(Nodes, len(elements))
			for index, element := range elements {
				patterns[index] = converter.convertPattern(element, nil)
			}
			return converter.createNode(node, "ArrayPattern", []Property{
				{"decorators", noNodes},
				{"elements", patterns},
				{"optional", Boolean(false)},
				{"typeAnnotation", nil},
			})
		}
		return converter.createNode(node, "ArrayExpression", []Property{
			{"elements", converter.convertChildren(elements, nil)},
		})

	case ast.KindObjectLiteralExpression:
		properties := node.AsObjectLiteralExpression().Properties.Nodes
		if converter.allowPattern {
			patterns := make(Nodes, len(properties))
			for index, property := range properties {
				patterns[index] = converter.convertPattern(property, nil)
			}
			return converter.createNode(node, "ObjectPattern", []Property{
				{"decorators", noNodes},
				{"optional", Boolean(false)},
				{"properties", patterns},
				{"typeAnnotation", nil},
			})
		}
		return converter.createNode(node, "ObjectExpression", []Property{
			{"properties", converter.convertChildren(properties, nil)},
		})

	case ast.KindPropertyAssignment:
		assignment := node.AsPropertyAssignment()
		return converter.createNode(node, "Property", []Property{
			{"computed", Boolean(isComputedProperty(node.Name()))},
			{"key", converter.convertChild(node.Name(), nil)},
			{"kind", String("init")},
			{"method", Boolean(false)},
			{"optional", Boolean(false)},
			{"shorthand", Boolean(false)},
			{"value", converter.converter(assignment.Initializer, node, converter.allowPattern)},
		})

	case ast.KindShorthandPropertyAssignment:
		shorthand := node.AsShorthandPropertyAssignment()
		if shorthand.ObjectAssignmentInitializer != nil {
			return converter.createNode(node, "Property", []Property{
				{"computed", Boolean(false)},
				{"key", converter.convertChild(node.Name(), nil)},
				{"kind", String("init")},
				{"method", Boolean(false)},
				{"optional", Boolean(false)},
				{"shorthand", Boolean(true)},
				{"value", converter.createNode(node, "AssignmentPattern", []Property{
					{"decorators", noNodes},
					{"left", converter.convertPattern(node.Name(), nil)},
					{"optional", Boolean(false)},
					{"right", converter.convertChild(shorthand.ObjectAssignmentInitializer, nil)},
					{"typeAnnotation", nil},
				})},
			})
		}
		return converter.createNode(node, "Property", []Property{
			{"computed", Boolean(false)},
			{"key", converter.convertChild(node.Name(), nil)},
			{"kind", String("init")},
			{"method", Boolean(false)},
			{"optional", Boolean(false)},
			{"shorthand", Boolean(true)},
			{"value", converter.convertChild(node.Name(), nil)},
		})

	case ast.KindComputedPropertyName:
		return converter.convertChild(node.Expression(), nil)

	case ast.KindPropertyDeclaration:
		property := node.AsPropertyDeclaration()
		isAbstract := hasModifier(ast.KindAbstractKeyword, node)
		isAccessor := hasModifier(ast.KindAccessorKeyword, node)
		nodeType := "PropertyDefinition"
		switch {
		case isAccessor && isAbstract:
			nodeType = "TSAbstractAccessorProperty"
		case isAccessor:
			nodeType = "AccessorProperty"
		case isAbstract:
			nodeType = "TSAbstractPropertyDefinition"
		}
		key := converter.convertChild(node.Name(), nil)
		nameKind := node.Name().Kind
		optional := (key.Is("Literal") || nameKind == ast.KindIdentifier || nameKind == ast.KindComputedPropertyName ||
			nameKind == ast.KindPrivateIdentifier) && isOptional(node)
		var value *Node
		if !isAbstract {
			value = converter.convertChild(property.Initializer, nil)
		}
		var typeAnnotation *Node
		if property.Type != nil {
			typeAnnotation = converter.convertTypeAnnotation(property.Type, node)
		}
		return converter.createNode(node, nodeType, []Property{
			{"accessibility", getTsNodeAccessibility(node)},
			{"computed", Boolean(isComputedProperty(node.Name()))},
			{"declare", Boolean(hasModifier(ast.KindDeclareKeyword, node))},
			{"decorators", converter.convertChildren(decorators(node), nil)},
			{"definite", Boolean(
				property.PostfixToken != nil && property.PostfixToken.Kind == ast.KindExclamationToken,
			)},
			{"key", key},
			{"optional", Boolean(optional)},
			{"override", Boolean(hasModifier(ast.KindOverrideKeyword, node))},
			{"readonly", Boolean(hasModifier(ast.KindReadonlyKeyword, node))},
			{"static", Boolean(hasModifier(ast.KindStaticKeyword, node))},
			{"typeAnnotation", typeAnnotation},
			{"value", value},
		})

	case ast.KindGetAccessor, ast.KindSetAccessor, ast.KindMethodDeclaration:
		if (node.Kind == ast.KindGetAccessor || node.Kind == ast.KindSetAccessor) &&
			(node.Parent.Kind == ast.KindInterfaceDeclaration || node.Parent.Kind == ast.KindTypeLiteral) {
			return converter.convertMethodSignature(node)
		}
		return converter.convertMethod(node, parent)

	case ast.KindConstructor:
		return converter.convertConstructor(node)

	case ast.KindFunctionExpression:
		function := node.AsFunctionExpression()
		return converter.createNode(node, "FunctionExpression", []Property{
			{"async", Boolean(hasModifier(ast.KindAsyncKeyword, node))},
			{"body", converter.convertChild(function.Body, nil)},
			{"declare", Boolean(false)},
			{"expression", Boolean(false)},
			{"generator", Boolean(function.AsteriskToken != nil)},
			{"id", converter.convertChild(node.Name(), nil)},
			{"params", converter.convertParameters(converter.parameterList(node))},
			{"returnType", converter.returnType(node)},
			{"typeParameters", converter.convertTypeParameters(node)},
		})

	case ast.KindSuperKeyword:
		return converter.createNode(node, "Super", nil)

	case ast.KindArrayBindingPattern:
		elements := node.AsBindingPattern().Elements.Nodes
		patterns := make(Nodes, len(elements))
		for index, element := range elements {
			patterns[index] = converter.convertPattern(element, nil)
		}
		return converter.createNode(node, "ArrayPattern", []Property{
			{"decorators", noNodes},
			{"elements", patterns},
			{"optional", Boolean(false)},
			{"typeAnnotation", nil},
		})

	case ast.KindOmittedExpression:
		return nil

	case ast.KindObjectBindingPattern:
		elements := node.AsBindingPattern().Elements.Nodes
		patterns := make(Nodes, len(elements))
		for index, element := range elements {
			patterns[index] = converter.convertPattern(element, nil)
		}
		return converter.createNode(node, "ObjectPattern", []Property{
			{"decorators", noNodes},
			{"optional", Boolean(false)},
			{"properties", patterns},
			{"typeAnnotation", nil},
		})

	case ast.KindBindingElement:
		return converter.convertBindingElement(node, parent)

	case ast.KindArrowFunction:
		arrow := node.AsArrowFunction()
		return converter.createNode(node, "ArrowFunctionExpression", []Property{
			{"async", Boolean(hasModifier(ast.KindAsyncKeyword, node))},
			{"body", converter.convertChild(arrow.Body, nil)},
			{"expression", Boolean(arrow.Body.Kind != ast.KindBlock)},
			{"generator", Boolean(false)},
			{"id", nil},
			{"params", converter.convertParameters(converter.parameterList(node))},
			{"returnType", converter.returnType(node)},
			{"typeParameters", converter.convertTypeParameters(node)},
		})

	case ast.KindYieldExpression:
		yield := node.AsYieldExpression()
		return converter.createNode(node, "YieldExpression", []Property{
			{"argument", converter.convertChild(yield.Expression, nil)},
			{"delegate", Boolean(yield.AsteriskToken != nil)},
		})

	case ast.KindAwaitExpression:
		return converter.createNode(node, "AwaitExpression", []Property{
			{"argument", converter.convertChild(node.Expression(), nil)},
		})

	case ast.KindNoSubstitutionTemplateLiteral:
		rawText := converter.text[converter.getStart(node)+1 : node.End()-1]
		cooked := node.Text()
		cookedValue := &cooked
		if node.Parent.Kind == ast.KindTaggedTemplateExpression && !isValidEscape(rawText) {
			cookedValue = nil
		}
		return converter.createNode(node, "TemplateLiteral", []Property{
			{"expressions", noNodes},
			{"quasis", Nodes{converter.createNode(node, "TemplateElement", []Property{
				{"tail", Boolean(true)},
				{"value", &TemplateValue{Cooked: cookedValue, Raw: rawText}},
			})}},
		})

	case ast.KindTemplateExpression:
		template := node.AsTemplateExpression()
		expressions := Nodes{}
		quasis := Nodes{converter.convertChild(template.Head, nil)}
		for _, span := range template.TemplateSpans.Nodes {
			templateSpan := span.AsTemplateSpan()
			expressions = append(expressions, converter.convertChild(templateSpan.Expression, nil))
			quasis = append(quasis, converter.convertChild(templateSpan.Literal, nil))
		}
		return converter.createNode(node, "TemplateLiteral", []Property{
			{"expressions", expressions},
			{"quasis", quasis},
		})

	case ast.KindTaggedTemplateExpression:
		tagged := node.AsTaggedTemplateExpression()
		return converter.createNode(node, "TaggedTemplateExpression", []Property{
			{"quasi", converter.convertChild(tagged.Template, nil)},
			{"tag", converter.convertChild(tagged.Tag, nil)},
			{"typeArguments", converter.convertTypeArguments(node)},
		})

	case ast.KindTemplateHead, ast.KindTemplateMiddle, ast.KindTemplateTail:
		tail := node.Kind == ast.KindTemplateTail
		endOffset := 2
		if tail {
			endOffset = 1
		}
		rawText := converter.text[converter.getStart(node)+1 : node.End()-endOffset]
		var isTagged bool
		if node.Kind == ast.KindTemplateHead {
			isTagged = node.Parent.Parent.Kind == ast.KindTaggedTemplateExpression
		} else {
			isTagged = node.Parent.Parent.Parent.Kind == ast.KindTaggedTemplateExpression
		}
		cooked := node.Text()
		cookedValue := &cooked
		if isTagged && !isValidEscape(rawText) {
			cookedValue = nil
		}
		return converter.createNode(node, "TemplateElement", []Property{
			{"tail", Boolean(tail)},
			{"value", &TemplateValue{Cooked: cookedValue, Raw: rawText}},
		})

	case ast.KindSpreadAssignment, ast.KindSpreadElement:
		if converter.allowPattern {
			return converter.createNode(node, "RestElement", []Property{
				{"argument", converter.convertPattern(node.Expression(), nil)},
				{"decorators", noNodes},
				{"optional", Boolean(false)},
				{"typeAnnotation", nil},
				{"value", nil},
			})
		}
		return converter.createNode(node, "SpreadElement", []Property{
			{"argument", converter.convertChild(node.Expression(), nil)},
		})

	case ast.KindParameter:
		return converter.convertParameter(node, parent)

	case ast.KindClassDeclaration, ast.KindClassExpression:
		return converter.convertClass(node)

	case ast.KindModuleBlock:
		return converter.createNode(node, "TSModuleBlock", []Property{
			{"body", converter.convertBodyExpressions(node.Statements(), node)},
		})

	case ast.KindImportDeclaration:
		return converter.convertImportDeclaration(node)

	case ast.KindNamespaceImport:
		return converter.createNode(node, "ImportNamespaceSpecifier", []Property{
			{"local", converter.convertChild(node.Name(), nil)},
		})

	case ast.KindImportSpecifier:
		specifier := node.AsImportSpecifier()
		imported := specifier.PropertyName
		if imported == nil {
			imported = node.Name()
		}
		importKind := "value"
		if specifier.IsTypeOnly {
			importKind = "type"
		}
		return converter.createNode(node, "ImportSpecifier", []Property{
			{"imported", converter.convertChild(imported, nil)},
			{"importKind", String(importKind)},
			{"local", converter.convertChild(node.Name(), nil)},
		})

	case ast.KindImportClause:
		local := converter.convertChild(node.Name(), nil)
		return converter.createNodeWithRange("ImportDefaultSpecifier", local.Range, []Property{{"local", local}})

	case ast.KindExportDeclaration:
		export := node.AsExportDeclaration()
		exportKind := "value"
		if export.IsTypeOnly {
			exportKind = "type"
		}
		if export.ExportClause != nil && export.ExportClause.Kind == ast.KindNamedExports {
			return converter.createNode(node, "ExportNamedDeclaration", []Property{
				{"attributes", converter.convertImportAttributes(export.Attributes)},
				{"declaration", nil},
				{"exportKind", String(exportKind)},
				{"source", converter.convertChild(export.ModuleSpecifier, nil)},
				{"specifiers", converter.convertChildren(export.ExportClause.AsNamedExports().Elements.Nodes, node)},
			})
		}
		var exported *Node
		if export.ExportClause != nil && export.ExportClause.Kind == ast.KindNamespaceExport {
			exported = converter.convertChild(export.ExportClause.Name(), nil)
		}
		return converter.createNode(node, "ExportAllDeclaration", []Property{
			{"attributes", converter.convertImportAttributes(export.Attributes)},
			{"exported", exported},
			{"exportKind", String(exportKind)},
			{"source", converter.convertChild(export.ModuleSpecifier, nil)},
		})

	case ast.KindExportSpecifier:
		specifier := node.AsExportSpecifier()
		local := specifier.PropertyName
		if local == nil {
			local = node.Name()
		}
		exportKind := "value"
		if specifier.IsTypeOnly {
			exportKind = "type"
		}
		return converter.createNode(node, "ExportSpecifier", []Property{
			{"exported", converter.convertChild(node.Name(), nil)},
			{"exportKind", String(exportKind)},
			{"local", converter.convertChild(local, nil)},
		})

	case ast.KindExportAssignment:
		assignment := node.AsExportAssignment()
		if assignment.IsExportEquals {
			return converter.createNode(node, "TSExportAssignment", []Property{
				{"expression", converter.convertChild(assignment.Expression, nil)},
			})
		}
		return converter.createNode(node, "ExportDefaultDeclaration", []Property{
			{"declaration", converter.convertChild(assignment.Expression, nil)},
			{"exportKind", String("value")},
		})

	case ast.KindPrefixUnaryExpression, ast.KindPostfixUnaryExpression:
		var operatorKind ast.Kind
		var operand *ast.Node
		if node.Kind == ast.KindPrefixUnaryExpression {
			operatorKind, operand = node.AsPrefixUnaryExpression().Operator, node.AsPrefixUnaryExpression().Operand
		} else {
			operatorKind, operand = node.AsPostfixUnaryExpression().Operator, node.AsPostfixUnaryExpression().Operand
		}
		operator := scanner.TokenToString(operatorKind)
		if operator == "++" || operator == "--" {
			return converter.createNode(node, "UpdateExpression", []Property{
				{"argument", converter.convertChild(operand, nil)},
				{"operator", String(operator)},
				{"prefix", Boolean(node.Kind == ast.KindPrefixUnaryExpression)},
			})
		}
		return converter.createNode(node, "UnaryExpression", []Property{
			{"argument", converter.convertChild(operand, nil)},
			{"operator", String(operator)},
			{"prefix", Boolean(node.Kind == ast.KindPrefixUnaryExpression)},
		})

	case ast.KindDeleteExpression:
		return converter.unary(node, "delete")
	case ast.KindVoidExpression:
		return converter.unary(node, "void")
	case ast.KindTypeOfExpression:
		return converter.unary(node, "typeof")

	case ast.KindTypeOperator:
		operator := node.AsTypeOperatorNode()
		return converter.createNode(node, "TSTypeOperator", []Property{
			{"operator", String(scanner.TokenToString(operator.Operator))},
			{"typeAnnotation", converter.convertChild(operator.Type, nil)},
		})

	case ast.KindBinaryExpression:
		return converter.convertBinary(node)

	case ast.KindPropertyAccessExpression:
		access := node.AsPropertyAccessExpression()
		object := converter.convertChild(access.Expression, nil)
		property := converter.convertChild(access.Name(), nil)
		result := converter.createNode(node, "MemberExpression", []Property{
			{"computed", Boolean(false)},
			{"object", object},
			{"optional", Boolean(access.QuestionDotToken != nil)},
			{"property", property},
		})
		return converter.convertChainExpression(result, node)

	case ast.KindElementAccessExpression:
		access := node.AsElementAccessExpression()
		object := converter.convertChild(access.Expression, nil)
		property := converter.convertChild(access.ArgumentExpression, nil)
		result := converter.createNode(node, "MemberExpression", []Property{
			{"computed", Boolean(true)},
			{"object", object},
			{"optional", Boolean(access.QuestionDotToken != nil)},
			{"property", property},
		})
		return converter.convertChainExpression(result, node)

	case ast.KindCallExpression:
		call := node.AsCallExpression()
		arguments := call.Arguments.Nodes
		if call.Expression.Kind == ast.KindImportKeyword {
			var options *Node
			if len(arguments) > 1 {
				options = converter.convertChild(arguments[1], nil)
			}
			var source *Node
			if len(arguments) > 0 {
				source = converter.convertChild(arguments[0], nil)
			}
			return converter.createNode(node, "ImportExpression", []Property{
				{"options", options},
				{"source", source},
			})
		}
		callee := converter.convertChild(call.Expression, nil)
		convertedArguments := converter.convertChildren(arguments, nil)
		typeArguments := converter.convertTypeArguments(node)
		result := converter.createNode(node, "CallExpression", []Property{
			{"arguments", convertedArguments},
			{"callee", callee},
			{"optional", Boolean(call.QuestionDotToken != nil)},
			{"typeArguments", typeArguments},
		})
		return converter.convertChainExpression(result, node)

	case ast.KindNewExpression:
		newExpression := node.AsNewExpression()
		typeArguments := converter.convertTypeArguments(node)
		var arguments []*ast.Node
		if newExpression.Arguments != nil {
			arguments = newExpression.Arguments.Nodes
		}
		return converter.createNode(node, "NewExpression", []Property{
			{"arguments", converter.convertChildren(arguments, nil)},
			{"callee", converter.convertChild(newExpression.Expression, nil)},
			{"typeArguments", typeArguments},
		})

	case ast.KindConditionalExpression:
		conditional := node.AsConditionalExpression()
		return converter.createNode(node, "ConditionalExpression", []Property{
			{"alternate", converter.convertChild(conditional.WhenFalse, nil)},
			{"consequent", converter.convertChild(conditional.WhenTrue, nil)},
			{"test", converter.convertChild(conditional.Condition, nil)},
		})

	case ast.KindMetaProperty:
		meta := node.AsMetaProperty()
		first := converter.firstToken(node)
		return converter.createNode(node, "MetaProperty", []Property{
			{"meta", converter.identifierNode(
				[2]int{first.start, first.end},
				scanner.TokenToString(meta.KeywordToken),
			)},
			{"property", converter.convertChild(meta.Name(), nil)},
		})

	case ast.KindDecorator:
		return converter.createNode(node, "Decorator", []Property{
			{"expression", converter.convertChild(node.Expression(), nil)},
		})

	case ast.KindStringLiteral:
		value := node.Text()
		if parent.Kind == ast.KindJsxAttribute {
			value = ecmascripttext.UnescapeStringLiteralText(value)
		}
		return converter.createNode(node, "Literal", []Property{
			{"raw", String(converter.getText(node))},
			{"value", String(value)},
		})

	case ast.KindNumericLiteral:
		raw := converter.getText(node)
		return converter.createNode(node, "Literal", []Property{
			{"raw", String(raw)},
			{"value", Number(numberValue(raw))},
		})

	case ast.KindBigIntLiteral:
		nodeRange := converter.getRange(node)
		rawValue := converter.text[nodeRange[0]:nodeRange[1]]
		bigint := strings.ReplaceAll(rawValue[:len(rawValue)-1], "_", "")
		return converter.createNodeWithRange("Literal", nodeRange, []Property{
			{"bigint", String(bigintString(bigint))},
			{"raw", String(rawValue)},
			{"value", nil},
		})

	case ast.KindRegularExpressionLiteral:
		text := node.Text()
		slash := strings.LastIndex(text, "/")
		return converter.createNode(node, "Literal", []Property{
			{"raw", String(text)},
			{"regex", &Regex{Pattern: text[1:slash], Flags: text[slash+1:]}},
			{"value", nil},
		})

	case ast.KindTrueKeyword:
		return converter.createNode(node, "Literal", []Property{{"raw", String("true")}, {"value", Boolean(true)}})
	case ast.KindFalseKeyword:
		return converter.createNode(node, "Literal", []Property{{"raw", String("false")}, {"value", Boolean(false)}})
	case ast.KindNullKeyword:
		return converter.createNode(node, "Literal", []Property{{"raw", String("null")}, {"value", nil}})

	case ast.KindEmptyStatement:
		return converter.createNode(node, "EmptyStatement", nil)
	case ast.KindDebuggerStatement:
		return converter.createNode(node, "DebuggerStatement", nil)

	case ast.KindJsxElement:
		element := node.AsJsxElement()
		return converter.createNode(node, "JSXElement", []Property{
			{"children", converter.convertChildren(element.Children.Nodes, nil)},
			{"closingElement", converter.convertChild(element.ClosingElement, nil)},
			{"openingElement", converter.convertChild(element.OpeningElement, nil)},
		})

	case ast.KindJsxFragment:
		fragment := node.AsJsxFragment()
		return converter.createNode(node, "JSXFragment", []Property{
			{"children", converter.convertChildren(fragment.Children.Nodes, nil)},
			{"closingFragment", converter.convertChild(fragment.ClosingFragment, nil)},
			{"openingFragment", converter.convertChild(fragment.OpeningFragment, nil)},
		})

	case ast.KindJsxSelfClosingElement:
		element := node.AsJsxSelfClosingElement()
		return converter.createNode(node, "JSXElement", []Property{
			{"children", noNodes},
			{"closingElement", nil},
			{"openingElement", converter.createNodeWithRange("JSXOpeningElement", converter.getRange(node), []Property{
				{"attributes", converter.convertChildren(element.Attributes.AsJsxAttributes().Properties.Nodes, nil)},
				{"name", converter.convertJsxTagName(element.TagName, node)},
				{"selfClosing", Boolean(true)},
				{"typeArguments", converter.convertTypeArguments(node)},
			})},
		})

	case ast.KindJsxOpeningElement:
		element := node.AsJsxOpeningElement()
		return converter.createNode(node, "JSXOpeningElement", []Property{
			{"attributes", converter.convertChildren(element.Attributes.AsJsxAttributes().Properties.Nodes, nil)},
			{"name", converter.convertJsxTagName(element.TagName, node)},
			{"selfClosing", Boolean(false)},
			{"typeArguments", converter.convertTypeArguments(node)},
		})

	case ast.KindJsxClosingElement:
		return converter.createNode(node, "JSXClosingElement", []Property{
			{"name", converter.convertJsxTagName(node.AsJsxClosingElement().TagName, node)},
		})

	case ast.KindJsxOpeningFragment:
		return converter.createNode(node, "JSXOpeningFragment", nil)
	case ast.KindJsxClosingFragment:
		return converter.createNode(node, "JSXClosingFragment", nil)

	case ast.KindJsxExpression:
		jsxExpression := node.AsJsxExpression()
		var expression *Node
		if jsxExpression.Expression != nil {
			expression = converter.convertChild(jsxExpression.Expression, nil)
		} else {
			expression = converter.createNodeWithRange(
				"JSXEmptyExpression",
				[2]int{converter.getStart(node) + 1, node.End() - 1}, nil,
			)
		}
		if jsxExpression.DotDotDotToken != nil {
			return converter.createNode(node, "JSXSpreadChild", []Property{{"expression", expression}})
		}
		return converter.createNode(node, "JSXExpressionContainer", []Property{{"expression", expression}})

	case ast.KindJsxAttribute:
		attribute := node.AsJsxAttribute()
		return converter.createNode(node, "JSXAttribute", []Property{
			{"name", converter.convertJsxNamespaceOrIdentifier(attribute.Name())},
			{"value", converter.convertChild(attribute.Initializer, nil)},
		})

	case ast.KindJsxText:
		start, end := node.Pos(), node.End()
		text := converter.text[start:end]
		return converter.createNodeWithRange("JSXText", [2]int{start, end}, []Property{
			{"raw", String(text)},
			{"value", String(ecmascripttext.UnescapeStringLiteralText(text))},
		})

	case ast.KindJsxSpreadAttribute:
		return converter.createNode(node, "JSXSpreadAttribute", []Property{
			{"argument", converter.convertChild(node.Expression(), nil)},
		})

	case ast.KindQualifiedName:
		qualified := node.AsQualifiedName()
		return converter.createNode(node, "TSQualifiedName", []Property{
			{"left", converter.convertChild(qualified.Left, nil)},
			{"right", converter.convertChild(qualified.Right, nil)},
		})

	case ast.KindTypeReference:
		// typescript-go parses an interface's extends and a class's implements as type references,
		// converting `A.B` to a qualified name, where TypeScript keeps ExpressionWithTypeArguments.
		// Upstream's ExpressionWithTypeArguments case is what these must become.
		if parent.Kind == ast.KindInterfaceDeclaration || parent.Kind == ast.KindHeritageClause {
			nodeType := "TSClassImplements"
			if parent.Kind == ast.KindInterfaceDeclaration {
				nodeType = "TSInterfaceHeritage"
			}
			return converter.createNode(node, nodeType, []Property{
				{"expression", converter.convertEntityNameAsExpression(node.AsTypeReferenceNode().TypeName)},
				{"typeArguments", converter.convertTypeArguments(node)},
			})
		}
		return converter.createNode(node, "TSTypeReference", []Property{
			{"typeArguments", converter.convertTypeArguments(node)},
			{"typeName", converter.convertChild(node.AsTypeReferenceNode().TypeName, nil)},
		})

	case ast.KindTypeParameter:
		parameter := node.AsTypeParameterDeclaration()
		var defaultType Value
		if parameter.DefaultType != nil {
			defaultType = converter.convertChild(parameter.DefaultType, nil)
		}
		var constraint Value
		if parameter.Constraint != nil {
			constraint = converter.convertChild(parameter.Constraint, nil)
		}
		return converter.createNode(node, "TSTypeParameter", []Property{
			{"const", Boolean(hasModifier(ast.KindConstKeyword, node))},
			{"constraint", constraint},
			{"default", defaultType},
			{"in", Boolean(hasModifier(ast.KindInKeyword, node))},
			{"name", converter.convertChild(node.Name(), nil)},
			{"out", Boolean(hasModifier(ast.KindOutKeyword, node))},
		})

	case ast.KindThisType:
		return converter.createNode(node, "TSThisType", nil)

	case ast.KindAnyKeyword, ast.KindBigIntKeyword, ast.KindBooleanKeyword, ast.KindNeverKeyword,
		ast.KindNumberKeyword, ast.KindObjectKeyword, ast.KindStringKeyword, ast.KindSymbolKeyword,
		ast.KindUnknownKeyword, ast.KindVoidKeyword, ast.KindUndefinedKeyword, ast.KindIntrinsicKeyword:
		return converter.createNode(node, keywordTypeNames[node.Kind], nil)

	case ast.KindNonNullExpression:
		nonNull := converter.createNode(node, "TSNonNullExpression", []Property{
			{"expression", converter.convertChild(node.Expression(), nil)},
		})
		return converter.convertChainExpression(nonNull, node)

	case ast.KindTypeLiteral:
		return converter.createNode(node, "TSTypeLiteral", []Property{
			{"members", converter.convertChildren(node.Members(), nil)},
		})

	case ast.KindArrayType:
		return converter.createNode(node, "TSArrayType", []Property{
			{"elementType", converter.convertChild(node.AsArrayTypeNode().ElementType, nil)},
		})

	case ast.KindIndexedAccessType:
		indexed := node.AsIndexedAccessTypeNode()
		return converter.createNode(node, "TSIndexedAccessType", []Property{
			{"indexType", converter.convertChild(indexed.IndexType, nil)},
			{"objectType", converter.convertChild(indexed.ObjectType, nil)},
		})

	case ast.KindConditionalType:
		conditional := node.AsConditionalTypeNode()
		return converter.createNode(node, "TSConditionalType", []Property{
			{"checkType", converter.convertChild(conditional.CheckType, nil)},
			{"extendsType", converter.convertChild(conditional.ExtendsType, nil)},
			{"falseType", converter.convertChild(conditional.FalseType, nil)},
			{"trueType", converter.convertChild(conditional.TrueType, nil)},
		})

	case ast.KindTypeQuery:
		return converter.createNode(node, "TSTypeQuery", []Property{
			{"exprName", converter.convertChild(node.AsTypeQueryNode().ExprName, nil)},
			{"typeArguments", converter.convertTypeArguments(node)},
		})

	case ast.KindMappedType:
		mapped := node.AsMappedTypeNode()
		typeParameter := mapped.TypeParameter.AsTypeParameterDeclaration()
		var optional Value = Boolean(false)
		if mapped.QuestionToken != nil {
			if mapped.QuestionToken.Kind == ast.KindQuestionToken {
				optional = Boolean(true)
			} else {
				optional = String(scanner.TokenToString(mapped.QuestionToken.Kind))
			}
		}
		var readonly Value
		if mapped.ReadonlyToken != nil {
			if mapped.ReadonlyToken.Kind == ast.KindReadonlyKeyword {
				readonly = Boolean(true)
			} else {
				readonly = String(scanner.TokenToString(mapped.ReadonlyToken.Kind))
			}
		}
		var typeAnnotation *Node
		if mapped.Type != nil {
			typeAnnotation = converter.convertChild(mapped.Type, nil)
		}
		return converter.createNode(node, "TSMappedType", []Property{
			{"constraint", converter.convertChild(typeParameter.Constraint, nil)},
			{"key", converter.convertChild(mapped.TypeParameter.Name(), nil)},
			{"nameType", converter.convertChild(mapped.NameType, nil)},
			{"optional", optional},
			{"readonly", readonly},
			{"typeAnnotation", typeAnnotation},
		})

	case ast.KindParenthesizedExpression:
		// Under babel, Prettier keeps Babel's ParenthesizedExpression after a Closure-style type cast
		// comment (parse/postprocess/index.js); typescript-estree never makes one.
		if start := converter.getStart(node); converter.followsTypeCastComment(start) {
			return converter.createNodeWithRange("ParenthesizedExpression", converter.getRange(node), []Property{
				{"expression", converter.convertChild(node.Expression(), parent)},
			})
		}
		return converter.convertChild(node.Expression(), parent)

	case ast.KindTypeAliasDeclaration:
		alias := node.AsTypeAliasDeclaration()
		result := converter.createNode(node, "TSTypeAliasDeclaration", []Property{
			{"declare", Boolean(hasModifier(ast.KindDeclareKeyword, node))},
			{"id", converter.convertChild(node.Name(), nil)},
			{"typeAnnotation", converter.convertChild(alias.Type, nil)},
			{"typeParameters", converter.convertTypeParameters(node)},
		})
		return converter.fixExports(node, result)

	case ast.KindMethodSignature:
		return converter.convertMethodSignature(node)

	case ast.KindPropertySignature:
		signature := node.AsPropertySignatureDeclaration()
		var typeAnnotation *Node
		if signature.Type != nil {
			typeAnnotation = converter.convertTypeAnnotation(signature.Type, node)
		}
		return converter.createNode(node, "TSPropertySignature", []Property{
			{"accessibility", getTsNodeAccessibility(node)},
			{"computed", Boolean(isComputedProperty(node.Name()))},
			{"key", converter.convertChild(node.Name(), nil)},
			{"optional", Boolean(isOptional(node))},
			{"readonly", Boolean(hasModifier(ast.KindReadonlyKeyword, node))},
			{"static", Boolean(hasModifier(ast.KindStaticKeyword, node))},
			{"typeAnnotation", typeAnnotation},
		})

	case ast.KindIndexSignature:
		return converter.createNode(node, "TSIndexSignature", []Property{
			{"accessibility", getTsNodeAccessibility(node)},
			{"parameters", converter.convertChildren(converter.parameterList(node), nil)},
			{"readonly", Boolean(hasModifier(ast.KindReadonlyKeyword, node))},
			{"static", Boolean(hasModifier(ast.KindStaticKeyword, node))},
			{"typeAnnotation", converter.returnType(node)},
		})

	case ast.KindConstructorType:
		return converter.createNode(node, "TSConstructorType", []Property{
			{"abstract", Boolean(hasModifier(ast.KindAbstractKeyword, node))},
			{"params", converter.convertParameters(converter.parameterList(node))},
			{"returnType", converter.returnType(node)},
			{"typeParameters", converter.convertTypeParameters(node)},
		})

	case ast.KindFunctionType, ast.KindConstructSignature, ast.KindCallSignature:
		nodeType := "TSFunctionType"
		switch node.Kind {
		case ast.KindConstructSignature:
			nodeType = "TSConstructSignatureDeclaration"
		case ast.KindCallSignature:
			nodeType = "TSCallSignatureDeclaration"
		}
		return converter.createNode(node, nodeType, []Property{
			{"params", converter.convertParameters(converter.parameterList(node))},
			{"returnType", converter.returnType(node)},
			{"typeParameters", converter.convertTypeParameters(node)},
		})

	case ast.KindExpressionWithTypeArguments:
		nodeType := "TSInstantiationExpression"
		switch parent.Kind {
		case ast.KindInterfaceDeclaration:
			nodeType = "TSInterfaceHeritage"
		case ast.KindHeritageClause:
			nodeType = "TSClassImplements"
		}
		return converter.createNode(node, nodeType, []Property{
			{"expression", converter.convertChild(node.Expression(), nil)},
			{"typeArguments", converter.convertTypeArguments(node)},
		})

	case ast.KindInterfaceDeclaration:
		interfaceDeclaration := node.AsInterfaceDeclaration()
		interfaceExtends := Nodes{}
		if interfaceDeclaration.HeritageClauses != nil {
			for _, clause := range interfaceDeclaration.HeritageClauses.Nodes {
				heritage := clause.AsHeritageClause()
				if heritage.Token != ast.KindExtendsKeyword {
					continue
				}
				for _, heritageType := range heritage.Types.Nodes {
					interfaceExtends = append(interfaceExtends, converter.convertChild(heritageType, node))
				}
			}
		}
		members := interfaceDeclaration.Members
		result := converter.createNode(node, "TSInterfaceDeclaration", []Property{
			{"body", converter.createNodeWithRange("TSInterfaceBody", [2]int{members.Pos() - 1, node.End()}, []Property{
				{"body", converter.convertChildren(members.Nodes, nil)},
			})},
			{"declare", Boolean(hasModifier(ast.KindDeclareKeyword, node))},
			{"extends", interfaceExtends},
			{"id", converter.convertChild(node.Name(), nil)},
			{"typeParameters", converter.convertTypeParameters(node)},
		})
		return converter.fixExports(node, result)

	case ast.KindTypePredicate:
		predicate := node.AsTypePredicateNode()
		result := converter.createNode(node, "TSTypePredicate", []Property{
			{"asserts", Boolean(predicate.AssertsModifier != nil)},
			{"parameterName", converter.convertChild(predicate.ParameterName, nil)},
			{"typeAnnotation", nil},
		})
		if predicate.Type != nil {
			annotation := converter.convertTypeAnnotation(predicate.Type, node)
			annotation.Range = annotation.Child("typeAnnotation").Range
			result.Set("typeAnnotation", annotation)
		}
		return result

	case ast.KindImportType:
		return converter.convertImportType(node)

	case ast.KindEnumDeclaration:
		enum := node.AsEnumDeclaration()
		members := converter.convertChildren(enum.Members.Nodes, nil)
		result := converter.createNode(
			node,
			"TSEnumDeclaration",
			[]Property{
				{"body", converter.createNodeWithRange(
					"TSEnumBody",
					[2]int{enum.Members.Pos() - 1, node.End()},
					[]Property{{"members", members}},
				)},
				{"const", Boolean(hasModifier(ast.KindConstKeyword, node))},
				{"declare", Boolean(hasModifier(ast.KindDeclareKeyword, node))},
				{"id", converter.convertChild(node.Name(), nil)},
			},
		)
		return converter.fixExports(node, result)

	case ast.KindEnumMember:
		member := node.AsEnumMember()
		var initializer *Node
		if member.Initializer != nil {
			initializer = converter.convertChild(member.Initializer, nil)
		}
		return converter.createNode(node, "TSEnumMember", []Property{
			{"id", converter.convertChild(node.Name(), nil)},
			{"initializer", initializer},
		})

	case ast.KindModuleDeclaration:
		return converter.convertModuleDeclaration(node)

	case ast.KindParenthesizedType:
		return converter.convertChild(node.AsParenthesizedTypeNode().Type, nil)

	case ast.KindUnionType:
		return converter.createNode(node, "TSUnionType", []Property{
			{"types", converter.convertChildren(node.AsUnionTypeNode().Types.Nodes, nil)},
		})

	case ast.KindIntersectionType:
		return converter.createNode(node, "TSIntersectionType", []Property{
			{"types", converter.convertChildren(node.AsIntersectionTypeNode().Types.Nodes, nil)},
		})

	case ast.KindAsExpression:
		asExpression := node.AsAsExpression()
		return converter.createNode(node, "TSAsExpression", []Property{
			{"expression", converter.convertChild(asExpression.Expression, nil)},
			{"typeAnnotation", converter.convertChild(asExpression.Type, nil)},
		})

	case ast.KindInferType:
		return converter.createNode(node, "TSInferType", []Property{
			{"typeParameter", converter.convertChild(node.AsInferTypeNode().TypeParameter, nil)},
		})

	case ast.KindLiteralType:
		literal := node.AsLiteralTypeNode().Literal
		if literal.Kind == ast.KindNullKeyword {
			return converter.createNode(literal, "TSNullKeyword", nil)
		}
		return converter.createNode(node, "TSLiteralType", []Property{
			{"literal", converter.convertChild(literal, nil)},
		})

	case ast.KindTypeAssertionExpression:
		assertion := node.AsTypeAssertion()
		return converter.createNode(node, "TSTypeAssertion", []Property{
			{"expression", converter.convertChild(assertion.Expression, nil)},
			{"typeAnnotation", converter.convertChild(assertion.Type, nil)},
		})

	case ast.KindImportEqualsDeclaration:
		importEquals := node.AsImportEqualsDeclaration()
		importKind := "value"
		if importEquals.IsTypeOnly {
			importKind = "type"
		}
		return converter.fixExports(node, converter.createNode(node, "TSImportEqualsDeclaration", []Property{
			{"id", converter.convertChild(node.Name(), nil)},
			{"importKind", String(importKind)},
			{"moduleReference", converter.convertChild(importEquals.ModuleReference, nil)},
		}))

	case ast.KindExternalModuleReference:
		return converter.createNode(node, "TSExternalModuleReference", []Property{
			{"expression", converter.convertChild(node.AsExternalModuleReference().Expression, nil)},
		})

	case ast.KindNamespaceExportDeclaration:
		return converter.createNode(node, "TSNamespaceExportDeclaration", []Property{
			{"id", converter.convertChild(node.Name(), nil)},
		})

	case ast.KindAbstractKeyword:
		return converter.createNode(node, "TSAbstractKeyword", nil)

	case ast.KindTupleType:
		return converter.createNode(node, "TSTupleType", []Property{
			{"elementTypes", converter.convertChildren(node.AsTupleTypeNode().Elements.Nodes, nil)},
		})

	case ast.KindNamedTupleMember:
		named := node.AsNamedTupleMember()
		member := converter.createNode(node, "TSNamedTupleMember", []Property{
			{"elementType", converter.convertChild(named.Type, node)},
			{"label", converter.convertChild(node.Name(), node)},
			{"optional", Boolean(named.QuestionToken != nil)},
		})
		if named.DotDotDotToken != nil {
			member.Range[0] = member.Child("label").Range[0]
			return converter.createNode(node, "TSRestType", []Property{{"typeAnnotation", member}})
		}
		return member

	case ast.KindOptionalType:
		return converter.createNode(node, "TSOptionalType", []Property{
			{"typeAnnotation", converter.convertChild(node.AsOptionalTypeNode().Type, nil)},
		})

	case ast.KindRestType:
		return converter.createNode(node, "TSRestType", []Property{
			{"typeAnnotation", converter.convertChild(node.AsRestTypeNode().Type, nil)},
		})

	case ast.KindTemplateLiteralType:
		template := node.AsTemplateLiteralTypeNode()
		types := Nodes{}
		quasis := Nodes{converter.convertChild(template.Head, nil)}
		for _, span := range template.TemplateSpans.Nodes {
			typeSpan := span.AsTemplateLiteralTypeSpan()
			types = append(types, converter.convertChild(typeSpan.Type, nil))
			quasis = append(quasis, converter.convertChild(typeSpan.Literal, nil))
		}
		return converter.createNode(node, "TSTemplateLiteralType", []Property{
			{"quasis", quasis},
			{"types", types},
		})

	case ast.KindClassStaticBlockDeclaration:
		return converter.createNode(node, "StaticBlock", []Property{
			{"body", converter.convertBodyExpressions(node.AsClassStaticBlockDeclaration().Body.Statements(), node)},
		})

	case ast.KindImportAttribute:
		attribute := node.AsImportAttribute()
		return converter.createNode(node, "ImportAttribute", []Property{
			{"key", converter.convertChild(node.Name(), nil)},
			{"value", converter.convertChild(attribute.Value, nil)},
		})

	case ast.KindSatisfiesExpression:
		satisfies := node.AsSatisfiesExpression()
		return converter.createNode(node, "TSSatisfiesExpression", []Property{
			{"expression", converter.convertChild(satisfies.Expression, nil)},
			{"typeAnnotation", converter.convertChild(satisfies.Type, nil)},
		})
	}

	// Upstream's deeplyCopy copies the TypeScript node under `TS${kind}`. Nothing in our code reaches it,
	// and a copied TypeScript node is not something the printer can print the way upstream does, so
	// the file is refused rather than formatted wrong.
	if converter.unsupported == nil {
		converter.unsupported = fmt.Errorf("unsupported syntax %s at %d", node.Kind.String(), converter.getStart(node))
	}
	return converter.createNode(node, "TS"+strings.TrimPrefix(node.Kind.String(), "Kind"), nil)
}

// convertEntityNameAsExpression converts an entity name the way upstream converts the property
// access it was parsed from: a qualified name becomes a MemberExpression.
func (converter *Converter) convertEntityNameAsExpression(node *ast.Node) *Node {
	if node.Kind != ast.KindQualifiedName {
		return converter.convertChild(node, nil)
	}
	qualified := node.AsQualifiedName()
	return converter.createNode(node, "MemberExpression", []Property{
		{"computed", Boolean(false)},
		{"object", converter.convertEntityNameAsExpression(qualified.Left)},
		{"optional", Boolean(false)},
		{"property", converter.convertChild(qualified.Right, nil)},
	})
}

func (converter *Converter) unary(node *ast.Node, operator string) *Node {
	return converter.createNode(node, "UnaryExpression", []Property{
		{"argument", converter.convertChild(node.Expression(), nil)},
		{"operator", String(operator)},
		{"prefix", Boolean(true)},
	})
}
