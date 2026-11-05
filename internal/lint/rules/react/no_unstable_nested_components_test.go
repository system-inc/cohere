package react

import (
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// upstreamMessage builds the exact text upstream produces, so the assertions below compare against
// a measured string rather than a paraphrase. Copied from the installed 7.37.5 build's output on
// 2026-08-27 rather than retyped from the rule source.
func upstreamMessage(parentName string, asProps bool) string {
	name := " "
	if parentName != "" {
		name = " “" + parentName + "” "
	}
	message := "Do not define components during render. React will see a new component type on " +
		"every render and destroy the entire subtree’s DOM nodes and state " +
		"(https://reactjs.org/docs/reconciliation.html#elements-of-different-types). Instead, " +
		"move this component definition out of the parent component" + name + "and pass data as props."
	if asProps {
		message += " If you want to allow component creation in props, set allowAsProps option to true."
	}
	return message
}

// TestNoUnstableNestedComponentsSpansAndMessages asserts where each finding points and what it
// says, neither of which ExpectFindings can see.
//
// Every expectation is the installed 7.37.5 build's own output, captured on 2026-08-27 by driving
// the real rule over these exact sources through the ESLint Linter API. Upstream reports columns;
// the reported text is sliced out of the source here, which is the same claim in the form this
// harness can check.
func TestNoUnstableNestedComponentsSpansAndMessages(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name string
		// source is the whole file.
		source string
		// reported is the exact text the finding must span.
		reported string
		// message is the exact rendered description.
		message string
	}{
		{
			name: "names a function declaration parent",
			source: `function ParentComponent() {
  function Nested() { return <div />; }
  return <div><Nested /></div>;
}`,
			reported: `function Nested() { return <div />; }`,
			message:  upstreamMessage("ParentComponent", false),
		},
		{
			// Upstream's resolveComponentName falls back to the binding name only for an arrow
			// parent, so this one names the parent.
			name: "names an arrow parent through its binding",
			source: `const ParentComponent = () => {
  function Nested() { return <div />; }
  return <div><Nested /></div>;
};`,
			reported: `function Nested() { return <div />; }`,
			message:  upstreamMessage("ParentComponent", false),
		},
		{
			// The same file written with a function expression parent produces the no-name wording,
			// because upstream's fallback is guarded on ArrowFunctionExpression specifically.
			// Measured: this is upstream's behaviour, not ours, and reproducing the narrower guard
			// is what keeps the message text right.
			name: "omits the name for a function expression parent",
			source: `const ParentComponent = function () {
  function Nested() { return <div />; }
  return <div><Nested /></div>;
};`,
			reported: `function Nested() { return <div />; }`,
			message:  upstreamMessage("", false),
		},
		{
			name: "appends the as-props note for a component in a JSX prop",
			source: `function ParentComponent() {
  return <SomeComponent footer={() => <div />} />;
}`,
			reported: `() => <div />`,
			message:  upstreamMessage("ParentComponent", true),
		},
		{
			// Our parser keeps the parenthesis node estree folds away. Upstream reports this at
			// column 34, one past the unparenthesized case, which is the arrow itself rather than
			// the parenthesized expression. Without semanticParentOf skipping the parens, the
			// walk up to the JSX attribute fails and the rule goes silent.
			name: "reports through a parenthesized prop value",
			source: `function ParentComponent() {
  return <SomeComponent footer={(() => <div />)} />;
}`,
			reported: `() => <div />`,
			message:  upstreamMessage("ParentComponent", true),
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedWithOptions(test, NoUnstableNestedComponents,
				"Component.tsx", testCase.source, DefaultNoUnstableNestedComponentsOptions())
			rule_testing.ExpectFindings(test, result, "unstableNestedComponent")

			// RunTyped writes strings.TrimSpace(source)+"\n" to disk, so slice what the harness
			// actually wrote rather than the literal above.
			onDisk := strings.TrimSpace(testCase.source) + "\n"
			diagnostic := result.Diagnostics[0]
			reported := onDisk[diagnostic.Range.Pos():diagnostic.Range.End()]
			if reported != testCase.reported {
				test.Errorf("finding spans %q, want %q", reported, testCase.reported)
			}
			if diagnostic.Message.Description != testCase.message {
				test.Errorf("message\n got %q\nwant %q", diagnostic.Message.Description, testCase.message)
			}
		})
	}
}

