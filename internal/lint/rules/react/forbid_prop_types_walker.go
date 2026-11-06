package react

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// forbidPropTypesWalker carries the per-file import state the rule's judgment depends on.
type forbidPropTypesWalker struct {
	ruleContext rule.Context
	settings    ForbidPropTypesOptions

	// propertyTypesPackageName is the local name bound to the prop-types package, when one is imported.
	propertyTypesPackageName string

	// reactPackageName is the local name bound to react, when it is imported.
	reactPackageName string

	// isForeignPropTypesPackage records that something named `PropTypes` was imported from a module
	// that is neither `react` nor `prop-types`. Upstream uses this to stop treating every
	// `PropTypes.x` in the file as the real one.
	isForeignPropTypesPackage bool
}

// collectImports runs before any judgment, because an import may follow the usage it governs.
//
// ESLint visits the whole program and the import listener happens to fire first for a top-level
// import, but nothing guarantees that ordering for the shapes this rule meets, and our walk is a
// single pre-order pass. Collecting first makes the dependence explicit rather than incidental.
func (walker *forbidPropTypesWalker) collectImports(sourceFile *ast.Node) {
	sourceFile.ForEachChild(func(child *ast.Node) bool {
		if child.Kind != ast.KindImportDeclaration {
			return false
		}
		declaration := child.AsImportDeclaration()
		moduleSpecifier := declaration.ModuleSpecifier
		if moduleSpecifier == nil || moduleSpecifier.Kind != ast.KindStringLiteral {
			return false
		}

		switch moduleSpecifier.Text() {
		case "prop-types":
			if name := forbidPropTypesFirstLocalName(child); name != "" {
				walker.propertyTypesPackageName = name
			}

		case "react":
			if name := forbidPropTypesFirstLocalName(child); name != "" {
				walker.reactPackageName = name
			}
			// `import { PropTypes } from 'react'` binds the prop types package under whatever
			// local name that specifier uses.
			if local := forbidPropTypesLocalNameForImported(child, "PropTypes"); local != "" {
				walker.propertyTypesPackageName = local
			}

		default:
			// Anything named `PropTypes` coming from a third module means the bare name in this
			// file is somebody else's. Measured: it makes an otherwise reporting file clean.
			if forbidPropTypesBindsLocalName(child, "PropTypes") {
				walker.isForeignPropTypesPackage = true
			}
		}
		return false
	})
}

// walk visits every node, dispatching the shapes that can carry a prop types object.
func (walker *forbidPropTypesWalker) walk(node *ast.Node) {
	if node == nil {
		return
	}

	switch node.Kind {
	case ast.KindPropertyDeclaration:
		walker.checkClassProperty(node)

	case ast.KindBinaryExpression:
		walker.checkMemberAssignment(node)

	case ast.KindObjectLiteralExpression:
		walker.checkObjectLiteral(node)

	case ast.KindGetAccessor:
		walker.checkGetter(node)

	case ast.KindCallExpression:
		walker.checkShapeCall(node)
	}

	node.ForEachChild(func(child *ast.Node) bool {
		walker.walk(child)
		return false
	})
}

// declarationNameKind says which of the three declaration names a node carries, or "".
func (walker *forbidPropTypesWalker) declarationNameKind(name string) bool {
	switch name {
	case "propTypes":
		return true
	case "contextTypes":
		return walker.settings.CheckContextTypes
	case "childContextTypes":
		return walker.settings.CheckChildContextTypes
	}
	return false
}

// checkClassProperty handles `class C { static propTypes = {...} }`.
//
// Upstream does not require the property to be static and neither does this: `propsUtil` tests the
// key's name alone.
func (walker *forbidPropTypesWalker) checkClassProperty(node *ast.Node) {
	property := node.AsPropertyDeclaration()
	name := property.Name()
	if name == nil || name.Kind != ast.KindIdentifier ||
		!walker.declarationNameKind(name.Text()) {
		return
	}
	walker.checkNode(property.Initializer)
}

