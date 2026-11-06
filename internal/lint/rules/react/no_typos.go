package react

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/imports"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// noTyposStaticClassProperties are the four names whose casing this rule polices.
//
// Upstream's `STATIC_CLASS_PROPERTIES`. A property whose lowercased spelling matches one of these
// and whose exact spelling does not is a typo: `PropTypes` on a component is not `propTypes` and
// React silently ignores it, so the declaration reads as configuration and does nothing.
var noTyposStaticClassProperties = []string{"propTypes", "contextTypes", "childContextTypes", "defaultProps"}

// noTyposInstanceLifecycleMethods is upstream's `lifecycleMethods.instance`, name for name.
var noTyposInstanceLifecycleMethods = []string{
	"getDefaultProps",
	"getInitialState",
	"getChildContext",
	"componentWillMount",
	"UNSAFE_componentWillMount",
	"componentDidMount",
	"componentWillReceiveProps",
	"UNSAFE_componentWillReceiveProps",
	"shouldComponentUpdate",
	"componentWillUpdate",
	"UNSAFE_componentWillUpdate",
	"getSnapshotBeforeUpdate",
	"componentDidUpdate",
	"componentDidCatch",
	"componentWillUnmount",
	"render",
}

// noTyposStaticLifecycleMethods is upstream's `lifecycleMethods.static`.
//
// One name, and it carries two separate judgments: written without `static` it reports
// `staticLifecycleMethod`, and written with the wrong casing it reports `typoLifecycleMethod` like
// any other. A method spelled `getderivedstatefromprops` without `static` reports both.
var noTyposStaticLifecycleMethods = []string{"getDerivedStateFromProps"}

// noTyposPropTypeNames is `Object.keys(require('prop-types'))`, verbatim from version 15.8.1.
//
// Three of these are not prop types at all. `checkPropTypes` and `resetWarningCache` are module
// helpers and `PropTypes` is the package's own self-reference, and they are accepted here because
// upstream takes `Object.keys` of the whole module rather than a curated list. So
// `PropTypes.checkPropTypes` in a propTypes object is silent upstream, which is an accident rather
// than a decision, and it is reproduced because widening or narrowing the set would change verdicts
// upstream does not change. Measured against the installed build.
var noTyposPropTypeNames = []string{
	"array", "bigint", "bool", "func", "number", "object", "string", "symbol",
	"any", "arrayOf", "element", "elementType", "instanceOf", "node", "objectOf",
	"oneOf", "oneOfType", "shape", "exact",
	"checkPropTypes", "resetWarningCache", "PropTypes",
}

