package estree

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
)

// convertMethod is upstream's GetAccessor, SetAccessor and MethodDeclaration case outside type members.
func (converter *Converter) convertMethod(node *ast.Node, parent *ast.Node) *Node {
	body := node.Body()
	functionType := "FunctionExpression"
	if body == nil {
		functionType = "TSEmptyBodyFunctionExpression"
	}
	var asterisk bool
	if node.Kind == ast.KindMethodDeclaration {
		asterisk = node.AsMethodDeclaration().AsteriskToken != nil
	}
	method := converter.createNodeWithRange(functionType, [2]int{node.ParameterList().Pos() - 1, node.End()},
		"async", hasModifier(ast.KindAsyncKeyword, node),
		"body", converter.convertChild(body, nil),
		"declare", false,
		"expression", false,
		"generator", asterisk,
		"id", nil,
		"params", noNodes,
		"returnType", converter.returnType(node),
		"typeParameters", converter.convertTypeParameters(node))
	if typeParameters := method.Child("typeParameters"); typeParameters != nil {
		fixParentLocation(method, typeParameters.Range)
	}

	var result *Node
	if parent.Kind == ast.KindObjectLiteralExpression {
		method.Set("params", converter.convertChildren(converter.parameterList(node), nil))
		result = converter.createNode(node, "Property",
			"computed", isComputedProperty(node.Name()),
			"key", converter.convertChild(node.Name(), nil),
			"kind", "init",
			"method", node.Kind == ast.KindMethodDeclaration,
			"optional", isOptional(node),
			"shorthand", false,
			"value", method)
	} else {
		isAbstract := hasModifier(ast.KindAbstractKeyword, node)
		method.Set("params", converter.convertParameters(converter.parameterList(node)))
		methodDefinitionType := "MethodDefinition"
		if isAbstract {
			methodDefinitionType = "TSAbstractMethodDefinition"
		}
		result = converter.createNode(node, methodDefinitionType,
			"accessibility", getTSNodeAccessibility(node),
			"computed", isComputedProperty(node.Name()),
			"decorators", converter.convertChildren(decorators(node), nil),
			"key", converter.convertChild(node.Name(), nil),
			"kind", "method",
			"optional", isOptional(node),
			"override", hasModifier(ast.KindOverrideKeyword, node),
			"static", hasModifier(ast.KindStaticKeyword, node),
			"value", method)
	}

	switch {
	case node.Kind == ast.KindGetAccessor:
		result.Set("kind", "get")
	case node.Kind == ast.KindSetAccessor:
		result.Set("kind", "set")
	case !result.Bool("static") && node.Name().Kind == ast.KindStringLiteral && node.Name().Text() == "constructor" &&
		result.Type() != "Property":
		result.Set("kind", "constructor")
	}
	return result
}

// convertConstructor is upstream's Constructor case.
func (converter *Converter) convertConstructor(node *ast.Node) *Node {
	nodeModifiers := modifiers(node)
	var constructorToken token
	if len(nodeModifiers) > 0 {
		constructorToken = converter.tokenAt(nodeModifiers[len(nodeModifiers)-1].End())
	} else {
		constructorToken = converter.firstToken(node)
	}

	body := node.Body()
	functionType := "FunctionExpression"
	if body == nil {
		functionType = "TSEmptyBodyFunctionExpression"
	}
	constructor := converter.createNodeWithRange(functionType, [2]int{node.ParameterList().Pos() - 1, node.End()},
		"async", false,
		"body", converter.convertChild(body, nil),
		"declare", false,
		"expression", false,
		"generator", false,
		"id", nil,
		"params", converter.convertParameters(converter.parameterList(node)),
		"returnType", converter.returnType(node),
		"typeParameters", converter.convertTypeParameters(node))
	if typeParameters := constructor.Child("typeParameters"); typeParameters != nil {
		fixParentLocation(constructor, typeParameters.Range)
	}

	var constructorKey *Node
	if constructorToken.kind == ast.KindStringLiteral {
		constructorKey = converter.createNodeWithRange("Literal", [2]int{constructorToken.start, constructorToken.end},
			"raw", converter.text[constructorToken.start:constructorToken.end],
			"value", "constructor")
	} else {
		constructorKey = converter.identifierNode([2]int{constructorToken.start, constructorToken.end}, "constructor")
	}

	isStatic := hasModifier(ast.KindStaticKeyword, node)
	methodType := "MethodDefinition"
	if hasModifier(ast.KindAbstractKeyword, node) {
		methodType = "TSAbstractMethodDefinition"
	}
	kind := "constructor"
	if isStatic {
		kind = "method"
	}
	return converter.createNode(node, methodType,
		"accessibility", getTSNodeAccessibility(node),
		"computed", false,
		"decorators", noNodes,
		"key", constructorKey,
		"kind", kind,
		"optional", false,
		"override", false,
		"static", isStatic,
		"value", constructor)
}