// checkMemberAssignment handles `C.propTypes = {...}`.
func (walker *forbidPropTypesWalker) checkMemberAssignment(node *ast.Node) {
	assignment := node.AsBinaryExpression()
	if !ast.IsAssignmentOperator(assignment.OperatorToken.Kind) {
		// Every assignment operator qualifies, not only `=`, and this line was wrong before it was
		// measured. Upstream anchors on the member expression and reads `node.parent.right`, which
		// any assignment carries, so `C.propTypes ||= {...}` and `+=` both report. Verified on the
		// installed build across four operators, all reporting.
		//
		// The first draft narrowed this to `KindEqualsToken` on the reasoning that a compound
		// operator has no meaning for a prop types object. That reasoning is about what the code
		// ought to mean rather than what upstream decides, and it is exactly the "documenting a
		// limit you did not measure" failure: it survived every one of the 97 imported cases,
		// because the corpus writes only plain assignment.
		return
	}
	target := assignment.Left
	if target == nil || target.Kind != ast.KindPropertyAccessExpression {
		return
	}
	name := target.AsPropertyAccessExpression().Name()
	if name == nil || name.Kind != ast.KindIdentifier ||
		!walker.declarationNameKind(name.Text()) {
		return
	}
	walker.checkNode(assignment.Right)
}

// checkObjectLiteral handles `createReactClass({ propTypes: {...} })`.
//
// Upstream visits every object expression and looks for a property with one of the three names, so
// this is not restricted to a factory call and an ordinary object literal carrying a `propTypes`
// key is checked too. Measured.
func (walker *forbidPropTypesWalker) checkObjectLiteral(node *ast.Node) {
	properties := node.AsObjectLiteralExpression().Properties
	if properties == nil {
		return
	}
	for _, property := range properties.Nodes {
		if property.Kind != ast.KindPropertyAssignment {
			continue
		}
		assignment := property.AsPropertyAssignment()
		name := assignment.Name()
		if name == nil || name.Kind != ast.KindIdentifier ||
			!walker.declarationNameKind(name.Text()) {
			continue
		}
		// Upstream checks only for an object expression here, not the full `checkNode` surface, so
		// a factory whose `propTypes` is an identifier is silent while the same identifier through
		// a member assignment reports. That asymmetry is real and measured, and
		// TestForbidPropTypesObjectLiteralArmDoesNotFollowIdentifiers pins it.
		//
		// The kind test on this line is nonetheless equivalent rather than load-bearing, and a
		// mutation widening it survives correctly. `checkProperties` opens with the same test and
		// returns on anything that is not an object literal, so no input can distinguish the two
		// versions. What produces the asymmetry is this arm calling `checkProperties` directly
		// rather than calling `checkNode`, which is a different line.
		//
		// Kept because it states the arm's own contract at the point of the call rather than
		// leaving a reader to follow the callee, and because widening it would read as an
		// invitation to pass anything. All four callers of `checkProperties` were enumerated when
		// this verdict was taken; a caller that skipped that entry guard would void it.
		value := assignment.Initializer
		if value != nil && value.Kind == ast.KindObjectLiteralExpression {
			walker.checkProperties(value)
		}
	}
}

// checkGetter handles `static get propTypes() { return {...} }`.
func (walker *forbidPropTypesWalker) checkGetter(node *ast.Node) {
	accessor := node.AsGetAccessorDeclaration()
	name := accessor.Name()
	if name == nil || name.Kind != ast.KindIdentifier ||
		!walker.declarationNameKind(name.Text()) {
		return
	}
	body := accessor.Body
	if body == nil {
		return
	}
	// Upstream's `findReturnStatement` takes the last return statement in the body's statement
	// list, at the top level only.
	statements := body.AsBlock().Statements
	if statements == nil {
		return
	}
	for index := len(statements.Nodes) - 1; index >= 0; index-- {
		statement := statements.Nodes[index]
		if statement.Kind != ast.KindReturnStatement {
			continue
		}
		walker.checkNode(statement.AsReturnStatement().Expression)
		return
	}
}

