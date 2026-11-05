package react

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// preferEs6ClassFile is where the fixtures pretend to live.
//
// A .tsx extension because most corpus cases return JSX from a render method. The rule has no
// suffix gate, which the suffix cases below pin by writing reporting source with no JSX in it to
// all four extensions.
const preferEs6ClassFile = "/repository/source/PreferEs6Class.tsx"

// The corpus is upstream's, extracted mechanically rather than retyped.
//
// Every case in the two tables below comes from
// /tmp/lint-sources/eslint-plugin-react/tests/lib/rules/prefer-es6-class.js, read by evaluating the
// two arrays in the tester and emitting Go raw strings from the decoded values, so no escape
// sequence was typed on the way here. Upstream carries 5 valid and 3 invalid cases, each invalid
// one naming exactly one messageId.
//
// That is a thin corpus for a rule with two arms and a factory-name set, and it is silent about
// every question this port had to answer: it writes no `createClass`, no `React.createClass`, no
// class expression, no parenthesized anything and no JSDoc. Those all live in the measured tables
// below, each row driven against the installed build rather than reasoned from the source.
//
// The option is upstream's bare string, and every option fixture routes through the exported
// decoder in upstream's spelling, so the decoder is under test rather than bypassed.

// TestPreferEs6ClassFires runs the three failing cases from upstream, one finding each.
func TestPreferEs6ClassFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		mode       PreferEs6ClassMode
		sourceText string
		wantIds    []string
	}{
		{"invalid 0 a factory call with a displayName by default", PreferEs6ClassAlways, `
        var Hello = createReactClass({
          displayName: 'Hello',
          render: function() {
            return <div>Hello {this.props.name}</div>;
          }
        });
      `, []string{"shouldUseES6Class"}},
		{"invalid 1 a factory call under always", PreferEs6ClassAlways, `
        var Hello = createReactClass({
          render: function() {
            return <div>Hello {this.props.name}</div>;
          }
        });
      `, []string{"shouldUseES6Class"}},
		{"invalid 2 an es6 class under never", PreferEs6ClassNever, `
        class Hello extends React.Component {
          render() {
            return <div>Hello {this.props.name}</div>;
          }
        }
      `, []string{"shouldUseCreateClass"}},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := runPreferEs6Class(test, testCase.mode, testCase.sourceText)
			rule_testing.ExpectFindings(test, result, testCase.wantIds...)
		})
	}
}

// TestPreferEs6ClassStaysSilent runs the five passing cases from upstream.
func TestPreferEs6ClassStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		mode       PreferEs6ClassMode
		sourceText string
	}{
		{"valid 0 an es6 class with a displayName assigned after by default", PreferEs6ClassAlways, `
        class Hello extends React.Component {
          render() {
            return <div>Hello {this.props.name}</div>;
          }
        }
        Hello.displayName = 'Hello'
      `},
		{"valid 1 an exported es6 class by default", PreferEs6ClassAlways, `
        export default class Hello extends React.Component {
          render() {
            return <div>Hello {this.props.name}</div>;
          }
        }
        Hello.displayName = 'Hello'
      `},
		{"valid 2 a file with no component in it by default", PreferEs6ClassAlways, `
        var Hello = "foo";
        module.exports = {};
      `},
		{"valid 3 a factory call under never", PreferEs6ClassNever, `
        var Hello = createReactClass({
          render: function() {
            return <div>Hello {this.props.name}</div>;
          }
        });
      `},
		{"valid 4 an es6 class under always", PreferEs6ClassAlways, `
        class Hello extends React.Component {
          render() {
            return <div>Hello {this.props.name}</div>;
          }
        }
      `},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := runPreferEs6Class(test, testCase.mode, testCase.sourceText)
			rule_testing.ExpectClean(test, result)
		})
	}
}

// runPreferEs6Class drives the rule through its own exported decoder.
//
// Routing through DecodePreferEs6ClassOptions rather than building the options struct directly is
// what puts the decoder under test: each mode reaches the rule as upstream writes it, `"always"` or
// `"never"`.
func runPreferEs6Class(test *testing.T, mode PreferEs6ClassMode, sourceText string) rule_testing.Result {
	test.Helper()
	decoded, decodePreferEs6ClassOptionsError := DecodePreferEs6ClassOptions(
		[]byte(`"` + strings.ToLower(string(mode)) + `"`),
	)
	if decodePreferEs6ClassOptionsError != nil {
		test.Fatalf("decoding mode %q: %v", mode, decodePreferEs6ClassOptionsError)
	}
	return rule_testing.RunWithOptions(test, PreferEs6Class, preferEs6ClassFile, sourceText, decoded)
}

