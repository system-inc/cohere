package nexus

import (
	"strings"
	"unicode"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/ecmascript/imports"
	"github.com/system-inc/cohere/internal/lint/ecmascript/nextjs"
	"github.com/system-inc/cohere/internal/lint/ecmascript/react"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

// The rule's messages, whose wording lives in `policy/messages/consistency-require-identifier-casing.json`.
var (
	identifierCasingValueText = policy.MessageOf(
		"nexus/consistency-require-identifier-casing",
		"requireLowerCamelCaseValue",
	)
	identifierCasingTypeText = policy.MessageOf(
		"nexus/consistency-require-identifier-casing",
		"requireUpperCamelCaseType",
	)
	identifierCasingCapitalText = policy.MessageOf(
		"nexus/consistency-require-identifier-casing",
		"requireUpperCamelCaseCapital",
	)
	identifierCasingLeadingUnderscoreText = policy.MessageOf(
		"nexus/consistency-require-identifier-casing",
		"noLeadingUnderscore",
	)
)

// messageIdentifierCasing renders a finding's message. A name opening on an underscore is told about the
// underscore, whatever casing its suggestion takes, as the Go and Swift engines tell it, since that is
// what the name chose and the advice removes.
func messageIdentifierCasing(
	text policy.MessageHandle,
	declaration string,
	name string,
	suggestion string,
) rule.Message {
	reportedText := text
	if strings.HasPrefix(name, "_") {
		reportedText = identifierCasingLeadingUnderscoreText
	}
	return rule.Message{
		Id: reportedText.Id,
		Description: reportedText.Render(
			map[string]string{"declaration": declaration, "name": name, "suggestion": suggestion},
		),
	}
}

// ConsistencyRequireIdentifierCasing spells values in lowerCamelCase and types in UpperCamelCase, at the
// declaration, which is what the Go and Swift engines already judge (#h97gacx, the text rules' casing
// family).
//
//	valid:   function sum(firstValue: number) { let runningTotal = firstValue }
//	valid:   function OrderList() { return <ul /> }
//	invalid: function sum(first_value: number) {}
//	invalid: let running_total = 0
//	invalid: interface Order_Line {}
//
// Values are parameters, `let` and `var` bindings, a `const` inside a function or a block, functions,
// and methods. Types are classes, interfaces, type aliases and enums. A name is judged once, where it is
// declared, and its reads are not reported again.
//
// A name judged as UpperCamelCase may open with a brand policy/Brands.json lists, spelled exactly as its
// owner spells it, followed by nothing or by an UpperCamelCase rest: `iMessageCommandLineInterface` passes,
// and `iMessagecommandLineInterface` is told `iMessageCommandLineInterface`. It is vocabulary, as the
// shouting and abbreviation lists are, and the file states the bar a brand meets to join it.
//
// Each shape has one owner, so no name is judged twice by rules that could disagree. A `const` of the
// module or of a namespace body belongs to consistency-require-constant-casing, whose casing there
// signals reach, and a SCREAMING_SNAKE_CASE `const` at any scope to consistency-no-screaming-snake-case, which
// judges exactly that: a `const` bound to a plain name. A shouting `let`, parameter, function or type has no
// other owner, so it is judged here like any other name that is not camel case.
//
// A property name is not judged, of an object type, an interface, a class or an object literal. In
// TypeScript a property's name is its wire key: Go keeps a field's wire spelling in a tag and Swift in
// CodingKeys, so a hand-written `created_at` or `REDDIT_PIXEL_ID` mirror can be told from a defect
// there, and here it cannot, by types or otherwise (the text rules' second principle). Structure's
// UpperCamelCase variant keys are the house's convention besides.
//
// A value keeps its capital where something reads the capital, decided by structure and types:
//
//   - a component: a function React's own rules call one, or whose every return is JSX, `null` or a
//     React node by the checker, a Next.js page's `return null` or `redirect(...)` default export
//     included; or a value
//     the file renders as a JSX tag, or whose type is constructed or called for an element;
//   - a decorator factory, whose return type the checker reads as a class, member or parameter
//     decorator, as Base's `Inject*` and `With*` are;
//   - a constructor: a function the file calls with `new`, or one that assigns properties of `this`.
//
// A leading underscore is flagged on every name this rule judges, as the Go and Swift engines flag it
// (Kirk, 2026-11-06, #4b337jk, case us05), and so is a trailing one, which both of them report too:
// `_cache` is told `cache`, `__meta__` `meta` and `event_` `event`. The underscore is a convention for
// private or for unused, and TypeScript says private with `#` or `private`, which the compiler checks,
// while an unused name is deleted, or written as the bare `_`.
//
// Left alone, besides: the bare `_` discard, and any name that is nothing but underscores, which is no
// name; a name that only escapes a reserved word, `class_` or `_default`; a member a base or an
// implemented type declares, which is judged at the base, an `override` included; a decorated method,
// whose decorator is handed its name; an ambient declaration, which describes something under the
// spelling its owner chose; an export Next.js reads by name; a value read from an external grammar into
// a name that spells it exactly; and a name the converter cannot turn into a passing one, since the
// advice has to be legal.
//
// A parameter, a local and a function nothing outside the file can name are renamed by the fix, at the
// declaration and every reference, through the checker-resolved rename consistency-require-constant-
// casing uses. A method, a type and anything exported are reported without one, since their references
// cross files.
var ConsistencyRequireIdentifierCasing = rule.Rule[rule.NoOptions]{
	Name:             "nexus/consistency-require-identifier-casing",
	NeedsTypeChecker: true,
	Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
		judgeMethod := func(node *ast.Node) { judgeIdentifierCasingMethod(ruleContext, node) }
		judgeType := func(node *ast.Node) { judgeIdentifierCasingType(ruleContext, node) }
		return rule.Listeners{
			ast.KindParameter:           func(node *ast.Node) { judgeIdentifierCasingParameter(ruleContext, node) },
			ast.KindVariableDeclaration: func(node *ast.Node) { judgeIdentifierCasingVariable(ruleContext, node) },
			ast.KindBindingElement: func(
				node *ast.Node,
			) {
				judgeIdentifierCasingBindingElement(ruleContext, node)
			},
			ast.KindFunctionDeclaration:  func(node *ast.Node) { judgeIdentifierCasingFunction(ruleContext, node) },
			ast.KindMethodDeclaration:    judgeMethod,
			ast.KindMethodSignature:      judgeMethod,
			ast.KindClassDeclaration:     judgeType,
			ast.KindClassExpression:      judgeType,
			ast.KindInterfaceDeclaration: judgeType,
			ast.KindTypeAliasDeclaration: judgeType,
			ast.KindEnumDeclaration:      judgeType,
		}
	},
}

