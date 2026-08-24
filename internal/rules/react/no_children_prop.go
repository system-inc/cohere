package react

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/utils/jsx"
	utilsreact "github.com/system-inc/verify/internal/utils/react"
)

var messageNoChildrenProp = rule.Message{
	Id: "noChildrenProp",
	Description: "Children are being passed as a prop rather than as children. React reads " +
		"`props.children` from what is nested between the tags, so writing the prop by hand " +
		"puts a second source of truth beside the one the reader can see, and whichever is " +
		"written last silently wins. Nest the children between the opening and closing tags, " +
		"or pass them as the arguments after the props object to `createElement`.",
}

// NoChildrenProp flags children passed as a prop instead of as children.
//
//	valid:   <div>Children</div>
//	valid:   React.createElement("div", {}, "Children")
//	invalid: <div children="Children" />
//	invalid: React.createElement("div", {children: "Children"})
//
// Ported from `react/no-children-prop`, read against oxc's `no_children_prop.rs` and against
// `eslint-plugin-react`'s own `no-children-prop.js`. The two sources disagree in three places and
// oxc is followed in all three, because oxc is what the gate being replaced actually runs.
//
// # Where the two upstreams disagree, and which one this follows
//
// **The option surface.** ESLint carries an `allowFunctions` option and three further messages
// (`nestFunction`, `passFunctionAsArgs`, `nestChildren`, `passChildrenAsArgs`) that exempt a
// function-valued child and instead report a function passed the other way around. oxc implements
// none of it: its `declare_oxc_lint` takes no configuration and it emits one diagnostic. This rule
// takes no options for the same reason, and adding them later is a real behavior change rather
// than a refinement.
//
// **Where the finding points.** ESLint reports the whole `JSXAttribute` and the whole
// `CallExpression`. oxc reports the *name* alone: `attr_ident.span` and `prop.key.span()`. The
// snapshot pins this at column 6 spanning eight characters for `<div children />`, so it is the
// name and not the attribute. Following ESLint here would underline the entire call expression on
// a `createElement` finding, which is most of the line.
//
// **Whether the value is read.** ESLint requires `childrenProp.value` to exist before reporting.
// oxc reports on the key alone, so `{children}` shorthand and `{children() {}}` report there and
// not in ESLint. Followed, and pinned by fixtures below.
//
// # The property kinds, which our AST splits and oxc's does not
//
// oxc's object literal has exactly two property kinds, `ObjectProperty` and `SpreadProperty`, so a
// method, a getter, a setter and a shorthand are all one kind there, distinguished by boolean
// flags the rule never reads. Our AST gives each its own node kind, so matching only
// `KindPropertyAssignment` would silently drop three shapes oxc reports. Measured on our parser:
// `{children}` is a ShorthandPropertyAssignment, `{children() {}}` a MethodDeclaration, and
// `{get children() {}}` a GetAccessor. All four are accepted here and only a spread is declined,
// which is the same partition oxc draws.
//
// # Computed keys, and the one place `Text()` will crash
//
// A computed key is a distinct `ComputedPropertyName` node in our tree and an ordinary key with a
// `computed` flag in oxc's. Calling `Text()` on ours panics outright rather than returning empty,
// which a probe hit before any of this was written, so the kind is checked before the text is read
// at every site here.
//
// Which computed keys count is oxc's judgment rather than a simplification. `is_specific_static_name`
// routes through `static_name`, which answers for a string, numeric, bigint or single-quasi
// template key and returns `None` for anything it would have to evaluate. So `{["children"]: 1}`
// reports and `{[children]: 1}` does not, and the difference is that the first names the property
// and the second names a variable holding some other name. `staticKeyName` below draws exactly
// that line.
var NoChildrenProp = rule.Rule{
	// No namespace prefix. The config writes `react/no-children-prop` and the parity guard strips
	// the namespace on a `/` boundary, so a rule named `react-no-children-prop` would match no
	// inventory entry, lint no files, and still pass every fixture in this file. The first rule in
	// `internal/rules/next/` shipped that way and was inert, which is why this comment is here.
	Name: "no-children-prop",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		return rule.Listeners{
			// A spread attribute (`<div {...props} />`) produces no JsxAttribute node at all, so it
			// is invisible to this listener rather than exempted by it. That matches oxc, which
			// listens on `JSXAttribute` and therefore also never sees one. A spread whose object
			// carries `children` is consequently not reported by either tool, and upstream's
			// `<MyComponent {...props} children="Children" />` fail case reports on the written
			// attribute beside the spread rather than on anything inside it.
			ast.KindJsxAttribute: func(node *ast.Node) {
				// AttributeName declines a namespaced name (`<svg xlink:children="x" />`), which is
				// the same shape oxc declines by destructuring `JSXAttributeName::Identifier` and
				// returning early on anything else.
				name, named := jsx.AttributeName(node)
				if !named || name != "children" {
					return
				}
				ctx.ReportNode(node.AsJsxAttribute().Name(), messageNoChildrenProp)
			},

			ast.KindCallExpression: func(node *ast.Node) {
				if !utilsreact.IsCreateElementCall(node) {
					return
				}
				properties := createElementPropertiesObject(node)
				if properties == nil {
					return
				}
				// Only the first matching key reports, matching oxc's `find_map`, which stops at
				// the first hit. An object writing `children` twice is a syntax the parser accepts
				// and reports once upstream, so reporting twice here would be a divergence nothing
				// asked for.
				for _, property := range properties.Nodes {
					key := staticKeyNode(property)
					if key == nil {
						continue
					}
					if name, static := staticKeyName(key); static && name == "children" {
						ctx.ReportNode(key, messageNoChildrenProp)
						return
					}
				}
			},
		}
	},
}

