package react

import (
	"regexp"
	"strings"
	"sync"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// NoUnstableNestedComponentsOptions configures the rule.
//
// Upstream's schema is a single object with three keys. Our config layer unwraps the severity tuple
// before dispatch, so the decoder receives that object directly rather than upstream's one-element
// array.
type NoUnstableNestedComponentsOptions struct {
	// AllowAsProperties permits a component defined inside a prop. Absent means false.
	AllowAsProperties bool `json:"allowAsProps"`

	// PropNamePattern is the glob a prop name is matched against to be treated as a render prop.
	// Absent means `render*`, which is upstream's default.
	PropNamePattern string `json:"propNamePattern"`

	// CustomValidators is in upstream's schema and is read by its prop-types machinery rather than
	// by this rule. It is accepted so a configuration carrying it decodes instead of erroring, and
	// it is deliberately unused: no branch in upstream's `validate` consults it.
	CustomValidators []string `json:"customValidators"`
}

// DefaultNoUnstableNestedComponentsOptions is the unconfigured answer.
//
// `propNamePattern` defaults to `render*` through upstream's
// `(context.options[0] || {}).propNamePattern || 'render*'`, so an absent key and an explicit empty
// string both land on `render*` there. The empty string is falsy in JavaScript, which is a
// distinction Go does not make for free and which the decoder below reproduces deliberately.
// The pattern's default is defended at three sites: this literal, the decoder's fold of an empty
// string, and Run's fallback for nil options. A mutation emptying any one of them survives the
// sweep, because the other two still produce `render*`. Mutating all three together fails 12 lines,
// which is what establishes the fixtures can see the default at all.
//
// That redundancy is deliberate rather than accidental: the three sites cover three different
// arrival paths, a configured value, an explicitly empty configured value, and no configuration at
// all, and the brief records that a default-inversion reaching the rule is invisible to fixtures
// built from a struct. Recorded here because a single-site survivor reads as a fixture gap and is
// not one.
func DefaultNoUnstableNestedComponentsOptions() NoUnstableNestedComponentsOptions {
	return NoUnstableNestedComponentsOptions{
		AllowAsProperties: false,
		PropNamePattern:   "render*",
	}
}

// DecodeNoUnstableNestedComponentsOptions reads this rule's configuration from the config layer.
//
// Hand-rolled rather than `rule.DecodeOptionsInto` because the pattern's default is a non-zero
// value. The generic helper would leave `PropNamePattern` empty on a bare `"error"` configuration,
// and an empty pattern matches only the empty string, so every render prop would stop being
// recognised and the rule would report where upstream is silent. That is the default-inversion trap
// the brief names, arriving through a string rather than through a bool.
func DecodeNoUnstableNestedComponentsOptions(raw []byte) (NoUnstableNestedComponentsOptions, error) {
	options := DefaultNoUnstableNestedComponentsOptions()
	if len(raw) == 0 {
		return options, nil
	}
	// Decoded into a fresh value so an absent key is distinguishable from an explicit empty string,
	// then folded onto the defaults the way upstream's `||` folds a falsy value.
	var wire NoUnstableNestedComponentsOptions
	if unmarshalOptionsError := rule.UnmarshalOptions(raw, &wire); unmarshalOptionsError != nil {
		return options, unmarshalOptionsError
	}
	options.AllowAsProperties = wire.AllowAsProperties
	options.CustomValidators = wire.CustomValidators
	if wire.PropNamePattern != "" {
		options.PropNamePattern = wire.PropNamePattern
	}
	return options, nil
}

// componentAsPropsInfo is appended to the message when the component sits in a prop and the option
// that would allow it is off. Copied byte for byte from upstream's constant of the same name.
const componentAsPropsInfo = " If you want to allow component creation in props, set allowAsProps option to true."

// NoUnstableNestedComponents keeps a component from being defined inside another component's render.
//
//	valid:   a component defined at module scope and used inside another
//	valid:   a component created by `useCallback` inside a parent, which upstream calls a
//	         known false negative rather than an exemption
//	valid:   `<Component renderFooter={() => <div />} />`, a render prop by name
//	valid:   `items.map((item) => <li />)`, which is a callback rather than a component
//	invalid: `function Parent() { function Nested() { return <div />; } ... }`
//	invalid: `function Parent() { const Nested = () => <div />; ... }`
//	invalid: `<Component footer={() => <div />} />` unless `allowAsProps` is set
//
// React reconciles by component type, so a component whose identity is recreated on every parent
// render is a different type every time. React unmounts the old subtree, discards its state and its
// DOM nodes, and mounts a new one. The finding is therefore about where a function is defined
// relative to a component boundary rather than about what it contains.
//
// # This rule is a filter over a detection pass that already exists in this package
//
// Upstream is `Components.detect(...)`, which merges its own detection visitors ahead of the rule's
// and hands the rule a populated component registry. Every finding comes from asking whether a node
// is a component and whether an ancestor is one; the rule's own body is a list of exceptions.
//
// `no-multi-comp` already ports that detection pass as `collectDetectedComponents` and
// `statelessComponentFor`, against the same authority and the same version. This rule reaches for
// them rather than deriving component-hood a second time, for the reason the brief gives about
// extension rules: two implementations of one question are two chances to disagree about it.
//
// # The scope walk is an AST walk here, measured rather than assumed
//
// Upstream's `getParentStatelessComponent` walks `scope.upper` and calls `getStatelessComponent` on
// each `scope.block`. We have no scope analysis, and the natural substitute is the chain of
// enclosing function-like AST ancestors. Those are different objects and the substitution had to be
// measured rather than argued.
//
// Measured on 2026-08-27 by instrumenting the installed 7.37.5 build and capturing both chains at
// every call the corpus produces: 484 calls, and after collapsing the consecutive duplicates ESLint
// emits for a function's own parameter scope, the two sequences agree on all 484. Controlled by
// removing the function kinds from the AST side, which took the disagreement count to 432, so the
// comparison can distinguish the two rather than passing vacuously.
//
// # Parentheses
//
// Our parser keeps `KindParenthesizedExpression`, which estree folds away, so an ancestry walk sees
// a node upstream never sees. Every walk here goes through `semanticParentOf`, which `no-multi-comp`
// already defines to skip them. Measured against the installed build: `<C footer={(() => <div />)} />`
// reports upstream, and without the skip the port was silent on it.
//
// # No fix
//
// Upstream ships none, and there is none available: moving the component out means choosing where
// it goes and rewriting every value it closed over into a prop. That is a design decision rather
// than a repair.
var NoUnstableNestedComponents = rule.Rule[NoUnstableNestedComponentsOptions]{
	Name: "react/no-unstable-nested-components",

	// Reached through this package's `isPragmaCreateElementCall`, which `returnsJsx` consults to
	// decide whether a bare `createElement(...)` names the React function. Upstream answers the
	// same question with scope analysis over the file's imports.
	NeedsTypeChecker: true,
	TypeReach:        rule.TypeReachShapes,

	Run: func(ruleContext rule.Context, options rule.Given[NoUnstableNestedComponentsOptions]) rule.Listeners {
		settings, ok := options.Read()
		if !ok {
			// A rule configured as a bare `"error"` is handed nil options, which is not this type.
			// Without this the zero value would apply and the empty pattern would match nothing.
			settings = DefaultNoUnstableNestedComponentsOptions()
		}
		if settings.PropNamePattern == "" {
			settings.PropNamePattern = "render*"
		}
		propertyNameMatches := compilePropNamePattern(settings.PropNamePattern)

		return rule.Listeners{
			ast.KindSourceFile: func(sourceFile *ast.Node) {
				// The detection pass runs once over the whole file before any judgment, which is
				// what upstream's merged visitors achieve by ordering. Doing it here rather than
				// per node also means an ancestor's component-hood is known when a descendant is
				// judged, which a pre-order walk cannot otherwise guarantee for the shapes where
				// the ancestor is a wrapping call.
				detected := collectDetectedComponents(ruleContext, sourceFile)
				isComponent := make(map[*ast.Node]bool, len(detected))
				for _, component := range detected {
					isComponent[component.node] = true
				}

				state := &unstableNestedState{
					ruleContext:         ruleContext,
					isComponent:         isComponent,
					allowAsProperties:   settings.AllowAsProperties,
					propertyNameMatches: propertyNameMatches,
				}

				var visit func(*ast.Node)
				visit = func(node *ast.Node) {
					switch node.Kind {
					case ast.KindFunctionDeclaration, ast.KindArrowFunction,
						ast.KindFunctionExpression, ast.KindClassDeclaration,
						ast.KindCallExpression:
						state.validate(node)
					}
					node.ForEachChild(func(child *ast.Node) bool {
						visit(child)
						return false
					})
				}
				visit(sourceFile)
			},
		}
	},
}

// unstableNestedMessage reproduces upstream's `generateErrorMessageWithParentName`.
//
// The text is copied byte for byte, including the curly quotes around the parent name and the
// typographic apostrophe in "subtree’s", because the corpus asserts the whole string. The spacing
// around the name is upstream's own: a known parent contributes ` “Name” ` with spaces on both
// sides, and an unknown one contributes a single space, so the two branches differ by more than the
// name itself.
func unstableNestedMessage(parentName string) string {
	name := " "
	if parentName != "" {
		name = " “" + parentName + "” "
	}
	return "Do not define components during render. React will see a new component type on every " +
		"render and destroy the entire subtree’s DOM nodes and state " +
		"(https://reactjs.org/docs/reconciliation.html#elements-of-different-types). Instead, move " +
		"this component definition out of the parent component" + name + "and pass data as props."
}

// closestMatchingAncestor is upstream's `getClosestMatchingParent` with an arbitrary matcher.
//
// Same two properties as above: starts at the parent, stops below the file.
func closestMatchingAncestor(node *ast.Node, matches func(*ast.Node) bool) *ast.Node {
	for current := semanticParentOf(node); current != nil; current = semanticParentOf(current) {
		if current.Kind == ast.KindSourceFile {
			return nil
		}
		if matches(current) {
			return current
		}
	}
	return nil
}

// resolveNestedComponentName is upstream's `resolveComponentName`.
//
// Upstream reads `node.id.name` first, which covers a function declaration and a named function
// expression, and falls back to `node.parent.id.name` for an arrow, which covers
// `const Parent = () => ...`. A class declaration's name arrives through the first branch here
// because our parser puts it in the same `Name()` position.
func resolveNestedComponentName(node *ast.Node) string {
	if node == nil {
		return ""
	}

	switch node.Kind {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindClassDeclaration,
		ast.KindClassExpression:
		if name := node.Name(); name != nil && name.Kind == ast.KindIdentifier {
			return name.Text()
		}
	}

	// Upstream's fallback is guarded on `ArrowFunctionExpression` specifically, so a function
	// expression assigned to a const does not take its name from the binding. Measured against the
	// installed build on 2026-08-27: `const Parent = function () { const N = () => <div/>; return
	// <N/>; }` reports with the no-name wording, while the same file written with an arrow parent
	// names `Parent`. Reproducing the narrower guard rather than the more useful one, because the
	// message text is asserted.
	if node.Kind == ast.KindArrowFunction {
		if parent := semanticParentOf(node); parent != nil &&
			parent.Kind == ast.KindVariableDeclaration {
			if name := parent.AsVariableDeclaration().Name(); name != nil &&
				name.Kind == ast.KindIdentifier {
				return name.Text()
			}
		}
	}
	return ""
}

// startsUppercase is upstream's `parentName[0] === parentName[0].toLowerCase()`, inverted.
//
// Deliberately not `hasComponentCapitalization`, which is upstream's `isFirstLetterCapitalized` and
// strips leading underscores before testing. This site does neither: it indexes character zero and
// compares it against its own lowercase, so `_Foo` is a lowercase parent here and a capitalized
// name there. Measured against the installed build on 2026-08-27: a nested component inside
// `function _Parent()` is silent, and inside `function Parent()` reports, while `no-multi-comp`
// counts `_Foo` as a component. Two predicates in one plugin that look interchangeable and are not.
func startsUppercase(name string) bool {
	if name == "" {
		return false
	}
	first := []rune(name)[0]
	return string(first) != strings.ToLower(string(first))
}

// isMapCallExpression is upstream's `isMapCall`.
//
// Upstream reads `node.callee.property.name === 'map'` with no check that the node is a call at
// all, relying on the undefined chain to answer false. It does not resolve the receiver, so
// `anything.map(...)` counts and a bare `map(...)` does not.
func isMapCallExpression(node *ast.Node) bool {
	if node == nil || node.Kind != ast.KindCallExpression {
		return false
	}
	callee := skipParenthesesOptional(node.AsCallExpression().Expression)
	if callee == nil || callee.Kind != ast.KindPropertyAccessExpression {
		return false
	}
	name := callee.AsPropertyAccessExpression().Name()
	return name != nil && name.Kind == ast.KindIdentifier && name.Text() == "map"
}

// hookNamePattern is upstream's `HOOK_REGEXP`, copied verbatim.
//
// Note it requires a character after the digit or capital, so bare `use` and `useX` differ: `useX`
// matches through `.*` being allowed to be empty, and `use` alone does not match at all.
var hookNamePattern = regexp.MustCompile(`^use[A-Z0-9].*$`)

// isJsxAttributeOfExpressionContainer is upstream's matcher of the same name.
//
// The value must be an expression container, so `<C foo="bar" />` does not match. Reading the value
// through the typed accessor rather than through `Text()` matters: the brief records that
// `Node.Text()` panics on several shapes, and a JSX attribute value can be a string literal, an
// expression container, or absent entirely for `<C flag />`. A bare attribute has no value node at
// all, so this is a nil check as much as a kind check.
//
// # The container half is a live survivor, recorded rather than papered over
//
// A mutation accepting any non-nil initializer survives. Instrumented over all 77 corpus cases: 37
// JSX attributes reach this predicate and every one of them carries an expression container, so
// nothing imported can see the kind test.
//
// Two rounds of fixtures failed to close it and each taught something. The first put a nested
// component in the parent's body beside a string-valued attribute; that component's ancestry is
// Block then FunctionDeclaration then SourceFile, so it never walks near an attribute and the
// predicate is not consulted. The second tried to make the walk climb past a string-valued
// attribute toward an outer container attribute, which is what the guard would really bound, and
// both such shapes are silent upstream for an unrelated reason:
//
//	<Outer footer={() => <div />} />                      reports, with the as-props note
//	<Outer footer={<Inner title="x" render={...} />} />   silent
//	<Outer footer={<Inner title="x">{...}</Inner>} />     silent
//
// So there is no measured input where the kind test changes a verdict. This is not filed as an
// equivalence verdict: I could not name such an input, which is a different claim from showing none
// exists. The check is kept because it is upstream's own condition and because the nil half of it is
// load bearing regardless.
func isJsxAttributeOfExpressionContainer(node *ast.Node) bool {
	if node == nil || node.Kind != ast.KindJsxAttribute {
		return false
	}
	initializer := node.AsJsxAttribute().Initializer
	return initializer != nil && initializer.Kind == ast.KindJsxExpression
}

// propertyNamePatternMatchers holds compilePropNamePattern's matcher for each pattern seen.
var propertyNamePatternMatchers sync.Map

// compilePropNamePattern turns upstream's minimatch pattern into a matcher.
//
// Upstream calls `minimatch(text, pattern)` on a prop name. minimatch is a PATH matcher and its
// extra machinery is all about paths: `/` separators, a leading dot being hidden, brace expansion,
// extended globs. A prop name is a JavaScript identifier, so none of that alphabet can appear.
//
// Measured on 2026-08-27 against minimatch 3.1.5, the version the installed plugin resolves:
// 16,854 comparisons over six patterns, including `render*`, `*Renderer` and `*`, against every
// string up to four characters over the identifier alphabet plus the corpus's own names. Zero
// disagreements. Controlled by disabling the star expansion, which took the disagreement count to
// 2,815, so the comparison can tell the two apart.
//
// The measurement is confined to identifier-shaped names, and that is exactly the input this rule
// feeds it: an object key read as `KindIdentifier` and a JSX attribute name read as
// `KindIdentifier`. A computed key or a namespaced attribute never reaches here, because both are
// declined by kind at the call site rather than being stringified.
//
// `Run` is called once per file, so compiling here compiled the same pattern once per file: 179K
// objects on a cold ahra run. A matcher depends only on its pattern, so each is compiled once and
// kept in propNamePatternMatchers. Keyed by the pattern rather than held in one variable, because a
// run can lint under more than one configuration.
func compilePropNamePattern(pattern string) func(string) bool {
	stored, found := propertyNamePatternMatchers.Load(pattern)
	if !found {
		stored, _ = propertyNamePatternMatchers.LoadOrStore(pattern, buildPropNamePattern(pattern))
	}
	matcher, isMatcher := stored.(func(string) bool)
	if !isMatcher {
		// Invariant: this function is the only writer to propNamePatternMatchers, and it stores only
		// what buildPropNamePattern returns.
		panic("react: propNamePatternMatchers held something other than a matcher")
	}
	return matcher
}

// buildPropNamePattern compiles one pattern, for compilePropNamePattern to keep.
func buildPropNamePattern(pattern string) func(string) bool {
	var builder strings.Builder
	builder.WriteString("^")
	for _, character := range pattern {
		switch character {
		case '*':
			builder.WriteString(".*")
		case '?':
			builder.WriteString(".")
		default:
			builder.WriteString(regexp.QuoteMeta(string(character)))
		}
	}
	builder.WriteString("$")

	compiled, compileError := regexp.Compile(builder.String())
	if compileError != nil {
		// Unreachable for any pattern this builder produces: every literal is escaped and the two
		// metacharacters expand to valid fragments. Declining rather than panicking, because a rule
		// that takes the run down over a configuration string is worse than one that stops matching
		// render props.
		return func(string) bool { return false }
	}
	return compiled.MatchString
}