// TestPreferEs6ClassReportsWithoutOptions pins the nil-options fallback.
//
// A rule configured as bare "error" is handed nil, which type-asserts to the zero value whose Mode
// matches neither arm. Without the explicit fallback the rule would register, pass every fixture
// above, and report nothing on the real tree. Every other fixture here reaches the rule through the
// decoder, so nothing else in this file can see it.
func TestPreferEs6ClassReportsWithoutOptions(test *testing.T) {
	test.Parallel()

	source := "var Hello = createReactClass({ render: function() { return null; } });\n"
	result := rule_testing.Run(test, PreferEs6Class, preferEs6ClassFile, source)
	rule_testing.ExpectFindings(test, result, "shouldUseES6Class")
}

// TestPreferEs6ClassHasNoFileSuffixGate pins the absence of the gate three siblings carry.
//
// Those siblings were ported from oxc, which gates on the file being read as JSX. The authority
// here has no such gate. The source deliberately holds no JSX so a .ts file parses and the parser's
// opinion cannot be mistaken for the rule's.
func TestPreferEs6ClassHasNoFileSuffixGate(test *testing.T) {
	test.Parallel()

	source := "var Hello = createReactClass({ render: function() { return null; } });\n"
	for _, fileName := range []string{
		"/repository/source/Suffix.tsx",
		"/repository/source/Suffix.jsx",
		"/repository/source/Suffix.ts",
		"/repository/source/Suffix.js",
	} {
		test.Run(fileName, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, PreferEs6Class, fileName, source)
			rule_testing.ExpectFindings(test, result, "shouldUseES6Class")
		})
	}
}

// TestPreferEs6ClassFactoryNames pins which factory spellings count, and the corpus writes only one.
//
// `getCreateClassFromContext` defaults to the single name `createReactClass`, so the bare and
// namespaced `createClass` spellings are both clean. That is the opposite of what the shelf's
// `IsEs5ComponentCall` answers, which is why this rule reuses the narrowed
// `isEs5ComponentCallStrict` instead. Following the shelf would report on code upstream leaves
// alone, and the corpus could not see it: it writes `createReactClass` and nothing else.
//
// Every row measured against the installed build.
func TestPreferEs6ClassFactoryNames(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		wantIds    []string
	}{
		// The control. If this stops reporting the rest of the table measures nothing.
		{"the bare factory", `var H = createReactClass({ render() { return null; } });`, []string{"shouldUseES6Class"}},
		{
			"the namespaced factory",
			`var H = React.createReactClass({ render() { return null; } });`,
			[]string{"shouldUseES6Class"},
		},

		// The shelf accepts these two and upstream does not.
		{"bare createClass", `var H = createClass({ render() { return null; } });`, nil},
		{"namespaced createClass", `var H = React.createClass({ render() { return null; } });`, nil},

		// The receiver has to be the React pragma.
		{"somebody else's factory", `var H = Foo.createReactClass({ render() { return null; } });`, nil},

		// Not a call at all.
		{"a plain object literal", `var H = { render() { return null; } };`, nil},
		{"an object passed to something else", `var H = somethingElse({ render() { return null; } });`, nil},

		// Parenthesised spellings, all measured reporting upstream. ESLint's tree does not
		// materialise these, so upstream never thinks about them and the corpus writes none.
		{
			"a parenthesised bare callee",
			`var H = (createReactClass)({ render() { return null; } });`,
			[]string{"shouldUseES6Class"},
		},
		{
			"a parenthesised namespaced callee",
			`var H = (React.createReactClass)({ render() { return null; } });`,
			[]string{"shouldUseES6Class"},
		},
		{
			"a parenthesised receiver",
			`var H = (React).createReactClass({ render() { return null; } });`,
			[]string{"shouldUseES6Class"},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := runPreferEs6Class(test, PreferEs6ClassAlways, testCase.sourceText)
			rule_testing.ExpectFindings(test, result, testCase.wantIds...)
		})
	}
}