// createElementPropertiesObject returns the props object literal a createElement call was given.
//
// The second argument specifically, and only when it is an object literal. `createElement("div")`
// has no second argument, `createElement("div", undefined)` has one that is not an object, and
// `createElement("div", "Children")` passes a child in the props position, which is legal and
// carries no props at all. oxc expresses all three as one pattern match,
// `Some(Argument::ObjectExpression(_)) = arguments.get(1)`, and each of the three is a passing
// fixture upstream ships.
func createElementPropertiesObject(node *ast.Node) *ast.NodeList {
	arguments := node.AsCallExpression().Arguments
	if arguments == nil || len(arguments.Nodes) < 2 {
		return nil
	}
	second := arguments.Nodes[1]
	if second == nil || second.Kind != ast.KindObjectLiteralExpression {
		return nil
	}
	return second.AsObjectLiteralExpression().Properties
}

// staticKeyNode returns the key node of an object literal member that has one, or nil.
//
// A spread carries no key and is the one member kind oxc also declines, by matching
// `ObjectPropertyKind::ObjectProperty` and letting `SpreadProperty` fall through. The other four
// kinds are a single `ObjectProperty` in oxc's tree and four distinct node kinds in ours, so they
// are listed rather than collapsed: dropping any one of them would make this rule silent where
// oxc reports, and no upstream fixture covers three of the four.
func staticKeyNode(property *ast.Node) *ast.Node {
	switch property.Kind {
	case ast.KindPropertyAssignment,
		ast.KindShorthandPropertyAssignment,
		ast.KindMethodDeclaration,
		ast.KindGetAccessor,
		ast.KindSetAccessor:
		return property.Name()
	}
	return nil
}

// staticKeyName returns the property name a key writes, when the key names it without evaluation.
//
// This is oxc's `PropertyKey::static_name` at our AST's spelling, narrowed to the kinds that can
// answer `children`. Upstream also answers for a numeric, bigint, regular-expression and null key,
// and those are deliberately absent here rather than overlooked: this rule compares the result
// against one fixed identifier, and no number, bigint or null renders as `children`, so those
// branches could never change an answer. Measured rather than reasoned, on our own parser: `{2: 1}`,
// `{0x10: 1}` and `{2n: 1}` are silent with the branches present, and a sweep dropping
// `KindNumericLiteral` survived every fixture because it could not change a verdict. A branch that
// cannot change a verdict is a branch no test can guard, so it is gone instead of being covered by
// a fixture that asserts nothing.
//
// The consequence to hold if this function is ever lifted onto the shelf: it is correct for a
// caller matching a non-numeric name and wrong for one matching `"2"`. That is the reason it is
// unexported and lives beside its single caller.
//
// The kind check is load-bearing rather than defensive. `Node.Text()` panics on a
// ComputedPropertyName rather than returning empty, so a version of this that read the text first
// and filtered afterwards would crash the linter on `{[children]: 1}`. That is not a hypothetical:
// a probe written before this rule existed hit exactly that panic.
func staticKeyName(key *ast.Node) (string, bool) {
	switch key.Kind {
	// No template arm here, and its absence is measured rather than an oversight. oxc's
	// `static_name` answers for a template through `single_quasi`, but a bare `` {`k`: 1} `` is not
	// valid JavaScript: our parser produces no object literal for that source at all, so a
	// template can only reach a property key inside brackets and is handled in the computed arm
	// below. A sweep confirmed the branch was unreachable rather than merely untested, so it is
	// deleted instead of covered by a fixture that could never fail.
	case ast.KindIdentifier,
		ast.KindStringLiteral:
		return key.Text(), true

	case ast.KindComputedPropertyName:
		// A computed key is static exactly when what is inside the brackets is a literal. oxc draws
		// the same line one level up: `{["children"]: 1}` parses there with a StringLiteral key and
		// `static_name` answers, while `{[children]: 1}` parses with an identifier *reference* key
		// that `static_name` returns None for, which its own doc states as `[a]: 1` returning None.
		inner := key.AsComputedPropertyName().Expression
		if inner == nil {
			return "", false
		}
		switch inner.Kind {
		case ast.KindStringLiteral,
			ast.KindNoSubstitutionTemplateLiteral:
			return inner.Text(), true
		}
	}
	return "", false
}