// judgeIdentifierCasingParameter judges a parameter bound to a plain name. A destructured one is judged
// at its elements, and a parameter property is a class property, whose name is not judged.
func judgeIdentifierCasingParameter(ruleContext rule.Context, node *ast.Node) {
	name := node.Name()
	if name == nil || name.Kind != ast.KindIdentifier || name.Text() == "this" || isAmbientDeclaration(node) ||
		ast.IsParameterPropertyDeclaration(node, node.Parent) {
		return
	}
	judgeIdentifierCasingValue(ruleContext, name, "parameter", node.Initializer(), true)
}

// judgeIdentifierCasingVariable judges a `let` or `var`, or a `const` below the module's own statements,
// bound to a plain name. A catch clause's binding is the language's, and has no declaration list.
func judgeIdentifierCasingVariable(ruleContext rule.Context, node *ast.Node) {
	declaration := node.AsVariableDeclaration()
	name := declaration.Name()
	list := node.Parent
	if name == nil || name.Kind != ast.KindIdentifier || list == nil || list.Kind != ast.KindVariableDeclarationList ||
		isAmbientDeclaration(node) {
		return
	}
	if list.Flags&ast.NodeFlagsConst != 0 {
		// consistency-no-screaming-snake-case's own boundary: a declaration bound to a plain name in a list
		// carrying the `const` flag, which a loop head's `const` and an `await using` carry too.
		if isScreamingSnakeCase(name.Text()) {
			return
		}
		if isModuleScopeConstant(list) || keepsAConstantShapesCasing(declaration) {
			return
		}
	}
	fixable := len(exportedNamesOf(ruleContext, list.Parent, name)) == 0
	judgeIdentifierCasingValue(ruleContext, name, "variable", declaration.Initializer, fixable)
}

