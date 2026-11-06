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
	methodRange := [2]int{node.ParameterList().Pos() - 1, node.End()}
	method := converter.createNodeWithRange(functionType, methodRange, []Property{
		{"async", Boolean(hasModifier(ast.KindAsyncKeyword, node))},
		{"body", converter.convertChild(body, nil)},
		{"declare", Boolean(false)},
		{"expression", Boolean(false)},
		{"generator", Boolean(asterisk)},
		{"id", nil},
		{"params", noNodes},
		{"returnType", converter.returnType(node)},
		{"typeParameters", converter.convertTypeParameters(node)},
	})
	if typeParameters := method.Child("typeParameters"); typeParameters != nil {
		fixParentLocation(method, typeParameters.Range)
	}

	var result *Node
	if parent.Kind == ast.KindObjectLiteralExpression {
		method.Set("params", converter.convertChildren(converter.parameterList(node), nil))
		result = converter.createNode(node, "Property", []Property{
			{"computed", Boolean(isComputedProperty(node.Name()))},
			{"key", converter.convertChild(node.Name(), nil)},
			{"kind", String("init")},
			{"method", Boolean(node.Kind == ast.KindMethodDeclaration)},
			{"optional", Boolean(isOptional(node))},
			{"shorthand", Boolean(false)},
			{"value", method},
		})
	} else {
		isAbstract := hasModifier(ast.KindAbstractKeyword, node)
		method.Set("params", converter.convertParameters(converter.parameterList(node)))
		methodDefinitionType := "MethodDefinition"
		if isAbstract {
			methodDefinitionType = "TSAbstractMethodDefinition"
		}
		result = converter.createNode(node, methodDefinitionType, []Property{
			{"accessibility", getTsNodeAccessibility(node)},
			{"computed", Boolean(isComputedProperty(node.Name()))},
			{"decorators", converter.convertChildren(decorators(node), nil)},
			{"key", converter.convertChild(node.Name(), nil)},
			{"kind", String("method")},
			{"optional", Boolean(isOptional(node))},
			{"override", Boolean(hasModifier(ast.KindOverrideKeyword, node))},
			{"static", Boolean(hasModifier(ast.KindStaticKeyword, node))},
			{"value", method},
		})
	}

	switch {
	case node.Kind == ast.KindGetAccessor:
		result.Set("kind", String("get"))
	case node.Kind == ast.KindSetAccessor:
		result.Set("kind", String("set"))
	case !result.Bool("static") && node.Name().Kind == ast.KindStringLiteral && node.Name().Text() == "constructor" &&
		result.Type() != "Property":
		result.Set("kind", String("constructor"))
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
	constructorRange := [2]int{node.ParameterList().Pos() - 1, node.End()}
	constructor := converter.createNodeWithRange(functionType, constructorRange, []Property{
		{"async", Boolean(false)},
		{"body", converter.convertChild(body, nil)},
		{"declare", Boolean(false)},
		{"expression", Boolean(false)},
		{"generator", Boolean(false)},
		{"id", nil},
		{"params", converter.convertParameters(converter.parameterList(node))},
		{"returnType", converter.returnType(node)},
		{"typeParameters", converter.convertTypeParameters(node)},
	})
	if typeParameters := constructor.Child("typeParameters"); typeParameters != nil {
		fixParentLocation(constructor, typeParameters.Range)
	}

	var constructorKey *Node
	if constructorToken.kind == ast.KindStringLiteral {
		constructorKey = converter.createNodeWithRange(
			"Literal",
			[2]int{constructorToken.start, constructorToken.end},
			[]Property{
				{"raw", String(converter.text[constructorToken.start:constructorToken.end])},
				{"value", String("constructor")},
			},
		)
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
	return converter.createNode(node, methodType, []Property{
		{"accessibility", getTsNodeAccessibility(node)},
		{"computed", Boolean(false)},
		{"decorators", noNodes},
		{"key", constructorKey},
		{"kind", String(kind)},
		{"optional", Boolean(false)},
		{"override", Boolean(false)},
		{"static", Boolean(isStatic)},
		{"value", constructor},
	})
}

// convertBindingElement is upstream's BindingElement case.
func (converter *Converter) convertBindingElement(node *ast.Node, parent *ast.Node) *Node {
	element := node.AsBindingElement()
	if parent.Kind == ast.KindArrayBindingPattern {
		arrayItem := converter.convertChild(node.Name(), parent)
		if element.Initializer != nil {
			return converter.createNode(node, "AssignmentPattern", []Property{
				{"decorators", noNodes},
				{"left", arrayItem},
				{"optional", Boolean(false)},
				{"right", converter.convertChild(element.Initializer, nil)},
				{"typeAnnotation", nil},
			})
		}
		if element.DotDotDotToken != nil {
			return converter.createNode(node, "RestElement", []Property{
				{"argument", arrayItem},
				{"decorators", noNodes},
				{"optional", Boolean(false)},
				{"typeAnnotation", nil},
				{"value", nil},
			})
		}
		return arrayItem
	}

	var result *Node
	if element.DotDotDotToken != nil {
		argument := element.PropertyName
		if argument == nil {
			argument = node.Name()
		}
		result = converter.createNode(node, "RestElement", []Property{
			{"argument", converter.convertChild(argument, nil)},
			{"decorators", noNodes},
			{"optional", Boolean(false)},
			{"typeAnnotation", nil},
			{"value", nil},
		})
	} else {
		key := element.PropertyName
		if key == nil {
			key = node.Name()
		}
		result = converter.createNode(node, "Property", []Property{
			{"computed", Boolean(
				element.PropertyName != nil && element.PropertyName.Kind == ast.KindComputedPropertyName,
			)},
			{"key", converter.convertChild(key, nil)},
			{"kind", String("init")},
			{"method", Boolean(false)},
			{"optional", Boolean(false)},
			{"shorthand", Boolean(element.PropertyName == nil)},
			{"value", converter.convertChild(node.Name(), nil)},
		})
	}
	if element.Initializer != nil {
		result.Set("value", converter.createNodeWithRange(
			"AssignmentPattern",
			[2]int{converter.getStart(node.Name()), element.Initializer.End()},
			[]Property{
				{"decorators", noNodes},
				{"left", converter.convertChild(node.Name(), nil)},
				{"optional", Boolean(false)},
				{"right", converter.convertChild(element.Initializer, nil)},
				{"typeAnnotation", nil},
			},
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
		parameter = converter.createNode(node, "RestElement", []Property{
			{"argument", converter.convertChild(node.Name(), nil)},
			{"decorators", noNodes},
			{"optional", Boolean(false)},
			{"typeAnnotation", nil},
			{"value", nil},
		})
		result = parameter
	case declaration.Initializer != nil:
		parameter = converter.convertChild(node.Name(), nil)
		result = converter.createNodeWithRange(
			"AssignmentPattern",
			[2]int{converter.getStart(node.Name()), declaration.Initializer.End()},
			[]Property{
				{"decorators", noNodes},
				{"left", parameter},
				{"optional", Boolean(false)},
				{"right", converter.convertChild(declaration.Initializer, nil)},
				{"typeAnnotation", nil},
			},
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
		parameter.Set("optional", Boolean(true))
	}
	if len(modifiers(node)) > 0 {
		return converter.createNode(node, "TSParameterProperty", []Property{
			{"accessibility", getTsNodeAccessibility(node)},
			{"decorators", noNodes},
			{"override", Boolean(hasModifier(ast.KindOverrideKeyword, node))},
			{"parameter", result},
			{"readonly", Boolean(hasModifier(ast.KindReadonlyKeyword, node))},
			{"static", Boolean(hasModifier(ast.KindStaticKeyword, node))},
		})
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
	result := converter.createNode(node, classType, []Property{
		{"abstract", Boolean(hasModifier(ast.KindAbstractKeyword, node))},
		{"body", converter.createNodeWithRange("ClassBody", [2]int{classLike.Members.Pos() - 1, node.End()}, []Property{
			{"body", converter.convertChildren(members, nil)},
		})},
		{"declare", Boolean(hasModifier(ast.KindDeclareKeyword, node))},
		{"decorators", converter.convertChildren(decorators(node), nil)},
		{"id", converter.convertChild(node.Name(), nil)},
		{"implements", converter.convertChildren(implements, nil)},
		{"superClass", superClass},
		{"superTypeArguments", nil},
		{"typeParameters", converter.convertTypeParameters(node)},
	})
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
	var phaseValue Value
	if phase == "defer" {
		phaseValue = String("defer")
	}
	specifiers := Nodes{}
	result := converter.createNode(node, "ImportDeclaration", []Property{
		{"attributes", converter.convertImportAttributes(declaration.Attributes)},
		{"importKind", String("value")},
		{"phase", phaseValue},
		{"source", converter.convertChild(declaration.ModuleSpecifier, nil)},
		{"specifiers", specifiers},
	})
	if declaration.ImportClause != nil {
		clause := declaration.ImportClause.AsImportClause()
		if phase == "type" {
			result.Set("importKind", String("type"))
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
		var properties Nodes
		for _, attribute := range attributes.Attributes.Nodes {
			properties = append(properties, converter.createNode(attribute, "Property", []Property{
				{"computed", Boolean(false)},
				{"key", converter.convertChild(attribute.Name(), nil)},
				{"kind", String("init")},
				{"method", Boolean(false)},
				{"optional", Boolean(false)},
				{"shorthand", Boolean(false)},
				{"value", converter.convertChild(attribute.AsImportAttribute().Value, nil)},
			}))
		}
		value := converter.createNode(importType.Attributes, "ObjectExpression", []Property{{"properties", properties}})
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
			[]Property{
				{"properties", Nodes{converter.createNodeWithRange(
					"Property",
					[2]int{withOrAssert.start, importType.Attributes.End()},
					[]Property{
						{"computed", Boolean(false)},
						{"key", converter.identifierNode(
							[2]int{withOrAssert.start, withOrAssert.end},
							withOrAssertName,
						)},
						{"kind", String("init")},
						{"method", Boolean(false)},
						{"optional", Boolean(false)},
						{"shorthand", Boolean(false)},
						{"value", value},
					},
				)}},
			},
		)
	}

	argument := converter.convertChild(importType.Argument, nil)
	typeArguments := converter.convertTypeArguments(node)
	result := converter.createNodeWithRange("TSImportType", nodeRange, []Property{
		{"options", options},
		{"qualifier", converter.convertChild(importType.Qualifier, nil)},
		{"source", argument.Child("literal")},
		{"typeArguments", typeArguments},
	})
	if importType.IsTypeOf {
		return converter.createNode(node, "TSTypeQuery", []Property{
			{"exprName", result},
			{"typeArguments", nil},
		})
	}
	return result
}

// convertModuleDeclaration is upstream's ModuleDeclaration case.
func (converter *Converter) convertModuleDeclaration(node *ast.Node) *Node {
	module := node.AsModuleDeclaration()
	isDeclare := hasModifier(ast.KindDeclareKeyword, node)
	isGlobal := module.Keyword == ast.KindGlobalKeyword
	result := converter.createNode(node, "TSModuleDeclaration", nil)

	switch {
	case isGlobal:
		result.Set("body", converter.convertChild(module.Body, nil))
		result.Set("declare", Boolean(false))
		result.Set("global", Boolean(false))
		result.Set("id", converter.convertChild(node.Name(), nil))
		result.Set("kind", String("global"))
	case node.Name().Kind == ast.KindStringLiteral:
		body := converter.convertChild(module.Body, nil)
		result.Set("kind", String("module"))
		if body != nil {
			result.Set("body", body)
		}
		result.Set("declare", Boolean(false))
		result.Set("global", Boolean(false))
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
			name = converter.createNodeWithRange("TSQualifiedName", [2]int{name.Range[0], right.Range[1]}, []Property{
				{"left", name},
				{"right", right},
			})
		}
		kind := "module"
		if innermost.AsModuleDeclaration().Keyword == ast.KindNamespaceKeyword {
			kind = "namespace"
		}
		result.Set("body", converter.convertChild(innermost.Body(), nil))
		result.Set("declare", Boolean(false))
		result.Set("global", Boolean(false))
		result.Set("id", name)
		result.Set("kind", String(kind))
	}

	result.Set("declare", Boolean(isDeclare))
	if isGlobal {
		result.Set("global", Boolean(true))
	}
	return converter.fixExports(node, result)
}

// convertBinary is upstream's BinaryExpression case.
func (converter *Converter) convertBinary(node *ast.Node) *Node {
	binary := node.AsBinaryExpression()
	operatorKind := binary.OperatorToken.Kind
	if operatorKind == ast.KindCommaToken {
		expressions := Nodes{}
		left := converter.convertChild(binary.Left, nil)
		if left.Is("SequenceExpression") && binary.Left.Kind != ast.KindParenthesizedExpression {
			expressions = append(expressions, left.List("expressions")...)
		} else {
			expressions = append(expressions, left)
		}
		expressions = append(expressions, converter.convertChild(binary.Right, nil))
		return converter.createNode(node, "SequenceExpression", []Property{{"expressions", expressions}})
	}

	expressionType, operator := binaryExpressionType(operatorKind)
	if converter.allowPattern && expressionType == "AssignmentExpression" {
		return converter.createNode(node, "AssignmentPattern", []Property{
			{"decorators", noNodes},
			{"left", converter.convertPattern(binary.Left, node)},
			{"optional", Boolean(false)},
			{"right", converter.convertChild(binary.Right, nil)},
			{"typeAnnotation", nil},
		})
	}
	return converter.createNode(node, expressionType, []Property{
		{"operator", String(operator)},
		{"left", converter.converter(binary.Left, node, expressionType == "AssignmentExpression")},
		{"right", converter.convertChild(binary.Right, nil)},
	})
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