// TestNoUnstableNestedComponentsLowercaseParentIsSilent pins the parent-name capitalization test,
// which is not the same predicate as this package's hasComponentCapitalization.
//
// Upstream indexes character zero and compares it to its own lowercase, with no underscore
// stripping, so `_ParentComponent` is a lowercase parent and nothing inside it reports. Measured
// silent against the installed build on 2026-08-27. `no-multi-comp` counts `_Foo` as a component
// through the other predicate, so the two look interchangeable and are not.
func TestNoUnstableNestedComponentsLowercaseParentIsSilent(test *testing.T) {
	test.Parallel()

	for _, source := range []string{
		`function _ParentComponent() {
  function Nested() { return <div />; }
  return <div><Nested /></div>;
}`,
		`function createTestComponent() {
  function Nested() { return <div />; }
  return <div><Nested /></div>;
}`,
	} {
		result := rule_testing.RunTypedWithOptions(test, NoUnstableNestedComponents,
			"Component.tsx", source, DefaultNoUnstableNestedComponentsOptions())
		rule_testing.ExpectClean(test, result)
	}
}

// TestNoUnstableNestedComponentsRenderMethodGuard pins the claim isInsideRenderMethod's doc comment
// makes: our parser produces no node whose semantic parent is a method declaration, so upstream's
// `node.parent.type === 'MethodDefinition'` guard has no input here.
//
// Written as a structural assertion over the kinds this rule validates rather than as a findings
// check, because a findings check would pass whether the claim held or not.
func TestNoUnstableNestedComponentsRenderMethodGuard(test *testing.T) {
	test.Parallel()

	source := `class ParentComponent extends React.Component {
  render() {
    class Nested extends React.Component {
      render() { return <div />; }
    }
    return <div><Nested /></div>;
  }
}`

	probe := rule.Rule{
		Name:             "react/no-unstable-nested-components",
		NeedsTypeChecker: true,
		Run: func(ruleContext rule.Context, options any) rule.Listeners {
			return rule.Listeners{
				ast.KindSourceFile: func(sourceFile *ast.Node) {
					validated := 0
					offenders := 0
					var visit func(*ast.Node)
					visit = func(node *ast.Node) {
						switch node.Kind {
						case ast.KindFunctionDeclaration, ast.KindArrowFunction,
							ast.KindFunctionExpression, ast.KindClassDeclaration,
							ast.KindCallExpression:
							validated++
							if parent := semanticParentOf(node); parent != nil &&
								parent.Kind == ast.KindMethodDeclaration {
								offenders++
							}
						}
						node.ForEachChild(func(child *ast.Node) bool {
							visit(child)
							return false
						})
					}
					visit(sourceFile)

					// The control: if the walk found nothing, the zero below would be meaningless.
					if validated == 0 {
						test.Error("no validated nodes found, so the offender count proves nothing")
					}
					if offenders != 0 {
						test.Errorf("%d of %d validated nodes have a method declaration as their "+
							"semantic parent, so the render-method guard is reachable after all",
							offenders, validated)
					}
				},
			}
		},
	}
	rule_testing.RunTyped(test, probe, "Component.tsx", source)
}