// convertBindingElement is upstream's BindingElement case.
func (converter *Converter) convertBindingElement(node *ast.Node, parent *ast.Node) *Node {
	element := node.AsBindingElement()
	if parent.Kind == ast.KindArrayBindingPattern {
		arrayItem := converter.convertChild(node.Name(), parent)
		if element.Initializer != nil {
			return converter.createNode(node, "AssignmentPattern",
				"decorators", noNodes,
				"left", arrayItem,
				"optional", false,
				"right", converter.convertChild(element.Initializer, nil),
				"typeAnnotation", nil)
		}
		if element.DotDotDotToken != nil {
			return converter.createNode(node, "RestElement",
				"argument", arrayItem,
				"decorators", noNodes,
				"optional", false,
				"typeAnnotation", nil,
				"value", nil)
		}
		return arrayItem
	}

	var result *Node
	if element.DotDotDotToken != nil {
		argument := element.PropertyName
		if argument == nil {
			argument = node.Name()
		}
		result = converter.createNode(node, "RestElement",
			"argument", converter.convertChild(argument, nil),
			"decorators", noNodes,
			"optional", false,
			"typeAnnotation", nil,
			"value", nil)
	} else {
		key := element.PropertyName
		if key == nil {
			key = node.Name()
		}
		result = converter.createNode(node, "Property",
			"computed", element.PropertyName != nil && element.PropertyName.Kind == ast.KindComputedPropertyName,
			"key", converter.convertChild(key, nil),
			"kind", "init",
			"method", false,
			"optional", false,
			"shorthand", element.PropertyName == nil,
			"value", converter.convertChild(node.Name(), nil))
	}
	if element.Initializer != nil {
		result.Set("value", converter.createNodeWithRange(
			"AssignmentPattern",
			[2]int{converter.getStart(node.Name()), element.Initializer.End()},
			"decorators",
			noNodes,
			"left",
			converter.convertChild(node.Name(), nil),
			"optional",
			false,
			"right",
			converter.convertChild(element.Initializer, nil),
			"typeAnnotation",
			nil,
		))
	}
	return result
}

// convertParameter is upstream's Parameter case.
func (converter *Converter) convertParameter(node *ast.Node, parent *ast.Node) *Node {
	declaration := node.AsParameterDeclaration()
	var parameter *Node
	var result *Node
	switch {
	case declaration.DotDotDotToken != nil:
		parameter = converter.createNode(node, "RestElement",
			"argument", converter.convertChild(node.Name(), nil),
			"decorators", noNodes,
			"optional", false,
			"typeAnnotation", nil,
			"value", nil)
		result = parameter
	case declaration.Initializer != nil:
		parameter = converter.convertChild(node.Name(), nil)
		result = converter.createNodeWithRange(
			"AssignmentPattern",
			[2]int{converter.getStart(node.Name()), declaration.Initializer.End()},
			"decorators",
			noNodes,
			"left",
			parameter,
			"optional",
			false,
			"right",
			converter.convertChild(declaration.Initializer, nil),
			"typeAnnotation",
			nil,
		)
		if len(modifiers(node)) > 0 {
			// AssignmentPattern should not contain modifiers in range
			result.Range[0] = parameter.Range[0]
		}
	default:
		parameter = converter.convertChild(node.Name(), parent)
		result = parameter
	}

	if declaration.Type != nil {
		annotation := converter.convertTypeAnnotation(declaration.Type, node)
		parameter.Set("typeAnnotation", annotation)
		fixParentLocation(parameter, annotation.Range)
	}
	if declaration.QuestionToken != nil {
		if declaration.QuestionToken.End() > parameter.Range[1] {
			parameter.Range[1] = declaration.QuestionToken.End()
		}
		parameter.Set("optional", true)
	}
	if len(modifiers(node)) > 0 {
		return converter.createNode(node, "TSParameterProperty",
			"accessibility", getTSNodeAccessibility(node),
			"decorators", noNodes,
			"override", hasModifier(ast.KindOverrideKeyword, node),
			"parameter", result,
			"readonly", hasModifier(ast.KindReadonlyKeyword, node),
			"static", hasModifier(ast.KindStaticKeyword, node))
	}
	return result
}