// keepsAConstantShapesCasing reports a local constant whose casing another shape decides, the shapes
// consistency-require-constant-casing exempts at module scope for the same reasons: an import wearing a
// const, which import-require-node-namespace spells; a React context, component wrapper or component
// annotation, which JSX reads by its capital; an enum-shaped object, which consistency-require-type-suffix
// names; and a capital that keeps the declaration from reading itself.
func keepsAConstantShapesCasing(declaration *ast.VariableDeclaration) bool {
	return isDynamicImportBinding(declaration.Initializer) || isReactContextBinding(declaration.Initializer) ||
		isReactComponentFactory(declaration.Initializer) || isReactComponentAnnotation(declaration) ||
		isCasingConstEnumShape(declaration.Initializer) || shadowsCamelCaseSource(declaration)
}

// judgeIdentifierCasingBindingElement judges a destructured name the author chose: an array element, or
// an object element renamed with `key: name`. A shorthand `{ unit_price }` takes the property's spelling,
// and a property's name is not judged.
func judgeIdentifierCasingBindingElement(ruleContext rule.Context, node *ast.Node) {
	element := node.AsBindingElement()
	name := element.Name()
	if name == nil || name.Kind != ast.KindIdentifier {
		return
	}
	if node.Parent.Kind == ast.KindObjectBindingPattern && element.PropertyName == nil {
		return
	}
	root := node
	for root != nil && (root.Kind == ast.KindBindingElement || root.Kind == ast.KindObjectBindingPattern ||
		root.Kind == ast.KindArrayBindingPattern) {
		root = root.Parent
	}
	if root == nil || isAmbientDeclaration(root) {
		return
	}
	switch root.Kind {
	case ast.KindParameter:
		judgeIdentifierCasingValue(ruleContext, name, "parameter", nil, true)
	case ast.KindVariableDeclaration:
		list := root.Parent
		if list == nil || list.Kind != ast.KindVariableDeclarationList ||
			(list.Flags&ast.NodeFlagsConst != 0 && isModuleScopeConstant(list)) {
			return
		}
		judgeIdentifierCasingValue(
			ruleContext,
			name,
			"variable",
			nil,
			len(exportedNamesOf(ruleContext, list.Parent, name)) == 0,
		)
	}
}

// judgeIdentifierCasingFunction judges a function declaration's name, once across its overloads.
func judgeIdentifierCasingFunction(ruleContext rule.Context, node *ast.Node) {
	name := node.Name()
	if name == nil || name.Kind != ast.KindIdentifier || isAmbientDeclaration(node) ||
		!isFirstDeclaration(ruleContext, name, node) {
		return
	}
	exported := len(exportedNamesOf(ruleContext, node, name)) > 0
	// Next.js reads a route file's `GET` or `generateMetadata` by name, so the spelling is the
	// framework's, as consistency-require-constant-casing holds for the same exports written as
	// constants.
	if exported && node.Parent != nil && node.Parent.Kind == ast.KindSourceFile &&
		nextjs.IsRouteContractExport(imports.NormalizedFileName(ruleContext.SourceFile), name.Text()) {
		return
	}
	if startsWithUppercase(name.Text()) {
		switch {
		case react.IsComponentOrHookLike(node) || returnsOnlyReactNodes(ruleContext, name) ||
			neverReturns(ruleContext, node):
			judgeIdentifierCasingCapital(ruleContext, name, "component")
			return
		case returnsADecorator(ruleContext, name):
			judgeIdentifierCasingCapital(ruleContext, name, "decorator factory")
			return
		case isConstructedInTheFile(ruleContext, name) || assignsPropertiesOfThis(node):
			judgeIdentifierCasingCapital(ruleContext, name, "constructor")
			return
		}
	}
	judgeIdentifierCasingValue(ruleContext, name, "function", nil, !exported)
}