// checkShapeCall handles a `shape({...})` call anywhere, which upstream visits independently of the
// declaration it sits in.
//
// That independence is real and measured: a `shape` call reached through a nested prop type is
// checked by this arm rather than by the recursion, which is why a shape's own properties report
// even though `checkProperties` does not recurse into them.
func (walker *forbidPropTypesWalker) checkShapeCall(node *ast.Node) {
	call := node.AsCallExpression()
	callee := call.Expression
	if callee == nil {
		return
	}

	// The receiver has to look like the prop types package, and this guard is the whole reason
	// `Yup.object().shape({...})` is clean. Upstream declines a member callee whose object fails
	// `isPropTypesPackage` and is not itself a prop types declaration.
	//
	// `isPropTypesPackage` accepts an identifier or a member expression and nothing else, so a call
	// receiver such as `Yup.object()` falls through to false whatever is imported. That is what
	// separates a validation schema from a prop type, and it is not the foreign-import flag:
	// measured with and without the `yup` import, both clean, while `X.propTypes = { a:
	// PropTypes.shape({...}) }` reports either way.
	//
	// Three of upstream's passing cases are exactly this shape and all three reported before this
	// guard was written. They are the reason step 3 exists: nothing about the rule's description
	// suggests a validation library is the false positive to worry about.
	if callee.Kind == ast.KindPropertyAccessExpression {
		receiver := callee.AsPropertyAccessExpression().Expression
		if !walker.isPropTypesPackage(receiver) && !walker.isPropTypesDeclarationAccess(callee) {
			return
		}
	}

	calleeName := ""
	switch callee.Kind {
	case ast.KindIdentifier:
		calleeName = callee.Text()
	case ast.KindPropertyAccessExpression:
		if name := callee.AsPropertyAccessExpression().Name(); name != nil &&
			name.Kind == ast.KindIdentifier {
			calleeName = name.Text()
		}
	}
	if calleeName != "shape" {
		return
	}

	if call.Arguments == nil || len(call.Arguments.Nodes) == 0 {
		return
	}
	argument := call.Arguments.Nodes[0]
	if argument != nil && argument.Kind == ast.KindObjectLiteralExpression {
		walker.checkProperties(argument)
	}
}

// checkNode resolves the several things a prop types declaration's value can be.
//
// Upstream's `checkNode` has a fourth branch for a prop wrapper call, which is not reproduced. See
// the rule doc: which functions count comes from a shared setting `rule.Context` does not expose.
func (walker *forbidPropTypesWalker) checkNode(node *ast.Node) {
	if node == nil {
		return
	}
	switch node.Kind {
	case ast.KindObjectLiteralExpression:
		walker.checkProperties(node)

	case ast.KindIdentifier:
		walker.checkProperties(walker.resolveToObjectLiteral(node))
	}
}

// resolveToObjectLiteral follows an identifier to the object literal it was initialized with.
//
// Upstream asks its own variable index; the checker answers the same question. Only a variable
// declaration with an object literal initializer qualifies, which is upstream's
// `propTypesObject && propTypesObject.properties` test.
func (walker *forbidPropTypesWalker) resolveToObjectLiteral(identifier *ast.Node) *ast.Node {
	if walker.ruleContext.TypeChecker == nil {
		return nil
	}
	symbol := walker.ruleContext.TypeChecker.GetSymbolAtLocation(identifier)
	if symbol == nil {
		return nil
	}
	for _, declaration := range symbol.Declarations {
		if declaration == nil || declaration.Kind != ast.KindVariableDeclaration {
			continue
		}
		initializer := declaration.AsVariableDeclaration().Initializer
		if initializer != nil && initializer.Kind == ast.KindObjectLiteralExpression {
			return initializer
		}
	}
	return nil
}

// checkProperties reads each property of a prop types object and reports the forbidden ones.
//
// It deliberately does not recurse into a nested object literal. A nested `shape({...})` is reached
// by the shape arm instead, which visits every such call in the file independently.
func (walker *forbidPropTypesWalker) checkProperties(object *ast.Node) {
	if object == nil || object.Kind != ast.KindObjectLiteralExpression {
		return
	}
	properties := object.AsObjectLiteralExpression().Properties
	if properties == nil {
		return
	}

	for _, property := range properties.Nodes {
		if property.Kind != ast.KindPropertyAssignment {
			// A spread carries no single type name, which is upstream declining anything that is
			// not a `Property`.
			continue
		}
		walker.checkPropertyValue(property, property.AsPropertyAssignment().Initializer)
	}
}