// convertClass is upstream's ClassDeclaration and ClassExpression case.
func (converter *Converter) convertClass(node *ast.Node) *Node {
	classType := "ClassExpression"
	if node.Kind == ast.KindClassDeclaration {
		classType = "ClassDeclaration"
	}
	classLike := node.ClassLikeData()
	var extendsClause *ast.HeritageClause
	var implementsClause *ast.HeritageClause
	if classLike.HeritageClauses != nil {
		for _, clause := range classLike.HeritageClauses.Nodes {
			heritage := clause.AsHeritageClause()
			if heritage.Token == ast.KindExtendsKeyword && extendsClause == nil {
				extendsClause = heritage
			}
			if heritage.Token == ast.KindImplementsKeyword && implementsClause == nil {
				implementsClause = heritage
			}
		}
	}

	var members []*ast.Node
	for _, member := range classLike.Members.Nodes {
		if member.Kind != ast.KindSemicolonClassElement {
			members = append(members, member)
		}
	}
	var implements []*ast.Node
	if implementsClause != nil {
		implements = implementsClause.Types.Nodes
	}
	var superClass *Node
	if extendsClause != nil && len(extendsClause.Types.Nodes) > 0 {
		superClass = converter.convertChild(extendsClause.Types.Nodes[0].Expression(), nil)
	}
	result := converter.createNode(node, classType,
		"abstract", hasModifier(ast.KindAbstractKeyword, node),
		"body", converter.createNodeWithRange("ClassBody", [2]int{classLike.Members.Pos() - 1, node.End()},
			"body", converter.convertChildren(members, nil)),
		"declare", hasModifier(ast.KindDeclareKeyword, node),
		"decorators", converter.convertChildren(decorators(node), nil),
		"id", converter.convertChild(node.Name(), nil),
		"implements", converter.convertChildren(implements, nil),
		"superClass", superClass,
		"superTypeArguments", nil,
		"typeParameters", converter.convertTypeParameters(node))
	if extendsClause != nil &&
		len(extendsClause.Types.Nodes) > 0 &&
		extendsClause.Types.Nodes[0].TypeArgumentList() != nil {
		result.Set("superTypeArguments", converter.convertTypeArguments(extendsClause.Types.Nodes[0]))
	}
	return converter.fixExports(node, result)
}

// convertImportDeclaration is upstream's ImportDeclaration case.
func (converter *Converter) convertImportDeclaration(node *ast.Node) *Node {
	declaration := node.AsImportDeclaration()
	phase := importClausePhase(declaration.ImportClause)
	var phaseValue any
	if phase == "defer" {
		phaseValue = "defer"
	}
	specifiers := []*Node{}
	result := converter.createNode(node, "ImportDeclaration",
		"attributes", converter.convertImportAttributes(declaration.Attributes),
		"importKind", "value",
		"phase", phaseValue,
		"source", converter.convertChild(declaration.ModuleSpecifier, nil),
		"specifiers", specifiers)
	if declaration.ImportClause != nil {
		clause := declaration.ImportClause.AsImportClause()
		if phase == "type" {
			result.Set("importKind", "type")
		}
		if declaration.ImportClause.Name() != nil {
			specifiers = append(specifiers, converter.convertChild(declaration.ImportClause, nil))
		}
		if clause.NamedBindings != nil {
			switch clause.NamedBindings.Kind {
			case ast.KindNamespaceImport:
				specifiers = append(specifiers, converter.convertChild(clause.NamedBindings, nil))
			case ast.KindNamedImports:
				specifiers = append(
					specifiers,
					converter.convertChildren(clause.NamedBindings.AsNamedImports().Elements.Nodes, nil)...,
				)
			}
		}
		result.Set("specifiers", specifiers)
	}
	return result
}