// NoTypos flags a React declaration whose casing means React will never read it.
//
//	valid:   class Foo extends React.Component { static propTypes = {}; }
//	valid:   class Foo extends React.Component { componentDidMount() {} }
//	invalid: class Foo extends React.Component { static PropTypes = {}; }
//	invalid: class Foo extends React.Component { ComponentDidMount() {} }
//	invalid: Foo.propTypes = { a: PropTypes.strng };
//
// Ported from `react/no-typos` in `eslint-plugin-react`, the implementation that defined it. Its 43
// clean and 53 reporting cases were extracted mechanically from the clone by stubbing its
// `RuleTester`, then all 96 were run against the installed build, 7.37.5, through the ESLint Linter
// API with the TypeScript parser. The two authorities agreed on 95; the one exception is a clone
// case the installed build cannot reproduce, recorded in the test.
//
// # Eight message ids, and each is a different judgment
//
//	typoStaticClassProp    `static PropTypes` on a class
//	typoPropDeclaration    the same casing error in a createReactClass object
//	typoLifecycleMethod    `ComponentDidMount` instead of `componentDidMount`
//	staticLifecycleMethod  `getDerivedStateFromProps` written without `static`
//	typoPropType           `PropTypes.strng`, a name prop-types does not export
//	typoPropTypeChain      `PropTypes.string.isRequird`, a qualifier that is not `isRequired`
//	noPropTypesBinding     `import 'prop-types'` with no local name
//	noReactBinding         `import 'react'` with no local name
//
// # The prop-type arms are inert until an import names the package
//
// `checkValidProp` returns immediately when neither package name is known, so a file that never
// imports `prop-types` or `react` produces no `typoPropType` or `typoPropTypeChain` finding however
// misspelled its propTypes are. That is upstream's guard, and it is why this rule tracks the two
// import bindings as state across the file rather than deciding per node.
//
// # What decides that something is a component, and why the shelf is enough here
//
// Three of the four arms gate on `componentUtil.isES6Component` or `isES5Component`, both of which
// the shelf answers. The fourth, the member-assignment arm, also accepts a non-class whose body
// returns JSX, which is upstream's `utils.isReturningJSX`. That predicate is narrow and is
// implemented directly here rather than reached for: measured against the installed build, it asks
// only whether a `return` in the function's own body returns JSX. There is no name gate, `null` does
// not count, and JSX inside a nested callback does not count. Every row is pinned by fixture.
//
// This is the difference from `default-props-match-prop-types`, which I stopped on in the same
// batch: that rule needs the registry's resolved `declaredPropTypes`, and this one needs a
// three-line predicate.
var NoTypos = rule.Rule[rule.NoOptions]{
	Name: "react/no-typos",
	Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
		// The two import bindings, tracked across the whole file. Upstream keeps them as closure
		// state and its listeners run in source order, so an import below a usage does not name the
		// package for that usage. Reproduced by walking the file once from the source node, in
		// order, rather than by registering per-kind listeners whose relative order is not stated.
		return rule.Listeners{
			ast.KindSourceFile: func(sourceFile *ast.Node) {
				state := &noTyposState{ruleContext: ruleContext}
				state.walk(sourceFile)
			},
		}
	},
}

// noTyposImportSpecifier is one imported binding, with the name it was imported and bound under.
type noTyposImportSpecifier struct {
	// importedName is the name on the package's side, empty for a default or namespace import.
	importedName string

	// localName is the name the file uses.
	localName string
}

// noTyposImportSpecifiers flattens an import declaration into upstream's flat specifier list.
//
// Upstream's ESTree gives one array holding the default specifier, the namespace specifier and every
// named specifier together, and this rule reads `specifiers[0]` and searches the rest, so the order
// matters and the flattening is this rule's business rather than the shelf's. Our parser splits the
// three shapes across separate fields, which `imports.BindingsOf` reads; this reassembles them in
// upstream's order, default first and then namespace or named.
//
// The three shapes come from the shelf rather than from a fourth inline copy of the same walk. That
// helper was lifted precisely because two rule packages held near-identical copies and three
// research passes each reported it absent, and this rule nearly became the third copy: it was
// written inline first and `TestRulePackagesDoNotReachPastWrappedAccessors` is what found it.
func noTyposImportSpecifiers(declaration *ast.Node) []noTyposImportSpecifier {
	bindings := imports.BindingsOf(declaration)

	specifiers := []noTyposImportSpecifier{}
	if bindings.Default != nil && bindings.Default.Kind == ast.KindIdentifier {
		specifiers = append(specifiers, noTyposImportSpecifier{localName: bindings.Default.Text()})
	}
	if bindings.Namespace != nil {
		if name := bindings.Namespace.Name(); name != nil && name.Kind == ast.KindIdentifier {
			specifiers = append(specifiers, noTyposImportSpecifier{localName: name.Text()})
		}
	}
	for _, element := range bindings.Named {
		if element.Kind != ast.KindImportSpecifier {
			continue
		}
		specifier := element.AsImportSpecifier()
		local := specifier.Name()
		if local == nil || local.Kind != ast.KindIdentifier {
			continue
		}
		imported := local.Text()
		if specifier.PropertyName != nil && specifier.PropertyName.Kind == ast.KindIdentifier {
			imported = specifier.PropertyName.Text()
		}
		specifiers = append(specifiers,
			noTyposImportSpecifier{importedName: imported, localName: local.Text()})
	}
	return specifiers
}