// TestPreferEs6ClassArgumentPosition pins that the rule asks about the object's parent.
//
// Upstream anchors on the object expression and tests whether its parent is a matching call, so the
// object's position among the arguments does not matter and a nested object is clean because its
// parent is the enclosing object. A port asking "is this the factory's first argument" would pass
// the whole corpus and get both rows below wrong. Both measured.
func TestPreferEs6ClassArgumentPosition(test *testing.T) {
	test.Parallel()

	test.Run("an object in second position still reports", func(test *testing.T) {
		test.Parallel()
		source := `var H = createReactClass(x, { render() { return null; } });`
		result := runPreferEs6Class(test, PreferEs6ClassAlways, source)
		rule_testing.ExpectFindings(test, result, "shouldUseES6Class")
	})

	test.Run("a nested object reports once, for the outer one", func(test *testing.T) {
		test.Parallel()
		source := `var H = createReactClass({ a: { b: 1 }, render() { return null; } });`
		result := runPreferEs6Class(test, PreferEs6ClassAlways, source)
		rule_testing.ExpectFindings(test, result, "shouldUseES6Class")
	})

	test.Run("a factory call assigned to nothing still reports", func(test *testing.T) {
		test.Parallel()
		source := `createReactClass({ render() { return null; } });`
		result := runPreferEs6Class(test, PreferEs6ClassAlways, source)
		rule_testing.ExpectFindings(test, result, "shouldUseES6Class")
	})

	test.Run("two components report twice", func(test *testing.T) {
		test.Parallel()
		source := "var A = createReactClass({ render() { return null; } });\n" +
			"var B = createReactClass({ render() { return null; } });\n"
		result := runPreferEs6Class(test, PreferEs6ClassAlways, source)
		rule_testing.ExpectFindings(test, result, "shouldUseES6Class", "shouldUseES6Class")
	})
}

// TestPreferEs6ClassNeverAnchorsOnDeclarationsOnly pins the node kind the Never arm registers.
//
// Upstream registers `ClassDeclaration` and not `ClassExpression`, so a component written as a
// class expression is silent. This is worth a fixture because the sibling `state-in-constructor` in
// this package goes the other way: its scope walk finds a class expression and reports there. Two
// rules, one authority, opposite answers, and the difference is which node each registers.
//
// Reproduced rather than improved on. A port that helpfully added class expressions would report on
// code upstream leaves alone and no imported fixture could see it. Both rows measured.
func TestPreferEs6ClassNeverAnchorsOnDeclarationsOnly(test *testing.T) {
	test.Parallel()

	test.Run("a class declaration reports", func(test *testing.T) {
		test.Parallel()
		source := "class Hello extends React.Component { render() { return null; } }\n"
		result := runPreferEs6Class(test, PreferEs6ClassNever, source)
		rule_testing.ExpectFindings(test, result, "shouldUseCreateClass")
	})

	test.Run("a class expression is silent", func(test *testing.T) {
		test.Parallel()
		source := "const Hello = class extends React.Component { render() { return null; } };\n"
		result := runPreferEs6Class(test, PreferEs6ClassNever, source)
		rule_testing.ExpectClean(test, result)
	})
}

// TestPreferEs6ClassBaseNames pins upstream's `/^(Pure)?Component$/` and its anchors.
//
// The pattern is anchored at both ends, so the two near-miss rows are the ones that show the
// anchors doing work: a port testing for a `Component` substring would pass every corpus case and
// report on both. Every row measured against the installed build.
func TestPreferEs6ClassBaseNames(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name     string
		heritage string
		wantIds  []string
	}{
		{"React.Component", `extends React.Component`, []string{"shouldUseCreateClass"}},
		{"React.PureComponent", `extends React.PureComponent`, []string{"shouldUseCreateClass"}},
		{"bare Component", `extends Component`, []string{"shouldUseCreateClass"}},
		{"bare PureComponent", `extends PureComponent`, []string{"shouldUseCreateClass"}},

		// Parenthesised spellings, all measured reporting upstream.
		{"a parenthesised qualified base", `extends (React.Component)`, []string{"shouldUseCreateClass"}},
		{"a parenthesised receiver", `extends (React).Component`, []string{"shouldUseCreateClass"}},
		{"a parenthesised bare base", `extends (Component)`, []string{"shouldUseCreateClass"}},

		// The anchors. Neither of these is a component.
		{"a name with the prefix", `extends ComponentFoo`, nil},
		{"a name with the suffix", `extends FooComponent`, nil},

		// The receiver has to be React.
		{"somebody else's Component", `extends Foo.Component`, nil},

		// No heritage at all.
		{"no heritage", ``, nil},

		// An implements clause names an interface, never a base class. This parser gives it
		// KindTypeReference, so the kind test declines it; no separate token test is needed and one
		// would be inert, measured on the sibling state-in-constructor.
		{"implements rather than extends", `implements React.Component`, nil},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			source := "class Hello " + testCase.heritage + " { render() { return null; } }\n"
			result := runPreferEs6Class(test, PreferEs6ClassNever, source)
			rule_testing.ExpectFindings(test, result, testCase.wantIds...)
		})
	}
}

