package react

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/utils/jsx"
	utilsreact "github.com/system-inc/verify/internal/utils/react"
)

var messageThisRefsDeprecated = rule.Message{
	Id: "thisRefsDeprecated",
	Description: "`this.refs` is the string-ref registry and is deprecated. It only ever holds " +
		"refs that were attached by name, so it is empty in any component written the modern " +
		"way, and it is gone entirely in React 19. Attach the node to a field of your own from " +
		"a ref callback and read that field instead of going through `refs`.",
}

var messageStringInRefDeprecated = rule.Message{
	Id: "stringInRefDeprecated",
	Description: "A ref written as a string asks React to keep the node in a name-keyed registry " +
		"rather than handing it to you. The name is resolved against whichever component owns " +
		"the element, which breaks when the element is passed through as a child, and it cannot " +
		"be typed or renamed with any confidence. React 19 removes the behavior outright. Pass a " +
		"ref callback or a ref object instead.",
}

// NoStringRefsOptions is the decoded option object.
//
// One field, matching both upstreams. ESLint's `meta.schema` is the authoritative surface here and
// it declares exactly `{noTemplateLiterals: boolean}` with `additionalProperties: false`; oxc's
// `NoStringRefs` struct declares the same single field. Our own rule inventory records this rule as
// `"options": "no"`, which is wrong, and the config line added for it passes a bare severity so
// nothing depended on the mistake.
type NoStringRefsOptions struct {
	// NoTemplateLiterals additionally reports a ref written as a template literal.
	//
	// Off by default at both upstreams, and the default is the whole reason the option exists: a
	// template ref is reported by neither tool unless it is asked for. Measured against the release
	// oxlint binary rather than read, on both settings, because the two template shapes below are
	// the part of this rule most likely to be got wrong quietly.
	NoTemplateLiterals bool `json:"noTemplateLiterals"`
}

// NoStringRefs flags the two halves of React's removed string-ref feature.
//
//	valid:   <div ref={c => { this.hello = c; }} />
//	valid:   var Hello = function() { return this.refs; };      (no enclosing component)
//	valid:   <div ref={`hello`} />                              (default; reported under the option)
//	invalid: <div ref="hello" />
//	invalid: <div ref={'hello'} />
//	invalid: class H extends React.Component { m() { return this.refs.hello; } }
//	invalid: <div ref={`hello`} />                              (noTemplateLiterals: true)
//
// Ported from `react/no-string-refs`, read against oxc's `no_string_refs.rs` and against
// `eslint-plugin-react`'s own `no-string-refs.js`, and checked against the release oxlint binary on
// constructed inputs wherever the two sources left a shape unstated.
//
// # One rule name, two judgments
//
// The name reads as a single judgment about ref attributes and it is not. Both upstreams carry two
// message ids, `thisRefsDeprecated` and `stringInRefDeprecated`, and the two halves answer to
// different gates: the `this.refs` half requires an enclosing component and does not care about the
// option, while the attribute half reads the option and requires no component at all. That
// asymmetry is measured rather than inferred. A bare `<div ref="hello" />` at the top level of a
// file, inside no component of any kind, reports on the release binary, and `this.refs` in a plain
// function does not.
//
// Reporting both under one id would have passed a fixture set asserting counts, since every
// upstream input that reports twice reports once under each. The ids are what separate them.
//
// # The file gate is real, and it is the one thing that would have over-reported everywhere
//
// oxc declares `should_run` on `source_type().is_jsx()`, which reads like an optimization: a file
// with no JSX has no `JSXAttribute` to visit. It is not an optimization for the other half.
// `this.refs` is ordinary member access that appears in plain TypeScript, so without the gate this
// rule would report inside every `.ts` file holding a class that extends `React.Component`.
//
// Measured rather than reasoned about, because the reasoning could have gone either way: the same
// four-line class was written to `probe.ts` and `probe.tsx` and linted by the release oxlint binary
// in one invocation. The `.tsx` copy reported and the `.ts` copy did not. Our harness has no
// source-type predicate on `rule.Context`, so the gate is spelled here as the file suffix, which is
// what decides the parser's script kind in both the harness and the real run.
//
// # What counts as `this.refs`, which is wider than the dotted spelling
//
// oxc matches any member expression whose object is `this` and whose `static_property_name()`
// answers `refs`, and that resolves a computed key as well as a dotted one. So `this["refs"]`
// reports, and so does “this[`refs`]” with a single-quasi template, while “this[`refs${x}`]” does
// not. All three were run against the release binary and behave exactly that way. Our AST splits
// what oxc calls one member expression into `PropertyAccessExpression` and
// `ElementAccessExpression`, so both kinds are listened for and the computed one re-implements the
// same static-name resolution.
//
// The `refs` read is what reports, not the property taken off it: upstream underlines nine
// characters for `this.refs.hello`, so the finding points at `this.refs` and the outer access is
// left alone. A bare `this.refs` with nothing taken off it reports on its own.
//
// # The two template shapes, which is where this family of rules has gone wrong before
//
// A template ref has two node kinds in our AST and only one in oxc's. “ref={`hello`}” parses as a
// `NoSubstitutionTemplateLiteral` and “ref={`hello${index}`}” as a `TemplateExpression`, where oxc
// sees `JSXExpression::TemplateLiteral` for both. Upstream's corpus fails under the option on both
// shapes, so matching only the first would drop a fixture, and matching only the second would drop
// two. Both are listed below for that reason.
//
// This is also the only place the option changes an answer. With it off, both template shapes are
// clean and upstream ships each as a passing case.
var NoStringRefs = rule.Rule{
	// No namespace prefix. The config writes `react/no-string-refs` and the parity guard strips the
	// namespace on a `/` boundary, so `react-no-string-refs` would match no inventory entry and lint
	// no files while passing every fixture in this package.
	Name: "no-string-refs",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		// A rule offered a file it does not judge should register nothing rather than register
		// listeners that decline every node, so the gate is here and not inside each listener.
		if !isJsxFileName(ctx.SourceFile.FileName()) {
			return nil
		}

		// An unconfigured rule gets the zero value, which is the upstream default of both sources:
		// template refs are not reported unless asked for.
		settings, _ := options.(NoStringRefsOptions)

		reportRefsAccess := func(node *ast.Node, object *ast.Node, propertyName string, named bool) {
			if !named || propertyName != "refs" {
				return
			}
			if object == nil || object.Kind != ast.KindThisKeyword {
				return
			}
			// The component gate applies to this half only. Upstream's clean cases are three
			// different ways of writing `this.refs` outside a component, including two that put it
			// directly beside one, so this is the discrimination the passing corpus is mostly about.
			if utilsreact.EnclosingComponent(node) == nil {
				return
			}
			ctx.ReportNode(node, messageThisRefsDeprecated)
		}

		return rule.Listeners{
			ast.KindPropertyAccessExpression: func(node *ast.Node) {
				access := node.AsPropertyAccessExpression()
				name := access.Name()
				if name == nil {
					return
				}
				reportRefsAccess(node, access.Expression, name.Text(), true)
			},

			ast.KindElementAccessExpression: func(node *ast.Node) {
				access := node.AsElementAccessExpression()
				propertyName, named := staticElementAccessName(access.ArgumentExpression)
				reportRefsAccess(node, access.Expression, propertyName, named)
			},

			ast.KindJsxAttribute: func(node *ast.Node) {
				// AttributeName declines a namespaced name, matching oxc's destructuring of
				// `JSXAttributeName::Identifier`, which returns early on anything else. The
				// comparison is exact rather than case-insensitive: `<div REF="hello" />` is a
				// different attribute and is silent on the release binary.
				name, named := jsx.AttributeName(node)
				if !named || name != "ref" {
					return
				}
				if !isStringRefValue(node.AsJsxAttribute().Initializer, settings.NoTemplateLiterals) {
					return
				}
				// The whole attribute, not its name. Upstream underlines `ref="hello"`, eleven
				// characters from the `r`, which is `attr.span` rather than the name span that
				// `no-children-prop` in this same package reports.
				ctx.ReportNode(node, messageStringInRefDeprecated)
			},
		}
	},
}