// judgeIdentifierCasingMethod judges a method of a class, or a method signature of an interface or a type
// literal. An object literal's methods are its keys, and a key is a property's name.
func judgeIdentifierCasingMethod(ruleContext rule.Context, node *ast.Node) {
	name := node.Name()
	if name == nil || (name.Kind != ast.KindIdentifier && name.Kind != ast.KindPrivateIdentifier) {
		return
	}
	owner := node.Parent
	if owner == nil {
		return
	}
	switch owner.Kind {
	case ast.KindClassDeclaration, ast.KindClassExpression, ast.KindInterfaceDeclaration, ast.KindTypeLiteral:
	default:
		return
	}
	// A decorator is handed the member's key, so a decorated method's name is the decorator's to read, as a
	// resolver's name is its GraphQL field's, and the spelling is a framework's contract like a wire key.
	if isAmbientDeclaration(node) || ast.HasSyntacticModifier(node, ast.ModifierFlagsOverride) ||
		ast.HasDecorators(node) || !isFirstDeclaration(ruleContext, name, node) {
		return
	}
	// An override, written with the keyword or without it, takes its name from the base, and the base
	// is where the name is judged.
	if name.Kind == ast.KindIdentifier && heritageDeclaresMember(ruleContext, owner, name.Text(), ast.IsStatic(node)) {
		return
	}
	// An interface merged into one a dependency declares keeps the dependency's spellings.
	if owner.Kind == ast.KindInterfaceDeclaration && ruleContext.TypeChecker != nil &&
		rule.IsDeclaredByADependency(ruleContext.TypeChecker.GetSymbolAtLocation(owner.Name())) {
		return
	}
	judgeIdentifierCasingValue(ruleContext, name, "method", nil, false)
}

// judgeIdentifierCasingType judges a class, interface, type alias or enum name, once across declarations
// that merge.
func judgeIdentifierCasingType(ruleContext rule.Context, node *ast.Node) {
	name := node.Name()
	if name == nil || name.Kind != ast.KindIdentifier || isAmbientDeclaration(node) ||
		!isFirstDeclaration(ruleContext, name, node) {
		return
	}
	// An interface merged into one a dependency declares keeps the dependency's spelling.
	if ruleContext.TypeChecker != nil &&
		rule.IsDeclaredByADependency(ruleContext.TypeChecker.GetSymbolAtLocation(name)) {
		return
	}
	declaration := "type"
	switch node.Kind {
	case ast.KindClassDeclaration, ast.KindClassExpression:
		declaration = "class"
	case ast.KindInterfaceDeclaration:
		declaration = "interface"
	case ast.KindEnumDeclaration:
		declaration = "enum"
	}
	text := name.Text()
	if !isJudgedCasing(text) || isUpperCamelCaseName(text) {
		return
	}
	suggestion := upperCamelCaseSuggestion(text)
	if !isUpperCamelCaseName(suggestion) {
		return
	}
	ruleContext.ReportNode(name, messageIdentifierCasing(identifierCasingTypeText, declaration, text, suggestion))
}

