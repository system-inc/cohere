package react

import (
	"fmt"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/rule"
	"github.com/system-inc/cohere/internal/utilities/jsx"
)

// iframeMissingSandboxAllowedValues is upstream's ALLOWED_VALUES, copied in order.
//
// The empty string is a member, and it is not padding: upstream splits the attribute on a single
// space and trims each piece, so `sandbox=" allow-forms "` yields an empty first and last token,
// and both are accepted only because the empty string is in this list. Corpus case
// `<iframe sandbox=" allow-forms __unknown__ allow-popups __unknown__  "/>` reports exactly twice
// rather than four times for that reason, and it is the one case in the corpus that pins it.
//
// Two entries look redundant and are not. `allow-downloads-without-user-activation` was the
// original spelling and `allow-downloads` replaced it, and upstream carries both, so a project
// written against either browser generation stays clean.
var iframeMissingSandboxAllowedValues = map[string]bool{
	"": true,
	"allow-downloads-without-user-activation": true,
	"allow-downloads":                         true,
	"allow-forms":                             true,
	"allow-modals":                            true,
	"allow-orientation-lock":                  true,
	"allow-pointer-lock":                      true,
	"allow-popups":                            true,
	"allow-popups-to-escape-sandbox":          true,
	"allow-presentation":                      true,
	"allow-same-origin":                       true,
	"allow-scripts":                           true,
	"allow-storage-access-by-user-activation": true,
	"allow-top-navigation":                    true,
	"allow-top-navigation-by-user-activation": true,
}

var (
	messageIframeMissingSandboxAttributeMissing = rule.Message{
		Id: "attributeMissing",
		Description: "This `iframe` has no `sandbox` attribute, so the page it embeds runs with the " +
			"full privileges of a top-level document: it can run scripts against its own origin, " +
			"submit forms, open popups, and navigate the page that framed it. Third-party embeds " +
			"are the common case and the one that matters, because the framing page has no " +
			"control over what the other origin ships tomorrow. Add `sandbox` with the narrowest " +
			"set of `allow-` tokens the embed actually needs; a bare `sandbox` denies everything.",
	}
	messageIframeMissingSandboxInvalidCombination = rule.Message{
		Id: "invalidCombination",
		Description: "This `sandbox` lists both `allow-scripts` and `allow-same-origin`, which " +
			"together undo the sandbox rather than narrowing it. With both set the framed document " +
			"runs script against its real origin, so it can reach into its own storage and cookies " +
			"and remove the `sandbox` attribute from itself, escaping for every load after the " +
			"first. Drop one of the two: keep `allow-scripts` for an untrusted embed, or " +
			"`allow-same-origin` for one that needs its own origin but no script.",
	}
)

// iframeMissingSandboxInvalidValue builds the per-finding message naming the token it rejected.
//
// The Id is fixed and only the Description moves, which is what keeps `ExpectFindings` able to
// count these while the rendered text still names the offending token the way upstream's
// `{{ value }}` interpolation does.
func iframeMissingSandboxInvalidValue(value string) rule.Message {
	return rule.Message{
		Id: "invalidValue",
		Description: fmt.Sprintf(
			"`%s` is not a sandbox token, so the browser ignores it and the permission the author "+
				"meant to grant is simply absent. A misspelled token fails silently in the "+
				"direction that looks safe and is not: the embed loses a capability it needs and "+
				"the author reads the missing behaviour as a bug in the embed. Check it against "+
				"the `allow-` list for the sandbox attribute.",
			value,
		),
	}
}

