package react

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// noArrayIndexKeyWalker carries the index-name stack across the recursion.
type noArrayIndexKeyWalker struct {
	ctx rule.Context

	// indexNames is every index parameter name currently in scope, outermost first. A name is
	// pushed on entering an iterator call whose callback declares one and popped on leaving it.
	indexNames []string
}

// walk visits one node, pushing and popping the stack around an iterator call.
func (walker *noArrayIndexKeyWalker) walk(node *ast.Node) {
	if node == nil {
		return
	}

	pushed := false
	switch node.Kind {
	case ast.KindCallExpression:
		if walker.checkCreateElementCall(node) {
			// Upstream returns early after handling a createElement call, so such a call never
			// also pushes an index name. Reproduced: the two arms are exclusive.
			break
		}
		if name, ok := walker.indexParameterName(node); ok {
			walker.indexNames = append(walker.indexNames, name)
			pushed = true
		}

	case ast.KindJsxAttribute:
		walker.checkAttribute(node)
	}

	node.ForEachChild(func(child *ast.Node) bool {
		walker.walk(child)
		return false
	})

	if pushed {
		walker.indexNames = walker.indexNames[:len(walker.indexNames)-1]
	}
}

// isArrayIndex reports whether a node is an identifier naming an index parameter in scope.
func (walker *noArrayIndexKeyWalker) isArrayIndex(node *ast.Node) bool {
	if node == nil || node.Kind != ast.KindIdentifier {
		return false
	}
	text := node.Text()
	for _, name := range walker.indexNames {
		if name == text {
			return true
		}
	}
	return false
}

// checkAttribute handles a `key={...}` attribute on a JSX element.
func (walker *noArrayIndexKeyWalker) checkAttribute(node *ast.Node) {
	if len(walker.indexNames) == 0 {
		// Not inside any iterator callback, so nothing can be an index. Upstream checks this first
		// too, and it is what makes `<Foo key={i} />` at the top level clean.
		//
		// The check is subsumed here and is kept as a cost guard rather than as a behavioural one.
		// A mutation forcing it always-false survived the whole fixture set, correctly: every one
		// of the five report sites in `checkKeyExpression` is gated on `isArrayIndex`, whose loop
		// over an empty slice answers false, and the one non-nil return in
		// `binaryIdentifiersNamingAnIndex` is gated on the same call. Enumerated by grep over both
		// functions rather than by reading, and no input can distinguish the two versions.
		//
		// It stays because most JSX attributes in a real tree sit outside any iterator, and this
		// returns before reading a name or an initializer for all of them. The verdict names the
		// report sites that existed when it was taken; a sixth report site not routed through
		// `isArrayIndex` would void it.
		return
	}
	attribute := node.AsJsxAttribute()
	name := attribute.Name()
	if name == nil || name.Kind != ast.KindIdentifier || name.Text() != "key" {
		return
	}
	initializer := attribute.Initializer
	if initializer == nil || initializer.Kind != ast.KindJsxExpression {
		// `key="foo"` or a bare `key`. Both silent upstream.
		return
	}
	walker.checkKeyExpression(initializer.AsJsxExpression().Expression)
}