// judgeIdentifierCasingValue judges a value's name: lowerCamelCase, unless it holds a component or a
// constructor, which keeps its capital, or mirrors an external grammar exactly. A fixable name is one
// nothing outside the file can name, which the rename may then change at every reference.
func judgeIdentifierCasingValue(
	ruleContext rule.Context,
	name *ast.Node,
	declaration string,
	initializer *ast.Node,
	fixable bool,
) {
	text := strings.TrimPrefix(name.Text(), "#")
	if !isJudgedCasing(text) || isCamelCase(text) {
		return
	}
	if startsWithUppercase(text) && holdsAComponentOrConstructor(ruleContext, name) {
		judgeIdentifierCasingCapital(ruleContext, name, declaration)
		return
	}
	if mirrorsExternalSpelling(ruleContext, text, initializer) {
		return
	}
	suggestion := constantNameToCamelCase(text)
	if !isCamelCase(suggestion) {
		return
	}
	message := messageIdentifierCasing(identifierCasingValueText, declaration, text, suggestion)
	// A name opening on an acronym, `URLPath`, converts to `uRLPath`, which is advice a reader can
	// correct and a rename should not write. A shouting name's words are capitals whole, which the
	// converter lowers whole, so `MAX_SIZE` becomes `maxSize` and is renamed.
	if fixable && (!opensOnTwoCapitals(text) || isScreamingSnakeCase(text)) {
		if fix, isSafe := fileLocalRenameFix(ruleContext, name, suggestion); isSafe {
			ruleContext.ReportNodeWithFixes(name, message, fix)
			return
		}
	}
	ruleContext.ReportNode(name, message)
}

// judgeIdentifierCasingCapital judges a name that keeps its capital: UpperCamelCase, so a snake-cased
// component is told the form JSX can still read.
func judgeIdentifierCasingCapital(ruleContext rule.Context, name *ast.Node, declaration string) {
	text := strings.TrimPrefix(name.Text(), "#")
	if isUpperCamelCaseName(text) {
		return
	}
	suggestion := upperCamelCaseSuggestion(text)
	if !isUpperCamelCaseName(suggestion) {
		return
	}
	ruleContext.ReportNode(name, messageIdentifierCasing(identifierCasingCapitalText, declaration, text, suggestion))
}

// isJudgedCasing reports a name whose casing is this rule's to judge. The bare `_` is the discard, no
// name at all, and so is a name of underscores only, which has no word to advise. A reserved word's
// escape belongs to the keyword it escapes.
func isJudgedCasing(name string) bool {
	return strings.Trim(name, "_") != "" && !escapesAReservedWord(name)
}

// isAmbientDeclaration reports a declaration under `declare`, in a declaration file, or in a `declare
// global` or `declare module` block: each describes something that exists elsewhere, under the spelling
// that elsewhere chose.
func isAmbientDeclaration(node *ast.Node) bool {
	return ast.GetCombinedNodeFlags(node)&ast.NodeFlagsAmbient != 0
}

// isFirstDeclaration reports whether a declaration is its symbol's first, so overloads and merged
// declarations are judged once. Without a checker every declaration is judged.
func isFirstDeclaration(ruleContext rule.Context, name *ast.Node, declaration *ast.Node) bool {
	if ruleContext.TypeChecker == nil {
		return true
	}
	symbol := ruleContext.TypeChecker.GetSymbolAtLocation(name)
	if symbol == nil || len(symbol.Declarations) == 0 {
		return true
	}
	return symbol.Declarations[0] == declaration
}

