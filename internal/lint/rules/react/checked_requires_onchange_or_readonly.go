package react

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/jsx"
	"github.com/system-inc/cohere/internal/lint/rule"
)

var (
	messageCheckedMissingProperty = rule.Message{
		Id: "missingProperty",
		Description: "This `input` is `checked` with no `onChange` and no `readOnly`. React treats " +
			"`checked` as the single source of truth for the control, so without a handler to move " +
			"that value the box refuses every click and looks broken rather than disabled. Add " +
			"`onChange` to make it interactive, or `readOnly` to say the frozen state is deliberate.",
	}
	messageCheckedExclusiveAttribute = rule.Message{
		Id: "exclusiveCheckedAttribute",
		Description: "This `input` writes both `checked` and `defaultChecked`. The first makes React " +
			"own the value and the second hands the initial value to the browser, so writing both " +
			"asks for a controlled and an uncontrolled input at once. React keeps `checked` and " +
			"ignores `defaultChecked`, which makes the discarded one read as live configuration.",
	}
)

// CheckedRequiresOnChangeOrReadOnlyOptions is the decoded option object.
//
// Two booleans, matching `meta.schema`'s two properties exactly. Both default to false upstream, so
// Go's zero value is already the right answer and no inversion is needed; see the decoder for why
// one is still hand-written.
type CheckedRequiresOnChangeOrReadOnlyOptions struct {
	// IgnoreMissingProperties silences the `missingProperty` arm and leaves the exclusive one.
	IgnoreMissingProperties bool `json:"ignoreMissingProperties"`

	// IgnoreExclusiveCheckedAttribute silences the `exclusiveCheckedAttribute` arm.
	IgnoreExclusiveCheckedAttribute bool `json:"ignoreExclusiveCheckedAttribute"`
}

// DecodeCheckedRequiresOnChangeOrReadOnlyOptions decodes the option object.
//
// Hand-written rather than `rule.DecodeOptionsInto` for one reason: the generic decoder ERRORS on
// empty input, and a rule configured as a bare `"error"` is handed exactly that. Upstream accepts
// it, since `Object.assign({}, defaultOptions, context.options[0])` with an undefined second source
// is just the defaults, so the empty path has to answer both-false rather than fail. Every default
// here IS the Go zero value, so nothing else needs translating.
func DecodeCheckedRequiresOnChangeOrReadOnlyOptions(raw []byte) (any, error) {
	var options CheckedRequiresOnChangeOrReadOnlyOptions
	if len(raw) == 0 {
		return options, nil
	}
	if err := rule.UnmarshalOptions(raw, &options); err != nil {
		return options, err
	}
	return options, nil
}

// checkedRequiresTargetProperties is the set upstream collects before deciding anything.
//
// Upstream builds a Set of the names present out of this fixed four and then asks the set three
// questions. Reproduced as a set rather than as three independent scans because the SET is what
// decides: a name written twice contributes once, which is why `<input checked defaultChecked
// defaultChecked />` reports exactly one exclusive finding rather than two. Measured on the
// installed build.
var checkedRequiresTargetProperties = map[string]bool{
	"checked":        true,
	"onChange":       true,
	"readOnly":       true,
	"defaultChecked": true,
}