// TestDecodeNoUnstableNestedComponentsOptions routes configuration through the rule's own decoder,
// which is where a default inversion would live. The pattern defaults to a non-empty string, so a
// zero-value struct is a different rule rather than the documented one.
func TestDecodeNoUnstableNestedComponentsOptions(test *testing.T) {
	test.Parallel()

	// A bare "error" arrives as empty input and must land on the documented defaults.
	decoded, err := DecodeNoUnstableNestedComponentsOptions(nil)
	if err != nil {
		test.Fatalf("decoding empty input: %v", err)
	}
	options, ok := decoded.(NoUnstableNestedComponentsOptions)
	if !ok {
		test.Fatalf("decoder returned %T", decoded)
	}
	if options.PropNamePattern != "render*" {
		test.Errorf("empty input gave pattern %q, want render*", options.PropNamePattern)
	}
	if options.AllowAsProps {
		test.Error("empty input enabled allowAsProps")
	}

	// An explicit pattern replaces the default.
	decoded, err = DecodeNoUnstableNestedComponentsOptions([]byte(`{"propNamePattern":"*Renderer"}`))
	if err != nil {
		test.Fatalf("decoding a pattern: %v", err)
	}
	options, ok = decoded.(NoUnstableNestedComponentsOptions)
	if !ok {
		test.Fatalf("decoder returned %T", decoded)
	}
	if options.PropNamePattern != "*Renderer" {
		test.Errorf("pattern %q, want *Renderer", options.PropNamePattern)
	}

	// An explicit empty pattern folds back onto the default, matching upstream's `|| 'render*'`,
	// where the empty string is falsy.
	decoded, err = DecodeNoUnstableNestedComponentsOptions([]byte(`{"propNamePattern":""}`))
	if err != nil {
		test.Fatalf("decoding an empty pattern: %v", err)
	}
	options, ok = decoded.(NoUnstableNestedComponentsOptions)
	if !ok {
		test.Fatalf("decoder returned %T", decoded)
	}
	if options.PropNamePattern != "render*" {
		test.Errorf("empty pattern gave %q, want render*", options.PropNamePattern)
	}

	// customValidators is in upstream's schema and unread by this rule. It must decode rather than
	// error, or a configuration carrying it would fail to load.
	if _, decodeNoUnstableNestedComponentsOptionsError := DecodeNoUnstableNestedComponentsOptions(
		[]byte(`{"customValidators":["./x"],"allowAsProps":true}`),
	); decodeNoUnstableNestedComponentsOptionsError != nil {
		test.Fatalf("decoding customValidators: %v", decodeNoUnstableNestedComponentsOptionsError)
	}
}

// TestNoUnstableNestedComponentsNilOptions covers the path a bare "error" takes into Run, which
// bypasses the decoder entirely: config.OptionsRegistry hands a non-required rule nil, and a type
// assertion on nil yields the zero value. Without the fallback in Run the empty pattern would match
// no prop name and every render prop would start reporting.
func TestNoUnstableNestedComponentsNilOptions(test *testing.T) {
	test.Parallel()

	source := `function ParentComponent() {
  return <SomeComponent renderFooter={() => <div />} />;
}`
	result := rule_testing.RunTypedWithOptions(test, NoUnstableNestedComponents,
		"Component.tsx", source, nil)
	rule_testing.ExpectClean(test, result)
}

// TestNoUnstableNestedComponentsAllowAsProps pins the option against the same source with it off,
// so the case cannot pass by the rule being silent for an unrelated reason.
func TestNoUnstableNestedComponentsAllowAsProps(test *testing.T) {
	test.Parallel()

	source := `function ParentComponent() {
  return <SomeComponent footer={() => <div />} />;
}`

	reporting := rule_testing.RunTypedWithOptions(test, NoUnstableNestedComponents,
		"Component.tsx", source, DefaultNoUnstableNestedComponentsOptions())
	rule_testing.ExpectFindings(test, reporting, "unstableNestedComponent")

	allowed := rule_testing.RunTypedWithOptions(test, NoUnstableNestedComponents,
		"Component.tsx", source, NoUnstableNestedComponentsOptions{
			AllowAsProps:    true,
			PropNamePattern: "render*",
		})
	rule_testing.ExpectClean(test, allowed)
}

// TestNoUnstableNestedComponentsHasNoFileSuffixGate pins the absence of a `.tsx` gate.
//
// Upstream has no filename condition anywhere, and three rules in this package carried one
// inherited from an oxc port. A React component in a `.ts` file is ordinary and legal, so a gate
// here would cost every finding in every `.ts` file. The two runs differ only in the extension.
func TestNoUnstableNestedComponentsHasNoFileSuffixGate(test *testing.T) {
	test.Parallel()

	source := `function ParentComponent() {
  const Nested = () => React.createElement("div", null);
  return React.createElement("div", null, React.createElement(Nested, null));
}`

	for _, fileName := range []string{"Component.tsx", "Component.ts"} {
		result := rule_testing.RunTypedWithOptions(test, NoUnstableNestedComponents,
			fileName, source, DefaultNoUnstableNestedComponentsOptions())
		if len(result.Diagnostics) != 1 {
			test.Errorf("%s reported %d findings, want 1", fileName, len(result.Diagnostics))
		}
	}
}