// heritageDeclaresMember reports whether a class or interface inherits a member of this name from
// anything it extends or implements, its own or a dependency's. The name is the base's then, judged at
// the base, and an implementing class repeats it because it has to.
func heritageDeclaresMember(ruleContext rule.Context, owner *ast.Node, name string, static bool) bool {
	if ruleContext.TypeChecker == nil {
		return false
	}
	heritage := abbreviatedHeritage(owner)
	if heritage == nil {
		return false
	}
	for _, clause := range heritage.Nodes {
		if static && clause.AsHeritageClause().Token == ast.KindImplementsKeyword {
			continue
		}
		for _, base := range clause.AsHeritageClause().Types.Nodes {
			baseType := ruleContext.TypeChecker.GetTypeAtLocation(base)
			if static {
				baseType = ruleContext.TypeChecker.GetTypeAtLocation(base.Expression())
			}
			// A base that does not resolve may declare the name once it does.
			if rule.IsUnresolvedType(baseType) {
				return true
			}
			if baseType != nil && ruleContext.TypeChecker.GetPropertyOfType(baseType, name) != nil {
				return true
			}
		}
	}
	return false
}

// holdsAComponentOrConstructor reports whether a value keeps its capital because of what it holds: a name
// the file renders as a JSX tag, or a type some part of which can be constructed, or called for a React
// element. `mayReturnAnElement` counts a call the checker cannot see as one, so a component whose JSX
// types do not resolve keeps its capital too.
func holdsAComponentOrConstructor(ruleContext rule.Context, name *ast.Node) bool {
	if usageIndexFor(ruleContext).isUsedAsJsxElementName(name.Text()) {
		return true
	}
	if ruleContext.TypeChecker == nil {
		return false
	}
	valueType := ruleContext.TypeChecker.GetTypeAtLocation(name)
	if valueType == nil {
		return false
	}
	for part := range type_checking.UnionTypePartsSequence(ruleContext.TypeChecker.GetNonNullableType(valueType)) {
		if len(ruleContext.TypeChecker.GetSignaturesOfType(part, checker.SignatureKindConstruct)) > 0 {
			return true
		}
		for _, signature := range ruleContext.TypeChecker.GetSignaturesOfType(part, checker.SignatureKindCall) {
			if mayReturnAnElement(
				ruleContext.TypeChecker,
				ruleContext.TypeChecker.GetReturnTypeOfSignature(signature),
			) {
				return true
			}
		}
	}
	return false
}

// callSignaturesOf is every call signature of the value a name declares, or none without a checker.
func callSignaturesOf(ruleContext rule.Context, name *ast.Node) []*checker.Signature {
	if ruleContext.TypeChecker == nil {
		return nil
	}
	valueType := ruleContext.TypeChecker.GetTypeAtLocation(name)
	if valueType == nil {
		return nil
	}
	return ruleContext.TypeChecker.GetSignaturesOfType(valueType, checker.SignatureKindCall)
}

// returnsOnlyReactNodes reports a function whose every return is, by the checker, a React element, a React
// node union holding one, or `null`, awaited first, so an async server component counts. That is a
// component whatever its body looks like: a Next.js page's default export that returns `null`, or a stub
// written before its markup. A function that never returns, `never`, has no return that is anything else,
// which is a page whose whole body is `redirect(...)` or `notFound()`.
func returnsOnlyReactNodes(ruleContext rule.Context, name *ast.Node) bool {
	signatures := callSignaturesOf(ruleContext, name)
	if len(signatures) == 0 {
		return false
	}
	for _, signature := range signatures {
		returnType := ruleContext.TypeChecker.GetReturnTypeOfSignature(signature)
		if awaited := checker.Checker_getAwaitedType(ruleContext.TypeChecker, returnType); awaited != nil {
			returnType = awaited
		}
		if returnType == nil {
			return false
		}
		if mayReturnAnElement(ruleContext.TypeChecker, returnType) ||
			type_checking.IsTypeFlagSet(returnType, checker.TypeFlagsNever) {
			continue
		}
		for part := range type_checking.UnionTypePartsSequence(returnType) {
			if !type_checking.IsTypeFlagSet(part, checker.TypeFlagsNull|checker.TypeFlagsUndefined) {
				return false
			}
		}
	}
	return true
}