// checkKeyExpression is upstream's `checkPropValue`, the five shapes it accepts and nothing else.
func (walker *noArrayIndexKeyWalker) checkKeyExpression(node *ast.Node) {
	if node == nil {
		return
	}

	// `key={i}`.
	if walker.isArrayIndex(node) {
		walker.ctx.ReportNode(node, messageNoArrayIndexKey)
		return
	}

	switch node.Kind {
	case ast.KindTemplateExpression:
		// `key={`foo-${bar}`}`. Upstream reports once per index reference, always on the whole
		// template rather than on the reference.
		template := node.AsTemplateExpression()
		if template.TemplateSpans == nil {
			return
		}
		for _, span := range template.TemplateSpans.Nodes {
			if span.Kind != ast.KindTemplateSpan {
				continue
			}
			if walker.isArrayIndex(span.AsTemplateSpan().Expression) {
				walker.ctx.ReportNode(node, messageNoArrayIndexKey)
			}
		}
		return

	case ast.KindBinaryExpression:
		// `key={'foo' + bar}`. Upstream collects every identifier in the binary tree and reports
		// once per one that names an index, always on the whole expression.
		//
		// A logical operator is excluded, and this is a parser difference that costs findings in
		// the reporting direction rather than the silent one. In ESTree `||`, `&&` and `??` are a
		// `LogicalExpression`, a node type distinct from `BinaryExpression`, and upstream's
		// `checkPropValue` has an arm only for the latter. Our parser gives all of them
		// `KindBinaryExpression`, so without this test `key={item.id ?? index}` reports here and is
		// silent upstream.
		//
		// Found on the real tree rather than in a fixture: the dry run produced four findings
		// ESLint does not, all of this shape, and `key={item.id ?? index}` is a completely ordinary
		// thing to write. Measured against the installed build: `??`, `||` and `&&` in a key are
		// all silent, and `+` reports.
		//
		// Upstream's own `getIdentifiersFromBinaryExpression` does carry a recursion arm for
		// logical expressions, which reads as though they were meant to be covered. They are not
		// reachable from the dispatch above, so that arm is dead in upstream. Reproduced as
		// upstream behaves rather than as upstream appears to intend.
		// This decline and the one inside `binaryIdentifiersNamingAnIndex` are a pair, and each
		// survives mutation alone. Removing either one leaves the other declining the same inputs:
		// the dispatch's decline stops a logical key before the walk, and the walk's decline stops
		// it at the top of the tree, so both routes end in silence. Mutating both at once is caught
		// by seven lines. Kept as a pair rather than collapsed, because they answer at different
		// depths and a nested shape reaches only the second.
		if noArrayIndexKeyIsLogicalOperator(node.AsBinaryExpression().OperatorToken.Kind) {
			return
		}
		for range walker.binaryIdentifiersNamingAnIndex(node) {
			walker.ctx.ReportNode(node, messageNoArrayIndexKey)
		}
		return

	case ast.KindCallExpression:
		call := node.AsCallExpression()
		callee := call.Expression
		if callee == nil {
			return
		}

		// `key={bar.toString()}`.
		if callee.Kind == ast.KindPropertyAccessExpression {
			access := callee.AsPropertyAccessExpression()
			methodName := access.Name()
			if walker.isArrayIndex(access.Expression) &&
				methodName != nil && methodName.Kind == ast.KindIdentifier &&
				methodName.Text() == "toString" {
				walker.ctx.ReportNode(node, messageNoArrayIndexKey)
			}
			return
		}

		// `key={String(bar)}`. This is the one site that reports the argument rather than the
		// whole call, which the span fixtures pin.
		if callee.Kind == ast.KindIdentifier && callee.Text() == "String" &&
			call.Arguments != nil && len(call.Arguments.Nodes) > 0 &&
			walker.isArrayIndex(call.Arguments.Nodes[0]) {
			walker.ctx.ReportNode(call.Arguments.Nodes[0], messageNoArrayIndexKey)
		}
	}
}

// binaryIdentifiersNamingAnIndex returns every identifier in a binary expression tree that names an
// index parameter.
//
// Upstream's `getIdentifiersFromBinaryExpression` recurses only through binary expressions and
// returns null for anything else, so `'x-' + foo(i)` contributes nothing: the call is not an
// identifier and not a binary expression. Reproduced rather than widened.
func (walker *noArrayIndexKeyWalker) binaryIdentifiersNamingAnIndex(node *ast.Node) []*ast.Node {
	if node == nil {
		return nil
	}
	if node.Kind == ast.KindIdentifier {
		if walker.isArrayIndex(node) {
			return []*ast.Node{node}
		}
		return nil
	}
	if node.Kind != ast.KindBinaryExpression {
		return nil
	}
	binary := node.AsBinaryExpression()
	if noArrayIndexKeyIsLogicalOperator(binary.OperatorToken.Kind) {
		// A logical operator is a different node type upstream, so its subtree is not walked. See
		// the dispatch above for the measurement.
		return nil
	}
	found := walker.binaryIdentifiersNamingAnIndex(binary.Left)
	return append(found, walker.binaryIdentifiersNamingAnIndex(binary.Right)...)
}