// importClausePhase is upstream's getImportClausePhaseModifier.
func importClausePhase(clause *ast.Node) string {
	if clause == nil {
		return ""
	}
	switch clause.AsImportClause().PhaseModifier {
	case ast.KindTypeKeyword:
		return "type"
	case ast.KindDeferKeyword:
		return "defer"
	}
	return ""
}

// convertImportType is upstream's ImportType case.
func (converter *Converter) convertImportType(node *ast.Node) *Node {
	importType := node.AsImportTypeNode()
	nodeRange := converter.getRange(node)
	if importType.IsTypeOf {
		first := converter.firstToken(node)
		next := converter.tokenAt(first.end)
		nodeRange[0] = next.start
	}

	var options *Node
	if importType.Attributes != nil {
		attributes := importType.Attributes.AsImportAttributes()
		var properties []*Node
		for _, attribute := range attributes.Attributes.Nodes {
			properties = append(properties, converter.createNode(attribute, "Property",
				"computed", false,
				"key", converter.convertChild(attribute.Name(), nil),
				"kind", "init",
				"method", false,
				"optional", false,
				"shorthand", false,
				"value", converter.convertChild(attribute.AsImportAttribute().Value, nil)))
		}
		value := converter.createNode(importType.Attributes, "ObjectExpression", "properties", properties)
		comma := converter.tokenAt(importType.Argument.End())
		openBrace := converter.tokenAt(comma.end)
		afterAttributes := converter.tokenAt(importType.Attributes.End())
		closeBrace := afterAttributes
		if afterAttributes.kind == ast.KindCommaToken {
			closeBrace = converter.tokenAt(afterAttributes.end)
		}
		withOrAssert := converter.tokenAt(openBrace.end)
		withOrAssertName := "with"
		if withOrAssert.kind == ast.KindAssertKeyword {
			withOrAssertName = "assert"
		}
		options = converter.createNodeWithRange(
			"ObjectExpression",
			[2]int{openBrace.start, closeBrace.end},
			"properties",
			[]*Node{converter.createNodeWithRange("Property", [2]int{withOrAssert.start, importType.Attributes.End()},
				"computed", false,
				"key", converter.identifierNode([2]int{withOrAssert.start, withOrAssert.end}, withOrAssertName),
				"kind", "init",
				"method", false,
				"optional", false,
				"shorthand", false,
				"value", value)},
		)
	}

	argument := converter.convertChild(importType.Argument, nil)
	typeArguments := converter.convertTypeArguments(node)
	result := converter.createNodeWithRange("TSImportType", nodeRange,
		"options", options,
		"qualifier", converter.convertChild(importType.Qualifier, nil),
		"source", argument.Child("literal"),
		"typeArguments", typeArguments)
	if importType.IsTypeOf {
		return converter.createNode(node, "TSTypeQuery",
			"exprName", result,
			"typeArguments", nil)
	}
	return result
}