// neverReturns reports a function whose body has no `return` of its own and whose end the checker finds
// unreachable, so no return of it is anything at all: a Next.js page whose whole body is `redirect(...)` or
// `notFound()`. The checker infers `void` there rather than `never`, so the return type cannot say it.
func neverReturns(ruleContext rule.Context, function *ast.Node) bool {
	body := function.Body()
	if ruleContext.TypeChecker == nil || body == nil || body.Kind != ast.KindBlock {
		return false
	}
	returns := false
	var visit func(node *ast.Node) bool
	visit = func(node *ast.Node) bool {
		if returns || ast.IsFunctionLike(node) || ast.IsClassLike(node) {
			return returns
		}
		if node.Kind == ast.KindReturnStatement {
			returns = true
			return true
		}
		node.ForEachChild(visit)
		return returns
	}
	body.ForEachChild(visit)
	return !returns && !checker.Checker_functionHasImplicitReturn(ruleContext.TypeChecker, function)
}

// returnsADecorator reports a decorator factory: a function whose return type the checker reads as a
// decorator in some signature of it, a class decorator taking a constructor, a member or parameter
// decorator taking a property key, or a standard decorator taking a decorator context. A union or an
// intersection of decorators, `ClassDecorator & MethodDecorator`, is one when any of its signatures is.
func returnsADecorator(ruleContext rule.Context, name *ast.Node) bool {
	for _, factory := range callSignaturesOf(ruleContext, name) {
		returnType := ruleContext.TypeChecker.GetReturnTypeOfSignature(factory)
		if returnType == nil {
			continue
		}
		for part := range type_checking.UnionTypePartsSequence(returnType) {
			for _, signature := range ruleContext.TypeChecker.GetSignaturesOfType(part, checker.SignatureKindCall) {
				if isDecoratorSignature(ruleContext, signature) {
					return true
				}
			}
		}
	}
	return false
}

// isDecoratorSignature reports the shapes a decorator is called with: `(target)` where the target is a
// constructor or a function, `(target, propertyKey, ...)` where the key may be a symbol, which only a
// member's or a parameter's key is, and `(value, context)` where the context carries a `kind` and an
// `addInitializer`, as every standard decorator context does.
func isDecoratorSignature(ruleContext rule.Context, signature *checker.Signature) bool {
	parameters := signature.Parameters()
	if len(parameters) == 0 {
		return false
	}
	typeOf := func(parameter *ast.Symbol) *checker.Type {
		parameterType := ruleContext.TypeChecker.GetTypeOfSymbol(parameter)
		if parameterType != nil && type_checking.IsTypeFlagSet(parameterType, checker.TypeFlagsTypeParameter) {
			if constraint := checker.Checker_getBaseConstraintOfType(
				ruleContext.TypeChecker,
				parameterType,
			); constraint != nil {
				return constraint
			}
		}
		return parameterType
	}
	if len(parameters) == 1 {
		target := typeOf(parameters[0])
		return target != nil &&
			(len(ruleContext.TypeChecker.GetSignaturesOfType(target, checker.SignatureKindConstruct)) > 0 ||
				ruleContext.TypeChecker.GetPropertyOfType(target, "prototype") != nil)
	}
	key := typeOf(parameters[1])
	if key == nil {
		return false
	}
	for part := range type_checking.UnionTypePartsSequence(key) {
		if type_checking.IsTypeFlagSet(part, checker.TypeFlagsESSymbol) {
			return true
		}
	}
	return len(parameters) == 2 && ruleContext.TypeChecker.GetPropertyOfType(key, "kind") != nil &&
		ruleContext.TypeChecker.GetPropertyOfType(key, "addInitializer") != nil
}