// IframeMissingSandbox flags an `iframe` that embeds another document without sandboxing it.
//
//	valid:   <iframe sandbox="" />
//	valid:   <iframe src="foo.htm" sandbox></iframe>
//	valid:   <iframe sandbox="allow-forms allow-modals"></iframe>
//	valid:   <iframe sandbox={""} />
//	valid:   <div sandbox="__unknown__" />
//	valid:   React.createElement("iframe", { sandbox: "" })
//	valid:   React.createElement("iframe", { src: "foo.htm", sandbox: true })
//	invalid: <iframe></iframe>
//	invalid: <iframe sandbox="__unknown__"></iframe>
//	invalid: <iframe sandbox="allow-scripts allow-same-origin"></iframe>
//	invalid: React.createElement("iframe")
//	invalid: React.createElement("iframe", {})
//
// Ported from `react/iframe-missing-sandbox` in `eslint-plugin-react`, read from the clone at
// `lib/rules/iframe-missing-sandbox.js`. No options (`schema: []`), three messages, no fixer. The
// whole 45-case corpus was replayed against the installed build (7.37.5) through the ESLint Linter
// API before any code was written, and it agreed on all 45, counts and message ids alike. Every
// behaviour below that the corpus does not state was measured the same way, on inputs written for
// the question.
//
// # There is no file-suffix gate
//
// Three siblings in this package gate on `.tsx`/`.jsx`, which is oxc residue rather than upstream
// behaviour: this rule was ported from the authority, which has no such gate anywhere. A fixture
// writes the same reporting source under `.tsx` and `.ts` and asserts both report. `.jsx` and `.js`
// are not testable here: `rule_testing`'s tsconfig pins `"include": ["**/*.ts", "**/*.tsx"]`, so a
// JavaScript-suffixed fixture is not in the program at all and the typed harness fails building the
// type graph. That is a limit of the harness rather than of the rule, and `.ts` is the extension
// that separates the gated behaviour from the ungated one anyway.
//
// # The two arms and why the createElement one is narrower than the shelf helper
//
// `internal/utilities/react.IsCreateElementCall` accepts a bare `createElement(...)` with no
// import, accepts `Preact.createElement`, and accepts a computed `React['createElement']`.
// Upstream's `isCreateElement` accepts none of those three. Measured on the installed build, all
// three are silent, and the shelf helper's own doc comment says to check before reaching for it,
// which is the check. This rule uses this package's `isPragmaCreateElementCall` instead, already
// ported against the same authority for `checked-requires-onchange-or-readonly` and shared by four
// rules. That is where the checker declaration comes from: the bare-identifier branch resolves the
// name to a binding the `react` module introduced.
//
// # `sandbox` is matched exactly, and case matters
//
// Upstream compares `attribute.name.name === 'sandbox'`. `<iframe SANDBOX="" />` reports
// `attributeMissing`, measured, so the attribute match is `jsx.MatchExactly` rather than the
// case-insensitive matcher some siblings use. The tag name is matched the same way: `<IFRAME>` is a
// component reference to JSX, not the HTML element, and is silent.
//
// # Presence and readability are two different questions, which is why the shelf value reader is
// not used here
//
// `jsx.StringAttributeValue` returns `("", false)` both for an absent attribute and for one present
// with a non-literal value, and this rule has to tell those apart: `<iframe sandbox={x} />` is
// CLEAN upstream because the attribute is present, while a missing one reports. So the attribute
// loop is written here, setting a found flag on the name alone and validating only when the value
// is a plain string literal.
//
// Three value shapes are present-but-unvalidated and all three are measured clean:
//
//	<iframe sandbox />              no initializer at all
//	<iframe sandbox={""} />         an expression container, even holding a literal
//	<iframe sandbox={`allow-forms`} />   a template literal
//
// The expression-container one is upstream's own corpus case and it is the surprising one: the
// value IS a literal, and upstream still declines it because its `attribute.value.type` is
// `JSXExpressionContainer` rather than `Literal`. Reproduced rather than improved on.
//
// # A spread does not count as supplying the attribute
//
// `<iframe {...props} />` reports `attributeMissing` and `React.createElement("iframe", {...p})`
// does too, both measured. Upstream's loops test the member kind and a spread never matches, so a
// spread that plainly could supply `sandbox` is treated as absent. This is upstream reporting on
// code it cannot see into, and it is reproduced: the alternative silences the rule on any element
// that spreads.
//
// # Which object keys count
//
// The createElement arm reads `x.key.name`, which is defined only on an identifier key, so a
// quoted key declines and the whole call reports as missing:
//
//	React.createElement("iframe", {'sandbox': ''})      reports attributeMissing
//	React.createElement("iframe", {['sandbox']: ''})    reports attributeMissing
//	React.createElement("iframe", {sandbox})           CLEAN
//
// All three measured. The shorthand is the sharp one: it satisfies the name test, so the attribute
// counts as found, and its value is an identifier rather than a literal, so nothing is validated.
// Present and unreadable, exactly like `sandbox={x}` on the JSX side.
//
// An accessor or a method key counts too, and this cost a defect on the way here. The first draft
// filtered on the member kind before the name test, accepting only a property assignment and a
// shorthand, which reads as a faithful spelling of upstream's `x.type === 'Property'`. It is not:
// in ESTree a getter, a setter and a shorthand method are ALL `Property`, differing only in `kind`,
// and every one of them carries `key.name`. Parsed with espree to confirm, and measured on the
// installed build:
//
//	React.createElement("iframe", { get sandbox() { return ""; } })   CLEAN
//	React.createElement("iframe", { set sandbox(v) {} })              CLEAN
//	React.createElement("iframe", { sandbox() { return ""; } })       CLEAN
//
// Our parser gives those three `KindGetAccessor`, `KindSetAccessor` and `KindMethodDeclaration`, so
// the kind filter excluded them and the rule reported `attributeMissing` on all three where
// upstream is silent. No imported fixture could see it, because the corpus writes no accessor
// anywhere, and the mutation that exposed it read as a subsumed guard: deleting the filter changed
// nothing the fixtures asserted. The filter is gone, the name test does the whole job, and three
// fixtures pin it.
//
// # Only the second argument is read
//
//	React.createElement("iframe", {a:1}, {sandbox: ""})    reports, measured
//
// Children are children.
//
// # Truthiness gates validation, and a non-string passes through it
//
// Upstream validates only when `value.value` is truthy AND a string. So `{ sandbox: true }` is
// clean (truthy, not a string, corpus case), and so are `{ sandbox: null }`, `{ sandbox: false }`
// and `{ sandbox: 0 }` (falsy). `{ sandbox: 5 }` is clean on both counts. All measured. Here the
// same gate is a single test that the value is a string literal, plus the empty-string case, which
// is the one shape where truthiness and stringness disagree: `sandbox=""` is a string and falsy, so
// upstream skips validation for it, and skipping validation on the empty string reaches the same
// verdict as validating it, since `""` is in the allowed list. Both routes are clean and the corpus
// asserts it twice.
var IframeMissingSandbox = rule.Rule{
	Name: "react/iframe-missing-sandbox",

	// Declared for the bare-call branch of `isPragmaCreateElementCall`, which asks the checker
	// whether `createElement` binds to a `react` import. The namespaced `React.createElement`
	// branch is purely syntactic and needs nothing.
	NeedsTypeChecker: true,

	Run: func(ctx rule.Context, options any) rule.Listeners {
		// validateSandboxValue splits a sandbox attribute's text and reports each rejected token,
		// then the invalid pairing. Upstream reports every unknown token separately and the
		// combination once, in that order, which is what the two-finding corpus case pins.
		validateSandboxValue := func(reported *ast.Node, value string) {
			allowScripts := false
			allowSameOrigin := false
			// Upstream splits on a single space, not on runs of whitespace, so a double space
			// yields an empty token. That token is accepted only because the empty string is in
			// the allowed list, which is why the list carries it.
			for _, token := range strings.Split(value, " ") {
				token = strings.TrimSpace(token)
				if !iframeMissingSandboxAllowedValues[token] {
					ctx.ReportNode(reported, iframeMissingSandboxInvalidValue(token))
				}
				if token == "allow-scripts" {
					allowScripts = true
				}
				if token == "allow-same-origin" {
					allowSameOrigin = true
				}
			}
			if allowScripts && allowSameOrigin {
				ctx.ReportNode(reported, messageIframeMissingSandboxInvalidCombination)
			}
		}

		// checkJsxElement runs the attribute loop for either opening form.
		//
		// `reported` is the node a finding anchors on. Upstream reports on the JSXOpeningElement,
		// which is the tag and its attributes rather than the whole element, so a paired
		// `<iframe></iframe>` points at the opening tag alone.
		checkJsxElement := func(node *ast.Node) {
			tagName, attributes := jsx.ElementParts(node)
			if !jsx.IsIntrinsicElementNamed(tagName, "iframe") {
				return
			}

			sandboxFound := false
			if attributes != nil && attributes.Kind == ast.KindJsxAttributes {
				properties := attributes.AsJsxAttributes().Properties
				if properties != nil {
					for _, property := range properties.Nodes {
						attributeName, named := jsx.AttributeName(property)
						if !named || !jsx.MatchExactly(attributeName, "sandbox") {
							continue
						}
						sandboxFound = true
						// Upstream requires the value to be a `Literal` with a truthy value, so an
						// expression container declines even when it holds a literal, and a bare
						// `sandbox` with no initializer declines too. Both are present, so neither
						// clears `sandboxFound`.
						initializer := property.AsJsxAttribute().Initializer
						if initializer != nil && initializer.Kind == ast.KindStringLiteral {
							validateSandboxValue(node, initializer.Text())
						}
					}
				}
			}

			if !sandboxFound {
				ctx.ReportNode(node, messageIframeMissingSandboxAttributeMissing)
			}
		}

		return rule.Listeners{
			ast.KindJsxOpeningElement: checkJsxElement,

			// A self-closing `<iframe />` never produces a JsxOpeningElement, and it is the shape
			// most iframes are written in. Upstream's selector `JSXOpeningElement[name.name=...]`
			// matches both because its parser gives a self-closing element that node type; ours
			// does not, so both kinds are listened for.
			ast.KindJsxSelfClosingElement: checkJsxElement,

			ast.KindCallExpression: func(node *ast.Node) {
				if ctx.TypeChecker == nil {
					return
				}
				if !isPragmaCreateElementCall(ctx, node) {
					return
				}
				arguments := node.AsCallExpression().Arguments
				if arguments == nil || len(arguments.Nodes) == 0 {
					return
				}
				// Upstream tests `tag.type === 'Literal' && tag.value === 'iframe'`, so a template
				// literal and an identifier both decline. Measured silent for both.
				first := arguments.Nodes[0]
				if first.Kind != ast.KindStringLiteral || first.Text() != "iframe" {
					return
				}

				sandboxFound := false
				// Only the second argument is read; a third holding `sandbox` changes nothing.
				if len(arguments.Nodes) > 1 && arguments.Nodes[1].Kind == ast.KindObjectLiteralExpression {
					properties := arguments.Nodes[1].AsObjectLiteralExpression().Properties
					if properties != nil {
						for _, property := range properties.Nodes {
							// Upstream requires `x.type === 'Property'` and reads `x.key.name`,
							// and the two tests together are exactly what
							// `propertyIdentifierName` answers: a spread is a SpreadElement with
							// no key, and a quoted or computed key leaves `key.name` undefined.
							// Both come back as the empty string here, so the name test alone
							// declines all three. No kind filter is written above it, and that is
							// deliberate rather than an omission; see the accessor note below.
							if propertyIdentifierName(property) != "sandbox" {
								continue
							}
							sandboxFound = true
							// Only a plain `key: value` member carries an initializer to read.
							// A shorthand, a getter, a setter and a method all count as SUPPLYING
							// the attribute and have no literal value to validate, which is why
							// presence is set above this rather than inside it.
							if property.Kind == ast.KindPropertyAssignment {
								initializer := property.AsPropertyAssignment().Initializer
								if initializer != nil && initializer.Kind == ast.KindStringLiteral {
									validateSandboxValue(node, initializer.Text())
								}
							}
						}
					}
				}

				if !sandboxFound {
					ctx.ReportNode(node, messageIframeMissingSandboxAttributeMissing)
				}
			},
		}
	},
}