// noTyposFindDeclaration searches the file for a declaration binding this name.
//
// A class, a function, or a variable whose initializer is a function. Written as a walk rather than
// through the checker because the question is syntactic: upstream resolves through its own registry
// of components it has seen in this file, never through a type graph, and a name imported from
// elsewhere has no declaration here for either implementation to find.
func noTyposFindDeclaration(from *ast.Node, name string) *ast.Node {
	sourceFile := from
	for sourceFile != nil && sourceFile.Kind != ast.KindSourceFile {
		sourceFile = sourceFile.Parent
	}
	if sourceFile == nil {
		return nil
	}

	var found *ast.Node
	var walk func(node *ast.Node)
	walk = func(node *ast.Node) {
		if node == nil || found != nil {
			return
		}
		switch node.Kind {
		case ast.KindClassDeclaration:
			if declarationName := node.AsClassDeclaration().Name(); declarationName != nil &&
				declarationName.Kind == ast.KindIdentifier && declarationName.Text() == name {
				found = node
				return
			}
		case ast.KindFunctionDeclaration:
			if declarationName := node.AsFunctionDeclaration().Name(); declarationName != nil &&
				declarationName.Kind == ast.KindIdentifier && declarationName.Text() == name {
				found = node
				return
			}
		case ast.KindVariableDeclaration:
			declaration := node.AsVariableDeclaration()
			declarationName := declaration.Name()
			if declarationName != nil && declarationName.Kind == ast.KindIdentifier &&
				declarationName.Text() == name && declaration.Initializer != nil {
				found = declaration.Initializer
				return
			}
		}
		node.ForEachChild(func(child *ast.Node) bool {
			walk(child)
			return false
		})
	}
	walk(sourceFile)
	return found
}

// noTyposReturnsJsx reports whether a function's own body returns JSX.
//
// A concise arrow body counts when it is JSX directly. Otherwise every `return` in the body counts,
// except those inside a nested function, which belong to that function instead. Reproduces
// upstream's `isReturningJSX` on every shape measured; the table is at `namesAComponent`.
func noTyposReturnsJsx(node *ast.Node) bool {
	var body *ast.Node
	switch node.Kind {
	case ast.KindFunctionDeclaration:
		body = node.AsFunctionDeclaration().Body
	case ast.KindFunctionExpression:
		body = node.AsFunctionExpression().Body
	case ast.KindArrowFunction:
		body = node.AsArrowFunction().Body
	default:
		return false
	}
	if body == nil {
		return false
	}

	// A concise arrow body is the returned expression itself.
	if body.Kind != ast.KindBlock {
		return noTyposIsJsxExpression(body)
	}

	found := false
	var walk func(inner *ast.Node)
	walk = func(inner *ast.Node) {
		if inner == nil || found {
			return
		}
		// A nested function's returns are its own, not this function's.
		switch inner.Kind {
		case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction:
			return
		case ast.KindReturnStatement:
			if noTyposIsJsxExpression(inner.AsReturnStatement().Expression) {
				found = true
				return
			}
		}
		inner.ForEachChild(func(child *ast.Node) bool {
			walk(child)
			return false
		})
	}
	body.ForEachChild(func(child *ast.Node) bool {
		walk(child)
		return false
	})
	return found
}

// noTyposIsJsxExpression reports whether an expression is a JSX element or fragment.
//
// Parentheses are unwrapped first, and they nest. Upstream's parser folds them away entirely, so
// `return (<div/>)` reaches its JSX test as a bare element, and without this loop eight of the
// corpus's own reporting cases went silent here: wrapping a returned element in parentheses is the
// ordinary way to write a multi-line component. `((<div/>))` measured too, which is why this is a
// loop rather than one step.
//
// Written as a loop rather than with `ast.SkipParentheses`, which dereferences its argument, and the
// thing being unwrapped here is a return argument that is legitimately absent for a bare `return;`.
func noTyposIsJsxExpression(expression *ast.Node) bool {
	unwrapped := expression
	for unwrapped != nil && unwrapped.Kind == ast.KindParenthesizedExpression {
		unwrapped = unwrapped.AsParenthesizedExpression().Expression
	}
	if unwrapped == nil {
		return false
	}
	switch unwrapped.Kind {
	case ast.KindJsxElement, ast.KindJsxSelfClosingElement, ast.KindJsxFragment:
		return true
	}
	return false
}

