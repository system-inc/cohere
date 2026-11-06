package react

import (
	"fmt"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	reactUtilities "github.com/system-inc/cohere/internal/lint/ecmascript/react"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// noTyposState carries the two import bindings while the file is walked.
type noTyposState struct {
	ctx rule.Context

	// propertyTypesPackageName is the local name `prop-types` was imported under, or the name a
	// `PropTypes` specifier from `react` was bound to.
	propertyTypesPackageName string

	// reactPackageName is the local name `react` was imported under.
	reactPackageName string
}

// walk visits the file in source order, updating the bindings and checking each node.
//
// Source order matters and is not incidental: upstream's `ImportDeclaration` listener sets the
// binding as ESLint reaches it, so a `propTypes` object written above the import sees no binding and
// its prop-type arms stay silent. Pinned by fixture.
func (state *noTyposState) walk(node *ast.Node) {
	if node == nil {
		return
	}

	switch node.Kind {
	case ast.KindImportDeclaration:
		state.recordImport(node)

	case ast.KindPropertyDeclaration:
		state.checkStaticClassProperty(node)

	case ast.KindMethodDeclaration:
		state.checkClassMethod(node)

	case ast.KindPropertyAccessExpression:
		state.checkMemberAssignment(node)

	case ast.KindObjectLiteralExpression:
		state.checkEs5Specification(node)
	}

	node.ForEachChild(func(child *ast.Node) bool {
		state.walk(child)
		return false
	})
}

// recordImport updates the package bindings from one import declaration.
//
// Upstream reads `node.specifiers[0].local.name` for both packages, which is the first specifier
// whatever its kind, and then separately looks for a named `PropTypes` specifier in a react import.
// A side-effect import with no specifiers is the reporting case for both packages.
func (state *noTyposState) recordImport(node *ast.Node) {
	declaration := node.AsImportDeclaration()
	moduleSpecifier := declaration.ModuleSpecifier
	if moduleSpecifier == nil || moduleSpecifier.Kind != ast.KindStringLiteral {
		return
	}

	specifiers := noTyposImportSpecifiers(node)

	switch moduleSpecifier.Text() {
	case "prop-types":
		if len(specifiers) == 0 {
			state.ctx.ReportNode(node, rule.Message{
				Id: "noPropTypesBinding",
				Description: "This imports `prop-types` for its side effects and never names it, " +
					"so nothing in the file can reference a prop type. The package has no side " +
					"effects worth importing for; write `import PropTypes from 'prop-types'`.",
			})
			return
		}
		state.propertyTypesPackageName = specifiers[0].localName

	case "react":
		if len(specifiers) == 0 {
			state.ctx.ReportNode(node, rule.Message{
				Id: "noReactBinding",
				Description: "This imports `react` for its side effects and never names it, so " +
					"nothing in the file can reference React. Write " +
					"`import React from 'react'`, or drop the import if it is genuinely unused.",
			})
			return
		}
		state.reactPackageName = specifiers[0].localName
		// A `PropTypes` named specifier from react binds the prop-types package too, which is the
		// pre-15.5 spelling `import { PropTypes } from 'react'`.
		for _, specifier := range specifiers {
			if specifier.importedName == "PropTypes" {
				state.propertyTypesPackageName = specifier.localName
			}
		}
	}
}

// checkStaticClassProperty answers upstream's `ClassProperty, PropertyDefinition` listener.
//
// Static only, and only on a class the shelf calls a component.
func (state *noTyposState) checkStaticClassProperty(node *ast.Node) {
	if !noTyposIsStatic(node) {
		return
	}
	class := noTyposEnclosingClass(node)
	if class == nil || !reactUtilities.IsEs6ComponentClass(class) {
		return
	}
	state.reportCasingTypo(node.AsPropertyDeclaration().Initializer, node.Name(), true)
}

// checkClassMethod answers upstream's `MethodDefinition` listener.
func (state *noTyposState) checkClassMethod(node *ast.Node) {
	class := noTyposEnclosingClass(node)
	if class == nil || !reactUtilities.IsEs6ComponentClass(class) {
		return
	}
	state.reportLifecycleCasingTypo(node, node.Name(), noTyposIsStatic(node))
}

// checkMemberAssignment answers upstream's `MemberExpression` listener.
//
// This is the widest arm and the only one that accepts a non-class. It fires on `Foo.PropTypes = {}`
// where `Foo` is a class component or a function whose body returns JSX.
func (state *noTyposState) checkMemberAssignment(node *ast.Node) {
	access := node.AsPropertyAccessExpression()
	property := access.Name()
	if property == nil || property.Kind != ast.KindIdentifier {
		return
	}
	if !noTyposMatchesStaticClassPropertyIgnoringCase(property.Text()) {
		return
	}

	// Upstream tests `node.parent.type === 'AssignmentExpression' && node.parent.right`, and that is
	// ALL it tests. There is no left-side check and no operator check, which reads as an oversight
	// and is what the rule does. Measured on the installed build, and this port had both extra
	// checks at first with a surviving mutant for each:
	//
	//	Foo.PropTypes = {}        reports, the ordinary case
	//	target = Foo.PropTypes    REPORTS, the member is on the RIGHT
	//	Foo.PropTypes ||= {}      REPORTS, a compound operator
	//	Foo.PropTypes             silent, no assignment at all
	//	doThing(Foo.PropTypes)    silent, likewise
	//	if (Foo.PropTypes === 1)  silent, a comparison is not an assignment
	//
	// The second row is the surprising one: reading a mis-cased property reports as if it had been
	// written. Reproduced rather than narrowed, because narrowing would silence inputs upstream
	// reports and the two mutants prove no fixture would have noticed.
	//
	// The `Right` value passed on is upstream's `node.parent.right` whichever side the member sits
	// on, so for the right-hand row it is the member itself, and `checkPropObject` declines it for
	// not being an object literal.
	//
	// The nil test on `Right` is equivalent rather than discriminating, measured rather than argued
	// after a mutant dropping it survived: our parser's error recovery synthesizes a missing
	// identifier rather than leaving the slot empty, probed over five shapes including
	// `Foo.PropTypes =`, `Foo.PropTypes = ;` and `Foo.PropTypes ||=`, all of which came back with a
	// KindIdentifier. It is kept because upstream tests it, because it costs nothing, and because
	// what it guards is a nil dereference one call later, which the walk would turn into a lost file
	// rather than a lost finding.
	parent := node.Parent
	if parent == nil || parent.Kind != ast.KindBinaryExpression {
		return
	}
	binary := parent.AsBinaryExpression()
	if !ast.IsAssignmentOperator(binary.OperatorToken.Kind) || binary.Right == nil {
		return
	}

	target := access.Expression
	if target == nil || target.Kind != ast.KindIdentifier {
		return
	}
	if !state.namesAComponent(node, target.Text()) {
		return
	}

	state.reportCasingTypo(binary.Right, property, true)
}

// namesAComponent reports whether an identifier names something this rule treats as a component.
//
// Upstream calls `utils.getRelatedComponent`, which resolves the name through its registry, and then
// accepts it when it is an ES6 component class or a non-class whose body returns JSX. Resolution is
// done here by searching the file for a declaration of that name, which is what the registry is
// doing for this shape: the assignment and the declaration are both in the file by construction,
// since a name declared elsewhere has no `Foo.propTypes = ...` here to check.
//
// The JSX test is upstream's `isReturningJSX` and it is narrower than it sounds. Measured against
// the installed build:
//
//	function Foo() { return <div/>; }         counts
//	const Foo = () => <div/>;                 counts, an implicit return
//	function Foo() { return null; }           does NOT count
//	function Foo() { return 'x'; }            does NOT count
//	function Foo() { }                        does NOT count
//	function foo() { return <div/>; }         counts, there is no name gate
//	function Foo() { items.map(() => <div/>); return null; }   does NOT count
//
// The last row is the one a port is most likely to get wrong: JSX inside a nested function belongs
// to that function, not to this one.
func (state *noTyposState) namesAComponent(from *ast.Node, name string) bool {
	declaration := noTyposFindDeclaration(from, name)
	if declaration == nil {
		return false
	}
	if declaration.Kind == ast.KindClassDeclaration {
		return reactUtilities.IsEs6ComponentClass(declaration)
	}
	return noTyposReturnsJsx(declaration)
}

// checkEs5Specification answers upstream's `ObjectExpression` listener.
//
// Every property of a `createReactClass` object is checked for both a casing typo and a lifecycle
// typo, and a spread contributes neither.
func (state *noTyposState) checkEs5Specification(node *ast.Node) {
	if !noTyposIsCreateReactClassSpecification(node) {
		return
	}
	properties := node.AsObjectLiteralExpression().Properties
	if properties == nil {
		return
	}
	for _, property := range properties.Nodes {
		// Upstream filters spreads out here. The skip is subsumed by `noTyposKeyName`, which
		// declines any key that is not an identifier, a string or a number, and a spread has no key
		// node at all: a mutant removing this line survived the whole suite, and so did one removing
		// it while also teaching the key reader to answer for a spread, because the name it answers
		// matches no canonical name either way. Kept because upstream writes it and because it says
		// what the loop is doing; the equivalence rests on a second function's behavior rather than
		// on anything visible here.
		if property.Kind == ast.KindSpreadAssignment {
			continue
		}
		state.reportCasingTypo(noTyposPropertyValue(property), property.Name(), false)
		state.reportLifecycleCasingTypo(property, property.Name(), false)
	}
}

// reportCasingTypo answers upstream's `reportErrorIfPropertyCasingTypo`.
//
// Two things happen here and they are independent. A property named exactly `propTypes`,
// `contextTypes` or `childContextTypes` has its object value walked for prop-type typos. And any
// property whose lowercased name matches one of the four static names while differing in case is
// itself a casing typo.
func (state *noTyposState) reportCasingTypo(value *ast.Node, key *ast.Node, isClassProperty bool) {
	name, ok := noTyposKeyName(key)
	if !ok {
		return
	}

	if name == "propTypes" || name == "contextTypes" || name == "childContextTypes" {
		state.checkPropObject(value)
	}

	for _, canonical := range noTyposStaticClassProperties {
		if strings.EqualFold(canonical, name) && canonical != name {
			if isClassProperty {
				state.ctx.ReportNode(key, rule.Message{
					Id: "typoStaticClassProp",
					Description: "This static property is spelled with the wrong casing, so React " +
						"never reads it. React matches these names exactly, which means the " +
						"declaration sits there looking like configuration and does nothing.",
				})
				return
			}
			state.ctx.ReportNode(key, rule.Message{
				Id: "typoPropDeclaration",
				Description: "This property is spelled with the wrong casing, so React never " +
					"reads it. React matches these names exactly, which means the declaration " +
					"sits there looking like configuration and does nothing.",
			})
			return
		}
	}
}

// reportLifecycleCasingTypo answers upstream's `reportErrorIfLifecycleMethodCasingTypo`.
//
// Two independent judgments again. A name matching the one static lifecycle method case-insensitively
// and written without `static` reports `staticLifecycleMethod`. And a name matching any lifecycle
// method case-insensitively while differing in case reports `typoLifecycleMethod`. A name can trip
// both, which upstream reports as two findings in that order.
func (state *noTyposState) reportLifecycleCasingTypo(node *ast.Node, key *ast.Node, isStatic bool) {
	name, ok := noTyposKeyName(key)
	if !ok {
		return
	}

	for _, method := range noTyposStaticLifecycleMethods {
		if !isStatic && strings.EqualFold(name, method) {
			state.ctx.ReportNode(node, rule.Message{
				Id: "staticLifecycleMethod",
				Description: fmt.Sprintf(
					"`%s` is a static lifecycle method and this one is not declared `static`, so "+
						"React never calls it. Add `static`.", name),
			})
		}
	}

	for _, method := range append(append([]string{}, noTyposInstanceLifecycleMethods...),
		noTyposStaticLifecycleMethods...) {
		if strings.EqualFold(method, name) && method != name {
			state.ctx.ReportNode(node, rule.Message{
				Id: "typoLifecycleMethod",
				Description: fmt.Sprintf(
					"`%s` is spelled with the wrong casing and React calls `%s`, so this method "+
						"never runs. React matches lifecycle names exactly.", name, method),
			})
		}
	}
}

// checkPropObject walks a propTypes object looking for prop-type typos.
func (state *noTyposState) checkPropObject(node *ast.Node) {
	if node == nil || node.Kind != ast.KindObjectLiteralExpression {
		return
	}
	properties := node.AsObjectLiteralExpression().Properties
	if properties == nil {
		return
	}
	for _, property := range properties.Nodes {
		state.checkProperty(noTyposPropertyValue(property))
	}
}

// checkProperty answers upstream's `checkValidProp`, including its recursion through shape and oneOfType.
//
// The leading guard is upstream's and it is load-bearing: with neither package name known, every
// prop-type arm is silent, so a file that imports nothing produces no typo findings however
// misspelled it is.
func (state *noTyposState) checkProperty(node *ast.Node) {
	if state.propertyTypesPackageName == "" && state.reactPackageName == "" {
		return
	}
	if node == nil {
		return
	}

	if node.Kind == ast.KindPropertyAccessExpression {
		access := node.AsPropertyAccessExpression()
		object := access.Expression
		property := access.Name()
		if object == nil || property == nil {
			return
		}

		if object.Kind == ast.KindPropertyAccessExpression {
			inner := object.AsPropertyAccessExpression()
			if state.isPropTypesPackage(inner.Expression) {
				// `PropTypes.myProp.isRequired`
				state.checkPropTypeName(inner.Name())
				state.checkPropTypeQualifier(property)
				return
			}
		}

		if state.isPropTypesPackage(object) {
			// `PropTypes.myProp`, unless the property is itself the qualifier.
			if property.Kind == ast.KindIdentifier && property.Text() == "isRequired" {
				return
			}
			state.checkPropTypeName(property)
			return
		}

		if object.Kind == ast.KindCallExpression {
			state.checkPropTypeQualifier(property)
			state.checkPropCall(object)
		}
		return
	}

	if node.Kind == ast.KindCallExpression {
		state.checkPropCall(node)
	}
}

// checkPropCall answers upstream's `checkValidCallExpression`.
//
// `shape(...)` recurses into the object literal, `oneOfType([...])` recurses into each element, and
// every other call is left alone.
func (state *noTyposState) checkPropCall(node *ast.Node) {
	call := node.AsCallExpression()
	callee := call.Expression
	if callee == nil || callee.Kind != ast.KindPropertyAccessExpression {
		return
	}
	name := callee.AsPropertyAccessExpression().Name()
	if name == nil || name.Kind != ast.KindIdentifier {
		return
	}
	if call.Arguments == nil || len(call.Arguments.Nodes) == 0 {
		return
	}
	first := call.Arguments.Nodes[0]

	switch name.Text() {
	case "shape":
		state.checkPropObject(first)

	case "oneOfType":
		if first.Kind != ast.KindArrayLiteralExpression {
			return
		}
		elements := first.AsArrayLiteralExpression().Elements
		if elements == nil {
			return
		}
		for _, element := range elements.Nodes {
			state.checkProperty(element)
		}
	}
}

// isPropTypesPackage reports whether an expression names the prop-types package.
//
// Either the bare local binding, or `<react binding>.PropTypes`, which is the pre-15.5 spelling.
func (state *noTyposState) isPropTypesPackage(node *ast.Node) bool {
	if node == nil {
		return false
	}
	if node.Kind == ast.KindIdentifier {
		return state.propertyTypesPackageName != "" && node.Text() == state.propertyTypesPackageName
	}
	if node.Kind == ast.KindPropertyAccessExpression {
		access := node.AsPropertyAccessExpression()
		name := access.Name()
		object := access.Expression
		return name != nil && name.Kind == ast.KindIdentifier && name.Text() == "PropTypes" &&
			object != nil && object.Kind == ast.KindIdentifier &&
			state.reactPackageName != "" && object.Text() == state.reactPackageName
	}
	return false
}

// checkPropTypeName reports a prop type the package does not export.
func (state *noTyposState) checkPropTypeName(node *ast.Node) {
	if node == nil || node.Kind != ast.KindIdentifier {
		return
	}
	name := node.Text()
	if name == "" {
		return
	}
	for _, known := range noTyposPropTypeNames {
		if known == name {
			return
		}
	}
	state.ctx.ReportNode(node, rule.Message{
		Id: "typoPropType",
		Description: fmt.Sprintf(
			"`%s` is not a prop type the `prop-types` package exports, so this validator is "+
				"`undefined` and React validates nothing for the prop.", name),
	})
}

// checkPropTypeQualifier reports a chain qualifier that is not `isRequired`.
func (state *noTyposState) checkPropTypeQualifier(node *ast.Node) {
	if node == nil || node.Kind != ast.KindIdentifier {
		return
	}
	if node.Text() == "isRequired" {
		return
	}
	state.ctx.ReportNode(node, rule.Message{
		Id: "typoPropTypeChain",
		Description: fmt.Sprintf(
			"`isRequired` is the only qualifier a prop type chain accepts, and `%s` is not it, so "+
				"this reads as `undefined` and validates nothing.", node.Text()),
	})
}