// TestPreferEs6ClassIgnoresJsDocComponents pins a path that is dead in the installed build.
//
// `isES6Component` first calls `isExplicitComponent`, which reads a JSDoc comment and looks for an
// `@extends React.Component` or `@augments` tag, so a class with no heritage carrying that tag
// should report under Never. It does not: all four spellings are silent, measured under both the
// default parser and the TypeScript one.
//
// Confirmed as a plugin-wide dead path rather than something specific to this rule by running
// `react/require-render-return`, which routes through the same `isES6Component`, over the same
// JSDoc class: also silent, while that rule over a class with a real `extends` clause reports.
// ESLint 10's `getJSDocComment` no longer returns what `isExplicitComponent` expects.
//
// So this rule implements no JSDoc path and these cases assert the silence. If a later ESLint
// revives the path they become the fixtures that fail, which is the point of writing them down
// rather than leaving the absence unrecorded.
func TestPreferEs6ClassIgnoresJsDocComponents(test *testing.T) {
	test.Parallel()

	for _, tag := range []string{
		"@extends React.Component",
		"@augments React.Component",
		"@extends React.PureComponent",
		"@extends Something.Else",
	} {
		test.Run(tag, func(test *testing.T) {
			test.Parallel()
			source := "/**\n * " + tag + "\n */\nclass Hello { render() { return null; } }\n"
			result := runPreferEs6Class(test, PreferEs6ClassNever, source)
			rule_testing.ExpectClean(test, result)
		})
	}
}

// TestPreferEs6ClassArmsAreDisjoint pins that each mode is silent on the other's subject.
//
// The corpus states this for two of the four pairings and this completes the square. It is the
// cheapest guard against a dispatch that registers both listeners regardless of mode, which would
// report twice on a file holding both styles while every corpus case still passed.
func TestPreferEs6ClassArmsAreDisjoint(test *testing.T) {
	test.Parallel()

	factory := "var Hello = createReactClass({ render() { return null; } });\n"
	class := "class Hello extends React.Component { render() { return null; } }\n"

	test.Run("Always is silent on an es6 class", func(test *testing.T) {
		test.Parallel()
		rule_testing.ExpectClean(test, runPreferEs6Class(test, PreferEs6ClassAlways, class))
	})
	test.Run("Never is silent on a factory call", func(test *testing.T) {
		test.Parallel()
		rule_testing.ExpectClean(test, runPreferEs6Class(test, PreferEs6ClassNever, factory))
	})
	test.Run("Always reports only the factory when both are present", func(test *testing.T) {
		test.Parallel()
		result := runPreferEs6Class(test, PreferEs6ClassAlways, factory+class)
		rule_testing.ExpectFindings(test, result, "shouldUseES6Class")
	})
	test.Run("Never reports only the class when both are present", func(test *testing.T) {
		test.Parallel()
		result := runPreferEs6Class(test, PreferEs6ClassNever, factory+class)
		rule_testing.ExpectFindings(test, result, "shouldUseCreateClass")
	})
}