// TestNoUnstableNestedComponentsDoesNotPanicOnAwkwardShapes covers the panic class the brief names:
// the walk recovers per file, so one nil dereference costs every rule its verdict on that file.
//
// The first shape is the one that actually fired during this port. `createElementCallCounts`
// forwards to an unchecked `node.AsCallExpression()`, and an ancestor walk hands it a
// VariableDeclaration on the way up. Upstream's own passing case 34 produces exactly that.
//
// The rest are shapes whose names cannot be read with Node.Text(): a destructured parameter, a JSX
// attribute whose value is a string rather than an expression container, a spread attribute with no
// name at all, a computed object key, and a namespaced JSX attribute.
func TestNoUnstableNestedComponentsDoesNotPanicOnAwkwardShapes(test *testing.T) {
	test.Parallel()

	sources := []string{
		`function ParentComponent() {
  const rows = [{ notPrefixedWithRender: (props) => <Row {...props} /> }];
  return <Table rows={rows} />;
}`,
		`function ParentComponent() {
  function Nested({ a, b }: Options) { return <div />; }
  return <div><Nested a={1} b={2} /></div>;
}`,
		`function ParentComponent() {
  return <SomeComponent title="plain" footer={() => <div />} />;
}`,
		`function ParentComponent() {
  return <SomeComponent {...rest} footer={() => <div />} />;
}`,
		`function ParentComponent() {
  const handlers = { [computed]: () => <div /> };
  return <SomeComponent handlers={handlers} />;
}`,
		`function ParentComponent() {
  return <svg xmlns:xlink="x"><use xlink:href="#a" /></svg>;
}`,
		`function ParentComponent() {
  return <div>{items.map(([first, ...rest]) => <li key={first} />)}</div>;
}`,
	}

	for index, source := range sources {
		// A panic here fails the test rather than being recovered, which is the assertion.
		result := rule_testing.RunTypedWithOptions(test, NoUnstableNestedComponents,
			"Component.tsx", source, DefaultNoUnstableNestedComponentsOptions())
		test.Logf("shape %d produced %d findings", index, len(result.Diagnostics))
	}
}

// TestNoUnstableNestedComponentsHookNamePattern pins upstream's HOOK_REGEXP, `^use[A-Z0-9].*$`.
//
// The hook exemption is reached through the closest enclosing call's callee identifier, so a call
// whose name is not hook-shaped must not exempt what it returns. Upstream's corpus writes only
// hook-named calls in this position, so a mutation widening the pattern to match every name
// survived the whole imported set.
//
// Every row measured against the installed 7.37.5 build on 2026-08-27. Note `use` alone reports:
// the pattern requires a capital or a digit after `use`, so the bare name does not match.
func TestNoUnstableNestedComponentsHookNamePattern(test *testing.T) {
	test.Parallel()

	cases := []struct {
		calleeName string
		findings   int
	}{
		{calleeName: "useThing", findings: 0},
		{calleeName: "use", findings: 1},
		{calleeName: "uselower", findings: 1},
		{calleeName: "notAHook", findings: 1},
		// A digit satisfies the character class as surely as a capital does.
		{calleeName: "use2", findings: 0},
	}

	for _, testCase := range cases {
		test.Run(testCase.calleeName, func(test *testing.T) {
			test.Parallel()
			source := `function ParentComponent() {
  const X = ` + testCase.calleeName + `(() => {
    return () => <div />;
  });
  return <div><X /></div>;
}`
			result := rule_testing.RunTypedWithOptions(test, NoUnstableNestedComponents,
				"Component.tsx", source, DefaultNoUnstableNestedComponentsOptions())
			if len(result.Diagnostics) != testCase.findings {
				test.Errorf("%s gave %d findings, want %d",
					testCase.calleeName, len(result.Diagnostics), testCase.findings)
			}
		})
	}
}