// checkPropertyValue unwraps one property's value and reports if the resulting name is forbidden.
//
// The finding is anchored on the property rather than on the value, which is upstream passing the
// declaration node to `report`.
func (walker *forbidPropTypesWalker) checkPropertyValue(property *ast.Node, value *ast.Node) {
	if value == nil {
		return
	}
	peeled := value

	// `PropTypes.any.isRequired` peels to `PropTypes.any`.
	if peeled.Kind == ast.KindPropertyAccessExpression {
		access := peeled.AsPropertyAccessExpression()
		if name := access.Name(); name != nil && name.Kind == ast.KindIdentifier &&
			name.Text() == "isRequired" {
			peeled = access.Expression
			if peeled == nil {
				return
			}
		}
	}

	// `PropTypes.arrayOf(PropTypes.object)` reports on the argument's name, then continues with the
	// callee so the wrapper's own name is considered too.
	if peeled.Kind == ast.KindCallExpression {
		call := peeled.AsCallExpression()
		if !walker.isPropTypesPackage(call.Expression) {
			return
		}
		if call.Arguments != nil {
			for _, argument := range call.Arguments.Nodes {
				walker.reportIfForbidden(property, forbidPropTypesTypeName(argument))
			}
		}
		peeled = call.Expression
		if peeled == nil {
			return
		}
	}

	if !walker.isPropTypesPackage(peeled) {
		return
	}
	walker.reportIfForbidden(property, forbidPropTypesTypeName(peeled))
}

// isPropTypesPackage reports whether a value is reached through something this file treats as the
// prop types package.
//
// Upstream's test is written as a disjunction whose last term dominates: unless a foreign
// `PropTypes` was imported, everything passes. So in an ordinary file with no such import this is
// always true, and the whole predicate exists to narrow the rule once the name has been claimed by
// another module. Measured: with a foreign import present, only the recorded local name qualifies.
func (walker *forbidPropTypesWalker) isPropTypesPackage(node *ast.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind {
	case ast.KindIdentifier:
		return node.Text() == walker.propertyTypesPackageName || !walker.isForeignPropTypesPackage

	case ast.KindPropertyAccessExpression:
		receiver := node.AsPropertyAccessExpression().Expression
		if receiver == nil {
			return !walker.isForeignPropTypesPackage
		}
		if receiver.Kind == ast.KindIdentifier {
			// The member arm compares against `reactPackageName` alone, never against
			// `propTypesPackageName`. That reads as an oversight upstream, since a
			// `import PT from 'prop-types'` alias is exactly what you would expect to qualify
			// here, and a first draft of this port helpfully added it.
			//
			// It is a divergence. Measured on the installed build with both a prop-types alias and
			// a foreign PropTypes import present: `PT.any` is silent upstream, in either import
			// order, while the same file without the foreign import reports. Adding the second
			// comparison made this port report where upstream does not, and the whole imported
			// corpus stayed green because no case writes both imports at once.
			//
			// The identifier arm above does consult `propTypesPackageName`, which is why the two
			// arms are not symmetric here.
			return receiver.Text() == walker.reactPackageName || !walker.isForeignPropTypesPackage
		}
		return !walker.isForeignPropTypesPackage
	}
	return false
}

// reportIfForbidden reports when a type name is on the forbid list.
func (walker *forbidPropTypesWalker) reportIfForbidden(property *ast.Node, typeName string) {
	if typeName == "" {
		return
	}
	for _, forbidden := range walker.settings.forbiddenTypes() {
		if forbidden == typeName {
			walker.ruleContext.ReportNode(property, forbidPropTypesMessage(typeName))
			return
		}
	}
}

// isPropTypesDeclarationAccess reports whether a member access names one of the three declarations.
//
// Upstream's shape guard has a second escape hatch beside the package test: a callee that is a prop
// types declaration passes even when its receiver does not look like the package. Reproduced for
// fidelity; no corpus case reaches it, and the shape it would need is a `shape` call written
// directly as `something.propTypes(...)`.
func (walker *forbidPropTypesWalker) isPropTypesDeclarationAccess(node *ast.Node) bool {
	if node == nil || node.Kind != ast.KindPropertyAccessExpression {
		return false
	}
	name := node.AsPropertyAccessExpression().Name()
	return name != nil && name.Kind == ast.KindIdentifier && walker.declarationNameKind(name.Text())
}