// indexParameterName returns the name of the index parameter an iterator call's callback declares.
//
// Nil-safe at every step, and the steps are where a port panics rather than where it mis-decides.
// A callee can be a call expression (`getFoo().map(...)`), an element access (`foo["map"](...)`) or
// a bare identifier, and `Text()` panics on the first two, so the kind is checked before the text
// is read every time. Measured: the computed form is silent upstream and the call-as-callee form
// reports, so neither may crash and they must answer differently.
func (walker *noArrayIndexKeyWalker) indexParameterName(node *ast.Node) (string, bool) {
	call := node.AsCallExpression()
	callee := call.Expression
	if callee == nil || callee.Kind != ast.KindPropertyAccessExpression {
		return "", false
	}

	access := callee.AsPropertyAccessExpression()
	methodName := access.Name()
	if methodName == nil || methodName.Kind != ast.KindIdentifier {
		// The kind half of this is equivalent rather than load-bearing, and it is kept because the
		// reason is not obvious from the line.
		//
		// A property access's name can be a private identifier here, so `foo.#map(...)` is a real
		// parse rather than a hypothetical. It is not a panic hazard: probed, `Text()` on a private
		// identifier returns `#map` rather than crashing, unlike `Text()` on a property access or a
		// call expression, which is why the callee kind test above is a genuine crash guard and this
		// one is not.
		//
		// No input can distinguish the two versions, and the argument is one sentence: every key in
		// the iterator table is a bare method name, and a private identifier's text always carries a
		// leading hash, so the lookup misses either way. A mutation removing the kind test survived
		// the fixtures, correctly.
		return "", false
	}
	position, known := noArrayIndexKeyIteratorPositions[methodName.Text()]
	if !known {
		return "", false
	}

	// `React.Children.map(collection, callback)` and `Children.map(...)` take the callback second.
	callbackIndex := 0
	if noArrayIndexKeyIsReactChildrenCall(access) {
		callbackIndex = 1
	}

	if call.Arguments == nil || len(call.Arguments.Nodes) <= callbackIndex {
		return "", false
	}
	callback := call.Arguments.Nodes[callbackIndex]
	if callback == nil {
		return "", false
	}

	var parameters *ast.NodeList
	switch callback.Kind {
	case ast.KindArrowFunction:
		parameters = callback.AsArrowFunction().Parameters
	case ast.KindFunctionExpression:
		parameters = callback.AsFunctionExpression().Parameters
	default:
		// A reference rather than a literal function. `foo.map(cb)` is silent upstream.
		return "", false
	}

	if parameters == nil || len(parameters.Nodes) < position+1 {
		return "", false
	}

	parameter := parameters.Nodes[position].AsParameterDeclaration()
	if parameter.Initializer != nil {
		// A defaulted parameter, and this is a parser difference rather than a rule decision.
		//
		// Upstream reads `params[n].name`, and ESTree wraps a defaulted parameter in an
		// `AssignmentPattern` whose `.name` is undefined, so `foo.map((bar, i = 0) => ...)` is
		// silent there. Measured on the installed build. Our parser keeps the identifier as the
		// parameter's name and hangs the default off `Initializer`, so the name is readable and a
		// port that only checked the name kind would report where upstream does not.
		//
		// Probed rather than assumed: `(bar, i = 0)` gives `name=KindIdentifier(i) init=yes`, while
		// `(bar, {i})` gives `name=KindObjectBindingPattern`. Two different shapes upstream
		// collapses into one absent name.
		return "", false
	}
	if parameter.DotDotDotToken != nil {
		// A rest parameter is an ESTree `RestElement`, also without a `.name`. It cannot be an
		// index in any case, since it collects every remaining argument.
		return "", false
	}

	parameterName := parameter.Name()
	if parameterName == nil || parameterName.Kind != ast.KindIdentifier {
		// A destructuring pattern has no name upstream either, and `params[n].name` is undefined
		// there. Measured: `foo.map((bar, {i}) => <Foo key={i} />)` is silent.
		return "", false
	}
	return parameterName.Text(), true
}