// TestNoUnstableNestedComponentsPropNamePattern pins the option against the same source under two
// patterns, so neither row can pass by the rule being silent for an unrelated reason.
//
// The corpus configures propNamePattern exactly once, on a passing case, so nothing imported can
// see the pattern being consulted rather than ignored.
func TestNoUnstableNestedComponentsPropNamePattern(test *testing.T) {
	test.Parallel()

	source := `function ParentComponent() {
  return <SomeComponent someRenderer={() => <div />} />;
}`

	// Under the default `render*`, `someRenderer` is not a render prop, so it reports.
	def := rule_testing.RunTypedWithOptions(test, NoUnstableNestedComponents,
		"Component.tsx", source, DefaultNoUnstableNestedComponentsOptions())
	rule_testing.ExpectFindings(test, def, "unstableNestedComponent")

	// Under `*Renderer` it is one, so it is silent. This is upstream's own valid case 39's option.
	configured := rule_testing.RunTypedWithOptions(test, NoUnstableNestedComponents,
		"Component.tsx", source, NoUnstableNestedComponentsOptions{PropNamePattern: "*Renderer"})
	rule_testing.ExpectClean(test, configured)
}

// TestNoUnstableNestedComponentsAttributeValueKinds covers JSX attribute values that are not
// expression containers: a string value and a bare boolean attribute with no value at all.
//
// Both rows are here for the panic surface rather than for the predicate. A bare attribute has no
// value node, so reading it through anything but the typed accessor is a nil dereference, and the
// walk recovers per file rather than per rule.
//
// # What these rows do not prove, stated because my first version of this comment claimed they did
//
// I wrote them expecting them to kill a mutation that drops the container check. They do not, and
// re-running found that out. The reason is that `Nested` is declared in the parent's body, so its
// ancestry is Block > FunctionDeclaration > SourceFile and it never walks near a JSX attribute at
// all. The predicate is not consulted for either row.
//
// Measured against the installed build on 2026-08-27, the guard's real job is to stop the walk
// climbing past a string-valued attribute toward an outer one that is a container:
//
//	<Outer footer={() => <div />} />                        reports, with the as-props note
//	<Outer footer={<Inner title="x" render={...} />} />     silent
//	<Outer footer={<Inner title="x">{...}</Inner>} />       silent
//
// Both of the shapes that would exercise the climb are silent upstream for a different reason, so
// no fixture built from them can separate the two versions either. The mutation stays a live
// survivor and is recorded as such in the rule's own comment rather than papered over here.
func TestNoUnstableNestedComponentsAttributeValueKinds(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name   string
		source string
	}{
		{
			name: "string valued attribute",
			source: `function ParentComponent() {
  function Nested() { return <div />; }
  return <SomeComponent title="x"><Nested /></SomeComponent>;
}`,
		},
		{
			name: "bare boolean attribute with no value",
			source: `function ParentComponent() {
  function Nested() { return <div />; }
  return <SomeComponent flag><Nested /></SomeComponent>;
}`,
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedWithOptions(test, NoUnstableNestedComponents,
				"Component.tsx", testCase.source, DefaultNoUnstableNestedComponentsOptions())
			rule_testing.ExpectFindings(test, result, "unstableNestedComponent")
			if strings.Contains(result.Diagnostics[0].Message.Description, "allowAsProps") {
				test.Errorf("a component declared in the parent body must not be read as a prop, but "+
					"the finding carried the as-props note: %q",
					result.Diagnostics[0].Message.Description)
			}
		})
	}
}

// TestNoUnstableNestedComponentsAttributeClimbIsBounded pins the three shapes above as a group.
//
// The first reports and the other two are silent, all three measured against the installed 7.37.5
// build. Held together in one test because the contrast is the content: the only difference between
// the first and the rest is whether an element with a string attribute sits between the component
// and the attribute that would exempt it.
func TestNoUnstableNestedComponentsAttributeClimbIsBounded(test *testing.T) {
	test.Parallel()

	reporting := `function ParentComponent() {
  return <Outer footer={() => <div />} />;
}`
	result := rule_testing.RunTypedWithOptions(test, NoUnstableNestedComponents,
		"Component.tsx", reporting, DefaultNoUnstableNestedComponentsOptions())
	rule_testing.ExpectFindings(test, result, "unstableNestedComponent")

	for _, source := range []string{
		`function ParentComponent() {
  return <Outer footer={<Inner title="x" render={() => <div />} />} />;
}`,
		`function ParentComponent() {
  return <Outer footer={<Inner title="x">{() => <div />}</Inner>} />;
}`,
	} {
		silent := rule_testing.RunTypedWithOptions(test, NoUnstableNestedComponents,
			"Component.tsx", source, DefaultNoUnstableNestedComponentsOptions())
		rule_testing.ExpectClean(test, silent)
	}
}