// CheckedRequiresOnChangeOrReadOnly flags a checked `input` that can never change.
//
//	valid:   <input type="checkbox" checked onChange={noop} />
//	valid:   <input type="checkbox" checked readOnly />
//	valid:   <input type="checkbox" defaultChecked />
//	valid:   React.createElement('input', { checked: true, onChange: noop })
//	invalid: <input type="checkbox" checked />
//	invalid: <input type="checkbox" checked defaultChecked />
//	invalid: React.createElement("input", { checked: false })
//
// Ported from `react/checked-requires-onchange-or-readonly` in `eslint-plugin-react`, the
// implementation that defined it. Its 24 clean and 11 reporting cases were extracted mechanically
// from the clone by stubbing its `RuleTester` and serializing the captured object, then every one
// of the 35 was run against the installed build, 7.37.5, through the ESLint Linter API. The two
// authorities agreed on all 35, so there is no drift to record for this rule.
//
// # The two arms are independent and both can fire on one element
//
// `meta.messages` names two, and upstream asks them as two separate questions after one shared
// gate. `checked` present is the gate; then `defaultChecked` alongside it is the exclusive finding,
// and the absence of both `onChange` and `readOnly` is the missing finding. An element writing
// `checked defaultChecked` and nothing else reports BOTH, exclusive first, which is the order the
// corpus asserts and the order this rule reports in.
//
// # Why the name comparison is exact and the value is never read
//
// Upstream collects `prop.name.name` for JSX and `prop.key.name` for a call, and compares against a
// four-name set. `name` exists on an Identifier and on nothing else, so three shapes contribute
// NOTHING and each is a real thing somebody writes:
//
//	<input {...properties} checked />          a spread has no name
//	React.createElement('input', {'checked': true})    a string-literal key has no `.name`
//	React.createElement('input', {['checked']: true})  a computed key has no `.name` either
//
// All three measured silent on the installed build, and the last two are the surprising ones: the
// property is unmistakably `checked` to a reader and invisible to this rule. Reproduced rather than
// improved, because widening it would report code upstream passes and no imported fixture could see
// the difference. Case is exact too, so `<input CHECKED />` and `{ readonly: true }` are both
// silent, both measured.
//
// The VALUE is never consulted. `checked={false}` reports exactly like `checked`, which reads wrong
// until you see that an input pinned to false is as stuck as one pinned to true. Upstream's own
// corpus asserts it twice.
//
// # `createElement` is resolved, not pattern-matched, and this is why the rule needs the checker
//
// Upstream's `isCreateElement` accepts two spellings and the second one needs scope analysis:
// `<pragma>.createElement(...)`, where the object name must equal the pragma, and a BARE
// `createElement(...)` only when that name is destructured from a pragma import. Measured against
// the installed build, the difference is load-bearing in both directions:
//
//	React.createElement('input', {checked: 1})                       reports
//	createElement('input', {checked: 1})                             SILENT, nothing declares it
//	import {createElement} from 'react'; createElement(...)          reports
//	import {createElement} from 'preact'; createElement(...)         SILENT, wrong module
//	const {createElement} = React; createElement(...)                reports
//	const createElement = React.createElement; createElement(...)    reports
//	const {createElement} = require('react'); createElement(...)     reports
//	Preact.createElement('input', {checked: 1})                      SILENT, object is not the pragma
//	React['createElement']('input', {checked: 1})                    SILENT, no `property.name`
//	document.createElement(...)                                      SILENT, object is not the pragma
//
// The shelf's `react.IsCreateElementCall` answers TRUE on four of those silent rows. It accepts any
// object, accepts a bare call with no import at all, and accepts a computed member, so this rule
// does not call it. That helper matches `no-children-prop`, whose upstream asks the question a
// different way, and its own doc comment says a rule reading its callee inline upstream should read
// it inline here. This is that rule.
//
// The pragma is fixed at `React` here. Upstream reads it from a `@jsx` comment or from
// `settings.react.pragma`, and cohere reads no React settings, so the second is unreachable by any
// route. The first is reachable and is NOT implemented; see `pragmaFromJsxComment` below for the
// measurement and the reasoning.
var CheckedRequiresOnChangeOrReadOnly = rule.Rule{
	Name:             "react/checked-requires-onchange-or-readonly",
	NeedsTypeChecker: true,
	TypeReach:        rule.TypeReachShapes,
	Run: func(ctx rule.Context, options any) rule.Listeners {
		settings, _ := rule.OptionsAs[CheckedRequiresOnChangeOrReadOnlyOptions](options)

		report := func(node *ast.Node, present map[string]bool) {
			if !present["checked"] {
				return
			}
			if !settings.IgnoreExclusiveCheckedAttribute && present["defaultChecked"] {
				ctx.ReportNode(node, messageCheckedExclusiveAttribute)
			}
			if !settings.IgnoreMissingProperties && !present["onChange"] && !present["readOnly"] {
				ctx.ReportNode(node, messageCheckedMissingProperty)
			}
		}

		checkElement := func(node *ast.Node) {
			tagName, attributes := jsx.ElementParts(node)
			if !jsx.IsIntrinsicElementNamed(tagName, "input") {
				return
			}
			present := map[string]bool{}
			if attributes != nil && attributes.Kind == ast.KindJsxAttributes {
				properties := attributes.AsJsxAttributes().Properties
				if properties != nil {
					for _, property := range properties.Nodes {
						name, named := jsx.AttributeName(property)
						if named && checkedRequiresTargetProperties[name] {
							present[name] = true
						}
					}
				}
			}
			report(node, present)
		}

		return rule.Listeners{
			ast.KindJsxOpeningElement:     checkElement,
			ast.KindJsxSelfClosingElement: checkElement,

			ast.KindCallExpression: func(node *ast.Node) {
				if ctx.TypeChecker == nil {
					return
				}
				if !isPragmaCreateElementCall(ctx, node) {
					return
				}
				arguments := node.AsCallExpression().Arguments
				if arguments == nil || len(arguments.Nodes) < 2 {
					return
				}
				// Upstream requires the first argument to be a Literal whose value is exactly the
				// string `input`. A template literal is not a Literal in its AST, and
				// `React.createElement(`input`, ...)` measured silent on the installed build, so the
				// kind test here is the same discrimination rather than a narrowing.
				first := arguments.Nodes[0]
				if first.Kind != ast.KindStringLiteral || first.Text() != "input" {
					return
				}
				second := arguments.Nodes[1]
				if second.Kind != ast.KindObjectLiteralExpression {
					return
				}

				present := map[string]bool{}
				for _, property := range second.AsObjectLiteralExpression().Properties.Nodes {
					name := propertyIdentifierName(property)
					if name != "" && checkedRequiresTargetProperties[name] {
						present[name] = true
					}
				}
				report(node, present)
			},
		}
	},
}