// checkCreateElementCall handles `React.createElement(type, props)` and its clone counterpart, and
// reports whether the call was one.
//
// The boolean is what makes the two arms exclusive: upstream returns early after handling such a
// call, so a `createElement` call never also pushes an index name.
func (walker *noArrayIndexKeyWalker) checkCreateElementCall(node *ast.Node) bool {
	call := node.AsCallExpression()
	if !walker.isCreateOrCloneElement(call.Expression) {
		return false
	}
	if call.Arguments == nil || len(call.Arguments.Nodes) <= 1 {
		return false
	}
	if len(walker.indexNames) == 0 {
		// Upstream checks the stack before reading the props, so a createElement call outside any
		// iterator is silent and still counts as handled. Measured silent.
		return true
	}

	properties := call.Arguments.Nodes[1]
	if properties == nil || properties.Kind != ast.KindObjectLiteralExpression {
		return true
	}

	list := properties.AsObjectLiteralExpression().Properties
	if list == nil {
		return true
	}
	for _, property := range list.Nodes {
		if property.Kind != ast.KindPropertyAssignment {
			// A spread element carries no key, which is upstream's `{ ...foo }` comment.
			continue
		}
		assignment := property.AsPropertyAssignment()
		name := assignment.Name()
		if name == nil || name.Kind != ast.KindIdentifier || name.Text() != "key" {
			// A computed `["key"]` is a KindComputedPropertyName here and has no identifier text,
			// which matches upstream reading `prop.key.name` off a non-Identifier as undefined.
			// Measured silent.
			continue
		}
		walker.checkKeyExpression(assignment.Initializer)
	}
	return true
}

// isCreateOrCloneElement reports whether a callee names React's element factory.
//
// Two forms, matching upstream. A property access whose object is the React pragma and whose
// property is `createElement` or `cloneElement`; or a bare identifier that resolves to an import
// from the module `react` specifically. The second is upstream's variable lookup, answered here
// through the checker, and measured both ways: an import of `cloneElement` from `react` reports and
// the same name imported from another module does not.
func (walker *noArrayIndexKeyWalker) isCreateOrCloneElement(callee *ast.Node) bool {
	if callee == nil {
		return false
	}

	switch callee.Kind {
	case ast.KindPropertyAccessExpression:
		access := callee.AsPropertyAccessExpression()
		receiver := access.Expression
		if receiver == nil || receiver.Kind != ast.KindIdentifier || receiver.Text() != "React" {
			return false
		}
		name := access.Name()
		if name == nil || name.Kind != ast.KindIdentifier {
			return false
		}
		return name.Text() == "createElement" || name.Text() == "cloneElement"

	case ast.KindIdentifier:
		return walker.resolvesToReactImport(callee)
	}
	return false
}

// resolvesToReactImport reports whether an identifier was imported from the module `react`.
//
// Upstream asks its own variable index for an `ImportSpecifier` and reads the parent import's
// source. The checker resolves the same binding; the declaration is an import specifier whose
// import declaration carries the module specifier.
//
// Note that upstream checks the source and not the imported name, so any identifier imported from
// `react` qualifies here, not only `createElement` and `cloneElement`. That is upstream being loose
// rather than upstream being right, and it is reproduced: the name has already been used as a
// callee with a props object, so the shapes that reach this are narrow in practice.
func (walker *noArrayIndexKeyWalker) resolvesToReactImport(identifier *ast.Node) bool {
	symbol := walker.ctx.TypeChecker.GetSymbolAtLocation(identifier)
	if symbol == nil {
		return false
	}
	for _, declaration := range symbol.Declarations {
		if declaration == nil || declaration.Kind != ast.KindImportSpecifier {
			continue
		}
		specifier := noArrayIndexKeyImportModuleSpecifier(declaration)
		if specifier != nil && specifier.Kind == ast.KindStringLiteral &&
			specifier.Text() == "react" {
			return true
		}
	}
	return false
}