// noTyposIsCreateReactClassSpecification reports whether an object is a createReactClass body.
//
// Written inline rather than through the shelf's `react.IsEs5ComponentCall`, which accepts
// `createClass` in both spellings. Upstream's `isES5Component` compares the callee against the
// createClass pragma, which is `createReactClass` with no React settings read here to change it, so
// `createClass` fails in either spelling. Measured on the installed build, all five:
//
//	createReactClass({...})          counts
//	React.createReactClass({...})    counts, the namespaced form of the same pragma
//	createClass({...})               SILENT, not the pragma
//	React.createClass({...})         SILENT, likewise
//	Other.createReactClass({...})    SILENT, the namespace must be React
//
// The namespaced row is the one this port got wrong first, and eleven corpus cases caught it: they
// are written `React.createReactClass` and the rule was silent on every one.
func noTyposIsCreateReactClassSpecification(object *ast.Node) bool {
	parent := object.Parent
	if parent == nil || parent.Kind != ast.KindCallExpression {
		return false
	}
	callee := parent.AsCallExpression().Expression
	if callee == nil {
		return false
	}

	if callee.Kind == ast.KindIdentifier {
		return callee.Text() == "createReactClass"
	}
	if callee.Kind == ast.KindPropertyAccessExpression {
		access := callee.AsPropertyAccessExpression()
		object := access.Expression
		if object == nil || object.Kind != ast.KindIdentifier || object.Text() != reactPragmaName {
			return false
		}
		name := access.Name()
		return name != nil && name.Kind == ast.KindIdentifier && name.Text() == "createReactClass"
	}
	return false
}

// noTyposPropertyValue returns an object member's value expression, or nil.
func noTyposPropertyValue(property *ast.Node) *ast.Node {
	switch property.Kind {
	case ast.KindPropertyAssignment:
		return property.AsPropertyAssignment().Initializer
	case ast.KindMethodDeclaration:
		// A method shorthand has no separate value node. Upstream sees a FunctionExpression here,
		// which `checkValidPropObject` declines because it is not an ObjectExpression, so the value
		// arm contributes nothing either way.
		return nil
	}
	return nil
}

// noTyposKeyName renders a property key when it has a static name.
//
// Upstream reads `key.name`, falling back to `key.value` for a literal, and declines a private name
// or a computed key whose value is not a string.
func noTyposKeyName(key *ast.Node) (string, bool) {
	if key == nil {
		return "", false
	}
	switch key.Kind {
	case ast.KindIdentifier, ast.KindStringLiteral, ast.KindNumericLiteral:
		return key.Text(), true
	case ast.KindComputedPropertyName:
		inner := key.AsComputedPropertyName().Expression
		if inner != nil && inner.Kind == ast.KindStringLiteral {
			return inner.Text(), true
		}
	}
	return "", false
}

// noTyposMatchesStaticClassPropertyIgnoringCase is upstream's first filter on the member arm.
func noTyposMatchesStaticClassPropertyIgnoringCase(name string) bool {
	for _, canonical := range noTyposStaticClassProperties {
		if strings.EqualFold(canonical, name) {
			return true
		}
	}
	return false
}

// noTyposIsStatic reports whether a class member carries `static`.
func noTyposIsStatic(member *ast.Node) bool {
	modifiers := member.Modifiers()
	if modifiers == nil {
		return false
	}
	for _, modifier := range modifiers.Nodes {
		if modifier.Kind == ast.KindStaticKeyword {
			return true
		}
	}
	return false
}

// noTyposEnclosingClass returns the class a member belongs to.
//
// Upstream reaches `node.parent.parent`, which is the member's class body's class. Ours puts members
// directly on the class, so this is one step, guarded because the parser can hand back a partial
// chain on malformed input.
func noTyposEnclosingClass(member *ast.Node) *ast.Node {
	parent := member.Parent
	if parent == nil {
		return nil
	}
	if parent.Kind == ast.KindClassDeclaration || parent.Kind == ast.KindClassExpression {
		return parent
	}
	return nil
}