// TestNoUnstableNestedComponentsInsideClassComponents pins the class recovery path, which upstream
// needs because its detection pass misses a function component declared inside a class.
//
// Both rows measured against the installed 7.37.5 build on 2026-08-27, and the second is the one
// worth having: upstream does not confine the recovery to `render`, so a component built in an
// ordinary method reports too.
func TestNoUnstableNestedComponentsInsideClassComponents(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name   string
		source string
	}{
		{
			name: "arrow component in a render method",
			source: `class ParentComponent extends React.Component {
  render() {
    const Nested = () => <div />;
    return <div><Nested /></div>;
  }
}`,
		},
		{
			name: "arrow component in a method that is not render",
			source: `class ParentComponent extends React.Component {
  helper() {
    const Nested = () => <div />;
    return <Nested />;
  }
  render() { return <div>{this.helper()}</div>; }
}`,
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedWithOptions(test, NoUnstableNestedComponents,
				"Component.tsx", testCase.source, DefaultNoUnstableNestedComponentsOptions())
			rule_testing.ExpectFindings(test, result, "unstableNestedComponent")
		})
	}
}

// TestNoUnstableNestedComponentsCreateElementPropsPosition pins the props argument of a
// createElement call, which is the second one.
//
// Measured against the installed build on 2026-08-27: a component in the props object reports with
// the as-props note, and one reached through the type slot does too, because a direct property
// value is claimed by an earlier branch before the createElement route is consulted.
//
// A mutation changing the argument index from 1 to 0 survives, and that is not a gap these rows
// close. Instrumented over all 77 corpus cases plus these: the createElement route is reached 92
// times, exactly one of those reaches a call carrying two or more arguments, and for that one both
// indices answer false. Every other reach short-circuits in `isComponentInProp`'s property branch
// first. A distinguishing input needs a detected component inside a props object that is not itself
// a direct property value, and I could not construct one upstream also detects. Recorded as
// unresolved rather than as an equivalence verdict.
func TestNoUnstableNestedComponentsCreateElementPropsPosition(test *testing.T) {
	test.Parallel()

	source := `function ParentComponent() {
  return React.createElement(Some, { footer: () => React.createElement("div", null) });
}`
	result := rule_testing.RunTypedWithOptions(test, NoUnstableNestedComponents,
		"Component.tsx", source, DefaultNoUnstableNestedComponentsOptions())
	rule_testing.ExpectFindings(test, result, "unstableNestedComponent")
	if !strings.Contains(result.Diagnostics[0].Message.Description, "allowAsProps") {
		test.Errorf("a component in a createElement props object must carry the as-props note, got %q",
			result.Diagnostics[0].Message.Description)
	}
}

// TestPropNamePatternIsCompiledOncePerPattern holds why compilePropNamePattern keeps its matchers:
// `Run` asks for one per file. A pattern seen before must come back without compiling again, which
// is an allocation-free lookup, and the matcher it returns must still be the pattern's.
//
// Not parallel: testing.AllocsPerRun reads the process-wide allocation count, so a test running
// beside it would add its allocations to this one's.
func TestPropNamePatternIsCompiledOncePerPattern(test *testing.T) {
	compiled := testing.AllocsPerRun(10, func() {
		buildPropNamePattern("render*")
	})
	if compiled < 1 {
		test.Fatalf("compiling a pattern allocated %.0f times; the comparison below cannot fail", compiled)
	}
	compilePropNamePattern("cached*")
	repeated := testing.AllocsPerRun(10, func() {
		compilePropNamePattern("cached*")
	})
	if repeated != 0 {
		test.Errorf("asking again for a pattern already seen allocated %.0f times, want 0", repeated)
	}
	matches := compilePropNamePattern("cached*")
	if !matches("cachedValue") || matches("renderValue") {
		test.Error("the kept matcher does not match its own pattern")
	}
}
