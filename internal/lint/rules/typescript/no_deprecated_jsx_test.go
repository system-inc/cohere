package typescript

import "testing"

// TestNoDeprecatedInJsx covers the two JSX arms: a deprecated component and a deprecated prop.
//
// A prop's tag is on a property of the element name's contextual type rather than on anything the
// attribute itself resolves to, so this arm shares no code with the identifier arm and would be
// silent if only that one worked.
func TestNoDeprecatedInJsx(test *testing.T) {
	test.Parallel()

	cases := []noDeprecatedCase{
		{
			name: "valid:68",
			sourceText: "\n" +
				"declare namespace JSX {}\n" +
				"\n" +
				"<foo bar={1} />;\n" +
				"    ",
			jsx:      true,
			expected: nil,
		},
		{
			name: "valid:69",
			sourceText: "\n" +
				"declare namespace JSX {\n" +
				"  interface IntrinsicElements {\n" +
				"    foo: any;\n" +
				"  }\n" +
				"}\n" +
				"\n" +
				"<foo bar={1} />;\n" +
				"    ",
			jsx:      true,
			expected: nil,
		},
		{
			name: "valid:70",
			sourceText: "\n" +
				"declare namespace JSX {\n" +
				"  interface IntrinsicElements {\n" +
				"    foo: unknown;\n" +
				"  }\n" +
				"}\n" +
				"\n" +
				"<foo bar={1} />;\n" +
				"    ",
			jsx:      true,
			expected: nil,
		},
		{
			name: "valid:71",
			sourceText: "\n" +
				"declare namespace JSX {\n" +
				"  interface IntrinsicElements {\n" +
				"    foo: {\n" +
				"      bar: any;\n" +
				"    };\n" +
				"  }\n" +
				"}\n" +
				"<foo bar={1} />;\n" +
				"    ",
			jsx:      true,
			expected: nil,
		},
		{
			name: "valid:72",
			sourceText: "\n" +
				"declare namespace JSX {\n" +
				"  interface IntrinsicElements {\n" +
				"    foo: {\n" +
				"      bar: unknown;\n" +
				"    };\n" +
				"  }\n" +
				"}\n" +
				"<foo bar={1} />;\n" +
				"    ",
			jsx:      true,
			expected: nil,
		},
		{
			name: "valid:74",
			sourceText: "\n" +
				"/** @deprecated */\n" +
				"function A() {\n" +
				"  return <div />;\n" +
				"}\n" +
				"\n" +
				"const a = <A></A>;\n" +
				"      ",
			options:  "{\"allow\":[{\"from\":\"file\",\"name\":\"A\"}]}",
			jsx:      true,
			expected: nil,
		},
		{
			name: "invalid:0",
			sourceText: "\n" +
				"interface AProps {\n" +
				"  /** @deprecated */\n" +
				"  b: number | string;\n" +
				"}\n" +
				"\n" +
				"function A(props: AProps) {\n" +
				"  return <div />;\n" +
				"}\n" +
				"\n" +
				"const a = <A b=\"\" />;\n" +
				"      ",
			jsx: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "b", reason: ""},
			},
		},
		{
			name: "invalid:87",
			sourceText: "\n" +
				"/** @deprecated */\n" +
				"const A = () => <div />;\n" +
				"\n" +
				"const a = <A />;\n" +
				"      ",
			jsx: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "A", reason: ""},
			},
		},
		{
			name: "invalid:88",
			sourceText: "\n" +
				"/** @deprecated */\n" +
				"const A = () => <div />;\n" +
				"\n" +
				"const a = <A></A>;\n" +
				"      ",
			jsx: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "A", reason: ""},
			},
		},
		{
			name: "invalid:89",
			sourceText: "\n" +
				"/** @deprecated */\n" +
				"function A() {\n" +
				"  return <div />;\n" +
				"}\n" +
				"\n" +
				"const a = <A />;\n" +
				"      ",
			jsx: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "A", reason: ""},
			},
		},
		{
			name: "invalid:90",
			sourceText: "\n" +
				"/** @deprecated */\n" +
				"function A() {\n" +
				"  return <div />;\n" +
				"}\n" +
				"\n" +
				"const a = <A></A>;\n" +
				"      ",
			jsx: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "A", reason: ""},
			},
		},
		{
			name:           "invalid:143",
			sourceText:     "const a = <div aria-grabbed></div>;",
			jsx:            true,
			withReactTypes: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecatedWithReason", name: "aria-grabbed", reason: "in ARIA 1.1"},
			},
		},
		{
			name: "invalid:144",
			sourceText: "\n" +
				"declare namespace JSX {\n" +
				"  interface IntrinsicElements {\n" +
				"    'foo-bar:baz-bam': {\n" +
				"      name: string;\n" +
				"      /**\n" +
				"       * @deprecated\n" +
				"       */\n" +
				"      deprecatedProp: string;\n" +
				"    };\n" +
				"  }\n" +
				"}\n" +
				"\n" +
				"const componentDashed = <foo-bar:baz-bam name=\"e\" deprecatedProp=\"oh no\" />;\n" +
				"      ",
			jsx:      true,
			asScript: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "deprecatedProp", reason: ""},
			},
		},
		{
			name: "invalid:145",
			sourceText: "\n" +
				"import * as React from 'react';\n" +
				"\n" +
				"interface Props {\n" +
				"  /**\n" +
				"   * @deprecated\n" +
				"   */\n" +
				"  deprecatedProp: string;\n" +
				"}\n" +
				"\n" +
				"interface Tab {\n" +
				"  List: React.FC<Props>;\n" +
				"}\n" +
				"\n" +
				"const Tab: Tab = {\n" +
				"  List: () => <div>Hi</div>,\n" +
				"};\n" +
				"\n" +
				"const anotherExample = <Tab.List deprecatedProp=\"oh no\" />;\n" +
				"      ",
			jsx:                true,
			withInstalledTypes: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "deprecatedProp", reason: ""},
			},
		},
	}
	runNoDeprecatedCases(test, cases)
}

// TestNoDeprecatedOnIntrinsicJsxAttributes covers the intrinsic-attribute arm in the harness's own
// module mode.
//
// Upstream's two cases for this arm, invalid:143 and invalid:144 above, each pin something to run:
// the @types/react declarations, and a script program so a bare `declare namespace JSX` is global.
// These rows need neither, the namespace declared global inside a module. The second row is the
// control that matters: a rule reaching the intrinsic attribute type but reporting unconditionally
// would pass the first row and fail only this one.
func TestNoDeprecatedOnIntrinsicJsxAttributes(test *testing.T) {
	test.Parallel()

	const intrinsicElements = "declare global {\n" +
		"  namespace JSX {\n" +
		"    interface IntrinsicElements {\n" +
		"      'foo-bar': {\n" +
		"        /** @deprecated in ARIA 1.1 */\n" +
		"        deprecatedProp: string;\n" +
		"        fine: string;\n" +
		"      };\n" +
		"    }\n" +
		"  }\n" +
		"}\n"

	runNoDeprecatedCases(test, []noDeprecatedCase{
		{
			name:       "a deprecated attribute on an intrinsic element",
			sourceText: intrinsicElements + "export const a = <foo-bar deprecatedProp=\"x\" />;\n",
			jsx:        true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecatedWithReason", name: "deprecatedProp", reason: "in ARIA 1.1"},
			},
		},
		{
			name:       "an undeprecated attribute on the same element",
			sourceText: intrinsicElements + "export const a = <foo-bar fine=\"x\" />;\n",
			jsx:        true,
			expected:   nil,
		},
	})
}