// propertyIdentifierName returns an object-literal member's key when that key is a plain identifier.
//
// Upstream reads `prop.key.name`, which is defined on an Identifier and undefined on everything
// else, and then tests set membership, so `undefined` simply never matches. The empty string is
// the same non-answer here, and it is safe as a sentinel because no member of the target set is
// empty.
//
// A spread element has no `key` at all and lands here as the empty string, which is why
// `React.createElement('input', {checked: 1, ...rest})` still reports: the spread contributes
// nothing and the literal `checked` still gates. Measured on the installed build.
func propertyIdentifierName(property *ast.Node) string {
	name := property.Name()
	if name == nil || name.Kind != ast.KindIdentifier {
		return ""
	}
	return name.Text()
}

// isPragmaCreateElementCall reproduces upstream's `isCreateElement` for the fixed `React` pragma.
//
// Two accepting shapes, matching the reference implementation branch for branch.
//
// The namespaced one is purely syntactic: the callee is a property access whose property is the
// IDENTIFIER `createElement` and whose object is the identifier `React`. Upstream reads
// `callee.property.name`, so a computed member has no name and declines, and it reads
// `callee.object.name`, so `Preact.` and `document.` and `a.b.createElement` all decline on the same
// line. All four measured silent.
//
// The bare one is where the checker earns its declaration: `createElement(...)` counts only when
// that name resolves to a binding introduced by the pragma module. Upstream answers this with
// `eslint-scope` and four accepted definition shapes; each is reproduced in
// `rule.IsReactPragmaBinding` against the declaration the checker hands back.
func isPragmaCreateElementCall(ctx rule.Context, node *ast.Node) bool {
	callee := node.AsCallExpression().Expression
	if callee == nil {
		return false
	}

	// Upstream sees no parenthesis nodes at all, because its parser folds them away, so
	// `(React.createElement)(...)` reaches its member branch and reports. Ours preserves them, and
	// skipping is what reproduces upstream rather than what improves on it. Measured: that input
	// reports on the installed build. `SkipParentheses` dereferences its argument, which is why the
	// nil check above it is not optional.
	callee = ast.SkipParentheses(callee)
	if callee == nil {
		return false
	}

	switch callee.Kind {
	case ast.KindPropertyAccessExpression:
		access := callee.AsPropertyAccessExpression()
		name := access.Name()
		if name == nil || name.Kind != ast.KindIdentifier || name.Text() != "createElement" {
			return false
		}
		object := access.Expression
		return object != nil && object.Kind == ast.KindIdentifier && object.Text() == reactPragmaName

	case ast.KindIdentifier:
		if callee.Text() != "createElement" {
			return false
		}
		return rule.IsReactPragmaBinding(ctx.SourceFile, ctx.TypeChecker, ctx.FileCache, callee)
	}

	return false
}

// reactPragmaName is the object name the namespaced spelling is compared against.
//
// Fixed rather than configurable, because both of upstream's sources for it are out of reach or
// deliberately declined. `settings.react.pragma` has no counterpart in `internal/config` at all. A
// `/** @jsx Foo.bar */` comment IS readable here through `comments.ForFile`, and is not read, which
// is a stated divergence rather than an oversight: implementing it would mean scanning every
// comment in every file to answer a question that changes the verdict only for a file that opts out
// of the automatic runtime by hand, and the ahra tree contains no such comment. The cost is a false
// NEGATIVE on such a file, which is the safe direction, and the next reader adding it should add a
// fixture for `/** @jsx Preact.h */ Preact.createElement('input', {checked: 1})`, which reports
// upstream and is measured silent here.
const reactPragmaName = "React"