// isJsxFileName reports whether a file name is one the parser reads as JSX.
//
// This stands in for oxc's `source_type().is_jsx()`, which our `rule.Context` does not expose. The
// suffix is what decides the script kind in the harness and in a real run alike, so asking it here
// gives the same answer by the same route rather than by a parallel one.
func isJsxFileName(fileName string) bool {
	normalizedPath := strings.ReplaceAll(fileName, `\`, "/")
	return strings.HasSuffix(normalizedPath, ".tsx") || strings.HasSuffix(normalizedPath, ".jsx")
}

// staticElementAccessName returns the property name a computed access names without evaluation.
//
// This is oxc's `ComputedMemberExpression::static_property_name` at our AST's spelling, narrowed to
// the kinds that can answer `refs`. Upstream also answers for a regular-expression literal, which is
// absent here deliberately rather than by oversight: the result is compared against one fixed name
// and no regular expression renders as `refs`, so the branch could not change a verdict and no test
// could guard it.
//
// The template arm is the single-quasi case and nothing wider. oxc requires `quasis.len() == 1`,
// which is exactly what a `NoSubstitutionTemplateLiteral` is in our tree; an interpolated template
// is a `TemplateExpression` and falls through here, so “this[`refs${x}`]” is silent at both tools.
func staticElementAccessName(argument *ast.Node) (string, bool) {
	if argument == nil {
		return "", false
	}
	switch argument.Kind {
	case ast.KindStringLiteral,
		ast.KindNoSubstitutionTemplateLiteral:
		return argument.Text(), true
	}
	return "", false
}

// isStringRefValue reports whether a ref attribute's value is a string this rule refuses.
//
// The bare-attribute case is the one worth naming: `<div ref />` has no initializer at all, and
// both upstreams require a value before looking at it. It is silent here for the same reason.
func isStringRefValue(initializer *ast.Node, noTemplateLiterals bool) bool {
	if initializer == nil {
		return false
	}
	switch initializer.Kind {
	// `ref="hello"`, the attribute written without braces.
	case ast.KindStringLiteral:
		return true

	// `ref={...}`, where only some expressions count.
	case ast.KindJsxExpression:
		inner := initializer.AsJsxExpression().Expression
		if inner == nil {
			return false
		}
		switch inner.Kind {
		case ast.KindStringLiteral:
			return true
		// Both template shapes, and only under the option. See the type doc above: our AST splits
		// what oxc matches as one `JSXExpression::TemplateLiteral` into two kinds, and upstream's
		// corpus fails on both.
		case ast.KindNoSubstitutionTemplateLiteral, ast.KindTemplateExpression:
			return noTemplateLiterals
		}
	}
	return false
}