// isConstructedInTheFile reports a function this file calls with `new`, matched by the checker's symbol
// where there is one and by spelling where there is not.
func isConstructedInTheFile(ruleContext rule.Context, name *ast.Node) bool {
	var declared *ast.Symbol
	if ruleContext.TypeChecker != nil {
		declared = ruleContext.TypeChecker.GetSymbolAtLocation(name)
	}
	found := false
	var visit func(node *ast.Node) bool
	visit = func(node *ast.Node) bool {
		if found {
			return true
		}
		if node.Kind == ast.KindNewExpression {
			callee := unwrapAssertions(node.AsNewExpression().Expression)
			if callee != nil && callee.Kind == ast.KindIdentifier && callee.Text() == name.Text() &&
				(declared == nil || ruleContext.TypeChecker.GetSymbolAtLocation(callee) == declared) {
				found = true
				return true
			}
		}
		node.ForEachChild(visit)
		return found
	}
	ruleContext.SourceFile.AsNode().ForEachChild(visit)
	return found
}

// assignsPropertiesOfThis reports an old-style constructor, a function whose own body, an arrow inside it
// included, assigns a property of `this`, through a cast or not. A nested function or class has a `this`
// of its own, so its assignments are not counted.
func assignsPropertiesOfThis(function *ast.Node) bool {
	body := function.Body()
	if body == nil {
		return false
	}
	found := false
	var visit func(node *ast.Node) bool
	visit = func(node *ast.Node) bool {
		if found {
			return true
		}
		switch node.Kind {
		case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindClassDeclaration,
			ast.KindClassExpression, ast.KindMethodDeclaration:
			return false
		case ast.KindBinaryExpression:
			binary := node.AsBinaryExpression()
			if binary.OperatorToken != nil && binary.OperatorToken.Kind == ast.KindEqualsToken {
				target := unwrapAssertions(binary.Left)
				if target != nil && target.Kind == ast.KindPropertyAccessExpression {
					object := unwrapAssertions(target.AsPropertyAccessExpression().Expression)
					if object != nil && object.Kind == ast.KindThisKeyword {
						found = true
						return true
					}
				}
			}
		}
		node.ForEachChild(visit)
		return found
	}
	body.ForEachChild(visit)
	return found
}

// mirrorsExternalSpelling reports a value read from an external grammar into a name that spells it
// exactly: an environment key off `process.env`, or a property a dependency declares. One `??` or `||`
// fallback is looked through, as consistency-no-screaming-snake-case does. A recased read mirrors
// nothing, since a mirror keeps the external system's own case.
func mirrorsExternalSpelling(ruleContext rule.Context, name string, initializer *ast.Node) bool {
	current := unwrapAssertions(initializer)
	if current == nil {
		return false
	}
	if current.Kind == ast.KindBinaryExpression {
		binary := current.AsBinaryExpression()
		if binary.OperatorToken != nil && (binary.OperatorToken.Kind == ast.KindQuestionQuestionToken ||
			binary.OperatorToken.Kind == ast.KindBarBarToken) {
			current = unwrapAssertions(binary.Left)
		}
	}
	if current == nil || current.Kind != ast.KindPropertyAccessExpression {
		return false
	}
	property := current.AsPropertyAccessExpression().Name()
	if property == nil || property.Text() != name {
		return false
	}
	if isProcessEnvironmentAccess(current) {
		return true
	}
	return ruleContext.TypeChecker != nil &&
		rule.IsDeclaredByADependency(ruleContext.TypeChecker.GetSymbolAtLocation(property))
}

// startsWithUppercase reports a name whose first character past any leading underscore is a capital, so
// `_Icon` holding a component keeps its capital and is told `Icon`, never `icon`, an HTML tag.
func startsWithUppercase(name string) bool {
	for _, character := range strings.TrimLeft(name, "_") {
		return unicode.IsUpper(character)
	}
	return false
}

// opensOnTwoCapitals reports a name whose first two characters past any leading underscore are capitals:
// an acronym the converter lowers only the first letter of.
func opensOnTwoCapitals(name string) bool {
	runes := []rune(strings.TrimLeft(name, "_"))
	return len(runes) > 1 && unicode.IsUpper(runes[0]) && unicode.IsUpper(runes[1])
}