// convertModuleDeclaration is upstream's ModuleDeclaration case.
func (converter *Converter) convertModuleDeclaration(node *ast.Node) *Node {
	module := node.AsModuleDeclaration()
	isDeclare := hasModifier(ast.KindDeclareKeyword, node)
	isGlobal := module.Keyword == ast.KindGlobalKeyword
	result := converter.createNode(node, "TSModuleDeclaration")

	switch {
	case isGlobal:
		result.Set("body", converter.convertChild(module.Body, nil))
		result.Set("declare", false)
		result.Set("global", false)
		result.Set("id", converter.convertChild(node.Name(), nil))
		result.Set("kind", "global")
	case node.Name().Kind == ast.KindStringLiteral:
		body := converter.convertChild(module.Body, nil)
		result.Set("kind", "module")
		if body != nil {
			result.Set("body", body)
		}
		result.Set("declare", false)
		result.Set("global", false)
		result.Set("id", converter.convertChild(node.Name(), nil))
	default:
		innermost := node
		name := converter.identifierNode([2]int{converter.getStart(node.Name()), node.Name().End()}, node.Name().Text())
		for innermost.Body() != nil &&
			innermost.Body().Kind == ast.KindModuleDeclaration &&
			innermost.Body().Name() != nil {
			innermost = innermost.Body()
			isDeclare = isDeclare || hasModifier(ast.KindDeclareKeyword, innermost)
			nextName := innermost.Name()
			right := converter.identifierNode([2]int{converter.getStart(nextName), nextName.End()}, nextName.Text())
			name = converter.createNodeWithRange("TSQualifiedName", [2]int{name.Range[0], right.Range[1]},
				"left", name,
				"right", right)
		}
		kind := "module"
		if innermost.AsModuleDeclaration().Keyword == ast.KindNamespaceKeyword {
			kind = "namespace"
		}
		result.Set("body", converter.convertChild(innermost.Body(), nil))
		result.Set("declare", false)
		result.Set("global", false)
		result.Set("id", name)
		result.Set("kind", kind)
	}

	result.Set("declare", isDeclare)
	if isGlobal {
		result.Set("global", true)
	}
	return converter.fixExports(node, result)
}

// convertBinary is upstream's BinaryExpression case.
func (converter *Converter) convertBinary(node *ast.Node) *Node {
	binary := node.AsBinaryExpression()
	operatorKind := binary.OperatorToken.Kind
	if operatorKind == ast.KindCommaToken {
		expressions := []*Node{}
		left := converter.convertChild(binary.Left, nil)
		if left.Is("SequenceExpression") && binary.Left.Kind != ast.KindParenthesizedExpression {
			expressions = append(expressions, left.List("expressions")...)
		} else {
			expressions = append(expressions, left)
		}
		expressions = append(expressions, converter.convertChild(binary.Right, nil))
		return converter.createNode(node, "SequenceExpression", "expressions", expressions)
	}

	expressionType, operator := binaryExpressionType(operatorKind)
	if converter.allowPattern && expressionType == "AssignmentExpression" {
		return converter.createNode(node, "AssignmentPattern",
			"decorators", noNodes,
			"left", converter.convertPattern(binary.Left, node),
			"optional", false,
			"right", converter.convertChild(binary.Right, nil),
			"typeAnnotation", nil)
	}
	return converter.createNode(node, expressionType,
		"operator", operator,
		"left", converter.converter(binary.Left, node, expressionType == "AssignmentExpression"),
		"right", converter.convertChild(binary.Right, nil))
}

// binaryExpressionType is upstream's getBinaryExpressionType.
func binaryExpressionType(kind ast.Kind) (string, string) {
	operator := scanner.TokenToString(kind)
	switch kind {
	case ast.KindAmpersandAmpersandEqualsToken, ast.KindAmpersandEqualsToken, ast.KindAsteriskAsteriskEqualsToken,
		ast.KindAsteriskEqualsToken, ast.KindBarBarEqualsToken, ast.KindBarEqualsToken, ast.KindCaretEqualsToken,
		ast.KindEqualsToken, ast.KindGreaterThanGreaterThanEqualsToken,
		ast.KindGreaterThanGreaterThanGreaterThanEqualsToken, ast.KindLessThanLessThanEqualsToken,
		ast.KindMinusEqualsToken, ast.KindPercentEqualsToken, ast.KindPlusEqualsToken,
		ast.KindQuestionQuestionEqualsToken, ast.KindSlashEqualsToken:
		return "AssignmentExpression", operator
	case ast.KindAmpersandAmpersandToken, ast.KindBarBarToken, ast.KindQuestionQuestionToken:
		return "LogicalExpression", operator
	}
	return "BinaryExpression", operator
}