// TestPreferEs6ClassSpans asserts where each finding points.
//
// The two arms report different node kinds and therefore different spans, which is the one thing a
// message-id assertion cannot see. The Always arm reports the object expression, so the span starts
// at the brace and excludes `createReactClass(` entirely; the Never arm reports the whole class
// declaration. Measured by reading the reported columns from the installed build.
func TestPreferEs6ClassSpans(test *testing.T) {
	test.Parallel()

	test.Run("Always underlines the object, not the call", func(test *testing.T) {
		test.Parallel()
		source := "var Hello = createReactClass({ render() { return null; } });\n"
		result := runPreferEs6Class(test, PreferEs6ClassAlways, source)
		if len(result.Diagnostics) != 1 {
			test.Fatalf("wanted exactly one finding, got %d", len(result.Diagnostics))
		}
		got := source[result.Diagnostics[0].Range.Pos():result.Diagnostics[0].Range.End()]
		want := "{ render() { return null; } }"
		if got != want {
			test.Errorf("span text = %q, wanted %q", got, want)
		}
	})

	test.Run("Never underlines the whole class declaration", func(test *testing.T) {
		test.Parallel()
		source := "class Hello extends React.Component { render() { return null; } }\n"
		result := runPreferEs6Class(test, PreferEs6ClassNever, source)
		if len(result.Diagnostics) != 1 {
			test.Fatalf("wanted exactly one finding, got %d", len(result.Diagnostics))
		}
		got := source[result.Diagnostics[0].Range.Pos():result.Diagnostics[0].Range.End()]
		want := "class Hello extends React.Component { render() { return null; } }"
		if got != want {
			test.Errorf("span text = %q, wanted %q", got, want)
		}
	})
}

// TestPreferEs6ClassMessages asserts the two messages by identity.
//
// Asserting the ids against literals typed here rather than against the rule's own constants is
// deliberate: comparing a finding to the constant it was reported with is an equality that moves on
// both sides under mutation and cannot fail.
func TestPreferEs6ClassMessages(test *testing.T) {
	test.Parallel()

	if messagePreferEs6ClassShouldUseEs6Class.Id != "shouldUseES6Class" {
		test.Errorf("Always arm id = %q", messagePreferEs6ClassShouldUseEs6Class.Id)
	}
	if messagePreferEs6ClassShouldUseCreateClass.Id != "shouldUseCreateClass" {
		test.Errorf("Never arm id = %q", messagePreferEs6ClassShouldUseCreateClass.Id)
	}
	for _, message := range []rule.Message{
		messagePreferEs6ClassShouldUseEs6Class,
		messagePreferEs6ClassShouldUseCreateClass,
	} {
		if len(message.Description) < 80 {
			test.Errorf("message %q has a description too short to say why: %q", message.Id, message.Description)
		}
	}
}

// TestDecodePreferEs6ClassOptions pins the decoder against upstream's schema,
// `[{ enum: ["always", "never"] }]`.
func TestDecodePreferEs6ClassOptions(test *testing.T) {
	test.Parallel()

	accepted := []struct {
		name string
		raw  string
		want PreferEs6ClassMode
	}{
		{"no options at all means Always", ``, PreferEs6ClassAlways},
		{"upstream's always", `"always"`, PreferEs6ClassAlways},
		{"upstream's never", `"never"`, PreferEs6ClassNever},
	}
	for _, testCase := range accepted {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			decoded, decodePreferEs6ClassOptionsError := DecodePreferEs6ClassOptions([]byte(testCase.raw))
			if decodePreferEs6ClassOptionsError != nil {
				test.Fatalf("unexpected error: %v", decodePreferEs6ClassOptionsError)
			}
			options, ok := decoded.(PreferEs6ClassOptions)
			if !ok {
				test.Fatalf("decoded to %T, want PreferEs6ClassOptions", decoded)
			}
			if options.Mode != testCase.want {
				test.Errorf("mode = %q, wanted %q", options.Mode, testCase.want)
			}
		})
	}

	// The object form is the shape cohere invented before #d21war2 and no ESLint version accepts,
	// so it is refused in both of its old spellings. The rest are values upstream's enum refuses.
	refused := []struct {
		name string
		raw  string
	}{
		{"the old invented object", `{"mode":"Never"}`},
		{"the old invented object in upstream's casing", `{"mode":"never"}`},
		{"an empty object", `{}`},
		{"the old PascalCase spelling", `"Never"`},
		{"an unknown mode", `"sometimes"`},
		{"an empty string", `""`},
		{"null", `null`},
	}
	for _, testCase := range refused {
		test.Run(testCase.name+" is refused", func(test *testing.T) {
			test.Parallel()
			if _, decodePreferEs6ClassOptionsError := DecodePreferEs6ClassOptions(
				[]byte(testCase.raw),
			); decodePreferEs6ClassOptionsError == nil {
				test.Errorf("%s decoded; upstream's schema refuses it", testCase.raw)
			}
		})
	}
}
