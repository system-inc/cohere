package typescript

import "testing"

// TestNoDeprecatedAcrossFiles is the part of the corpus that imports from another file.
//
// This is where the rule's difficulty lives and where the shim's alias accessors earn their place.
// The shared fixture writes tags in three positions that a one-hop answer confuses: on the
// declaration, on an export specifier that re-exports an untagged declaration, and on one overload
// of a function whose other overloads are clean. The middle position is invisible to a resolve,
// which jumps to the declaration and finds nothing, and is why deprecationInAliasChain walks.
func TestNoDeprecatedAcrossFiles(test *testing.T) {
	test.Parallel()

	cases := []noDeprecatedCase{
		{
			name: "valid:43",
			sourceText: "\n" +
				"import { deprecatedFunctionWithOverloads } from './deprecated';\n" +
				"\n" +
				"const foo = deprecatedFunctionWithOverloads();\n" +
				"    ",
			withDeprecatedFixture: true,
			expected:              nil,
		},
		{
			name: "valid:44",
			sourceText: "\n" +
				"import * as imported from './deprecated';\n" +
				"\n" +
				"const foo = imported.deprecatedFunctionWithOverloads();\n" +
				"    ",
			withDeprecatedFixture: true,
			expected:              nil,
		},
		{
			name: "valid:45",
			sourceText: "\n" +
				"import { ClassWithDeprecatedConstructor } from './deprecated';\n" +
				"\n" +
				"const foo = new ClassWithDeprecatedConstructor();\n" +
				"    ",
			withDeprecatedFixture: true,
			expected:              nil,
		},
		{
			name: "valid:46",
			sourceText: "\n" +
				"import * as imported from './deprecated';\n" +
				"\n" +
				"const foo = new imported.ClassWithDeprecatedConstructor();\n" +
				"    ",
			withDeprecatedFixture: true,
			expected:              nil,
		},
		{
			name: "valid:56",
			sourceText: "\n" +
				"export { deprecatedFunction as 'bur' } from './deprecated';\n" +
				"    ",
			withDeprecatedFixture: true,
			expected:              nil,
		},
		{
			name: "valid:57",
			sourceText: "\n" +
				"export { 'deprecatedFunction' } from './deprecated';\n" +
				"    ",
			withDeprecatedFixture: true,
			expected:              nil,
		},
		{
			name: "valid:61",
			sourceText: "\n" +
				"/**\n" +
				" * @deprecated\n" +
				" */\n" +
				"export import Bar = require('./deprecated');\n" +
				"    ",
			withDeprecatedFixture: true,
			expected:              nil,
		},
		{
			name: "valid:65",
			sourceText: "\n" +
				"async function fn() {\n" +
				"  const d = await import('./deprecated.js');\n" +
				"  d.default;\n" +
				"}\n" +
				"      ",
			withDeprecatedFixture: true,
			environmentDependent:  true,
			expected:              nil,
		},
		{
			name: "invalid:97",
			sourceText: "\n" +
				"import { DeprecatedClass } from './deprecated';\n" +
				"\n" +
				"const foo = new DeprecatedClass();\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "DeprecatedClass", reason: ""},
			},
		},
		{
			name: "invalid:98",
			sourceText: "\n" +
				"import { DeprecatedClass } from './deprecated';\n" +
				"\n" +
				"declare function inject(something: new () => unknown): void;\n" +
				"\n" +
				"inject(DeprecatedClass);\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "DeprecatedClass", reason: ""},
			},
		},
		{
			name: "invalid:99",
			sourceText: "\n" +
				"import { deprecatedVariable } from './deprecated';\n" +
				"\n" +
				"const foo = deprecatedVariable;\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "deprecatedVariable", reason: ""},
			},
		},
		{
			name: "invalid:100",
			sourceText: "\n" +
				"import { DeprecatedClass } from './deprecated';\n" +
				"\n" +
				"declare const x: DeprecatedClass;\n" +
				"\n" +
				"const { foo } = x;\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "DeprecatedClass", reason: ""},
				{messageId: "deprecated", name: "foo", reason: ""},
			},
		},
		{
			name: "invalid:101",
			sourceText: "\n" +
				"import { deprecatedFunction } from './deprecated';\n" +
				"\n" +
				"deprecatedFunction();\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "deprecatedFunction", reason: ""},
			},
		},
		{
			name: "invalid:102",
			sourceText: "\n" +
				"import * as imported from './deprecated';\n" +
				"\n" +
				"const foo = new imported.NormalClass();\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "NormalClass", reason: ""},
			},
		},
		{
			name: "invalid:103",
			sourceText: "\n" +
				"import { NormalClass } from './deprecated';\n" +
				"\n" +
				"const foo = new NormalClass();\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "NormalClass", reason: ""},
			},
		},
		{
			name: "invalid:104",
			sourceText: "\n" +
				"import * as imported from './deprecated';\n" +
				"\n" +
				"const foo = imported.NormalClass;\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "NormalClass", reason: ""},
			},
		},
		{
			name: "invalid:105",
			sourceText: "\n" +
				"import { NormalClass } from './deprecated';\n" +
				"\n" +
				"const foo = NormalClass;\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "NormalClass", reason: ""},
			},
		},
		{
			name: "invalid:106",
			sourceText: "\n" +
				"import { normalVariable } from './deprecated';\n" +
				"\n" +
				"const foo = normalVariable;\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "normalVariable", reason: ""},
			},
		},
		{
			name: "invalid:107",
			sourceText: "\n" +
				"import * as imported from './deprecated';\n" +
				"\n" +
				"const foo = imported.normalVariable;\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "normalVariable", reason: ""},
			},
		},
		{
			name: "invalid:108",
			sourceText: "\n" +
				"import * as imported from './deprecated';\n" +
				"\n" +
				"const { normalVariable } = imported;\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "normalVariable", reason: ""},
			},
		},
		{
			name: "invalid:109",
			sourceText: "\n" +
				"import { deprecatedVariable } from './deprecated';\n" +
				"\n" +
				"const test = {\n" +
				"  someField: deprecatedVariable,\n" +
				"};\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "deprecatedVariable", reason: ""},
			},
		},
		{
			name: "invalid:110",
			sourceText: "\n" +
				"import { normalFunction } from './deprecated';\n" +
				"\n" +
				"const foo = normalFunction;\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "normalFunction", reason: ""},
			},
		},
		{
			name: "invalid:111",
			sourceText: "\n" +
				"import * as imported from './deprecated';\n" +
				"\n" +
				"const foo = imported.normalFunction;\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "normalFunction", reason: ""},
			},
		},
		{
			name: "invalid:112",
			sourceText: "\n" +
				"import * as imported from './deprecated';\n" +
				"\n" +
				"const { normalFunction } = imported;\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "normalFunction", reason: ""},
			},
		},
		{
			name: "invalid:113",
			sourceText: "\n" +
				"import { normalFunction } from './deprecated';\n" +
				"\n" +
				"const foo = normalFunction();\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "normalFunction", reason: ""},
			},
		},
		{
			name: "invalid:114",
			sourceText: "\n" +
				"import * as imported from './deprecated';\n" +
				"\n" +
				"const foo = imported.normalFunction();\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "normalFunction", reason: ""},
			},
		},
		{
			name: "invalid:115",
			sourceText: "\n" +
				"import { deprecatedFunctionWithOverloads } from './deprecated';\n" +
				"\n" +
				"const foo = deprecatedFunctionWithOverloads('a');\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "deprecatedFunctionWithOverloads", reason: ""},
			},
		},
		{
			name: "invalid:116",
			sourceText: "\n" +
				"import * as imported from './deprecated';\n" +
				"\n" +
				"const foo = imported.deprecatedFunctionWithOverloads('a');\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "deprecatedFunctionWithOverloads", reason: ""},
			},
		},
		{
			name: "invalid:117",
			sourceText: "\n" +
				"import { reexportedDeprecatedFunctionWithOverloads } from './deprecated';\n" +
				"\n" +
				"const foo = reexportedDeprecatedFunctionWithOverloads;\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{
					messageId: "deprecatedWithReason",
					name:      "reexportedDeprecatedFunctionWithOverloads",
					reason:    "Reason",
				},
			},
		},
		{
			name: "invalid:118",
			sourceText: "\n" +
				"import * as imported from './deprecated';\n" +
				"\n" +
				"const foo = imported.reexportedDeprecatedFunctionWithOverloads;\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{
					messageId: "deprecatedWithReason",
					name:      "reexportedDeprecatedFunctionWithOverloads",
					reason:    "Reason",
				},
			},
		},
		{
			name: "invalid:119",
			sourceText: "\n" +
				"import * as imported from './deprecated';\n" +
				"\n" +
				"const { reexportedDeprecatedFunctionWithOverloads } = imported;\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{
					messageId: "deprecatedWithReason",
					name:      "reexportedDeprecatedFunctionWithOverloads",
					reason:    "Reason",
				},
			},
		},
		{
			name: "invalid:120",
			sourceText: "\n" +
				"import { reexportedDeprecatedFunctionWithOverloads } from './deprecated';\n" +
				"\n" +
				"const foo = reexportedDeprecatedFunctionWithOverloads();\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{
					messageId: "deprecatedWithReason",
					name:      "reexportedDeprecatedFunctionWithOverloads",
					reason:    "Reason",
				},
			},
		},
		{
			name: "invalid:121",
			sourceText: "\n" +
				"import * as imported from './deprecated';\n" +
				"\n" +
				"const foo = imported.reexportedDeprecatedFunctionWithOverloads();\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{
					messageId: "deprecatedWithReason",
					name:      "reexportedDeprecatedFunctionWithOverloads",
					reason:    "Reason",
				},
			},
		},
		{
			name: "invalid:122",
			sourceText: "\n" +
				"import { reexportedDeprecatedFunctionWithOverloads } from './deprecated';\n" +
				"\n" +
				"const foo = reexportedDeprecatedFunctionWithOverloads('a');\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{
					messageId: "deprecatedWithReason",
					name:      "reexportedDeprecatedFunctionWithOverloads",
					reason:    "Reason",
				},
			},
		},
		{
			name: "invalid:123",
			sourceText: "\n" +
				"import * as imported from './deprecated';\n" +
				"\n" +
				"const foo = imported.reexportedDeprecatedFunctionWithOverloads('a');\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{
					messageId: "deprecatedWithReason",
					name:      "reexportedDeprecatedFunctionWithOverloads",
					reason:    "Reason",
				},
			},
		},
		{
			name: "invalid:124",
			sourceText: "\n" +
				"import { ClassWithDeprecatedConstructor } from './deprecated';\n" +
				"\n" +
				"const foo = new ClassWithDeprecatedConstructor('a');\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "ClassWithDeprecatedConstructor", reason: ""},
			},
		},
		{
			name: "invalid:125",
			sourceText: "\n" +
				"import * as imported from './deprecated';\n" +
				"\n" +
				"const foo = new imported.ClassWithDeprecatedConstructor('a');\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "ClassWithDeprecatedConstructor", reason: ""},
			},
		},
		{
			name: "invalid:126",
			sourceText: "\n" +
				"import { ReexportedClassWithDeprecatedConstructor } from './deprecated';\n" +
				"\n" +
				"const foo = ReexportedClassWithDeprecatedConstructor;\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecatedWithReason", name: "ReexportedClassWithDeprecatedConstructor", reason: "Reason"},
			},
		},
		{
			name: "invalid:127",
			sourceText: "\n" +
				"import * as imported from './deprecated';\n" +
				"\n" +
				"const foo = imported.ReexportedClassWithDeprecatedConstructor;\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecatedWithReason", name: "ReexportedClassWithDeprecatedConstructor", reason: "Reason"},
			},
		},
		{
			name: "invalid:128",
			sourceText: "\n" +
				"import * as imported from './deprecated';\n" +
				"\n" +
				"const { ReexportedClassWithDeprecatedConstructor } = imported;\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecatedWithReason", name: "ReexportedClassWithDeprecatedConstructor", reason: "Reason"},
			},
		},
		{
			name: "invalid:129",
			sourceText: "\n" +
				"import { ReexportedClassWithDeprecatedConstructor } from './deprecated';\n" +
				"\n" +
				"const foo = ReexportedClassWithDeprecatedConstructor();\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecatedWithReason", name: "ReexportedClassWithDeprecatedConstructor", reason: "Reason"},
			},
		},
		{
			name: "invalid:130",
			sourceText: "\n" +
				"import * as imported from './deprecated';\n" +
				"\n" +
				"const foo = imported.ReexportedClassWithDeprecatedConstructor();\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecatedWithReason", name: "ReexportedClassWithDeprecatedConstructor", reason: "Reason"},
			},
		},
		{
			name: "invalid:131",
			sourceText: "\n" +
				"import { ReexportedClassWithDeprecatedConstructor } from './deprecated';\n" +
				"\n" +
				"const foo = ReexportedClassWithDeprecatedConstructor('a');\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecatedWithReason", name: "ReexportedClassWithDeprecatedConstructor", reason: "Reason"},
			},
		},
		{
			name: "invalid:132",
			sourceText: "\n" +
				"import * as imported from './deprecated';\n" +
				"\n" +
				"const foo = imported.ReexportedClassWithDeprecatedConstructor('a');\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecatedWithReason", name: "ReexportedClassWithDeprecatedConstructor", reason: "Reason"},
			},
		},
		{
			name: "invalid:133",
			sourceText: "\n" +
				"import imported from './deprecated';\n" +
				"\n" +
				"imported;\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "imported", reason: ""},
			},
		},
		{
			name: "invalid:134",
			sourceText: "\n" +
				"async function fn() {\n" +
				"  const d = await import('./deprecated.js');\n" +
				"  d.default.default;\n" +
				"}\n" +
				"      ",
			withDeprecatedFixture: true,
			environmentDependent:  true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "default", reason: ""},
			},
		},
		{
			name: "invalid:164",
			sourceText: "\n" +
				"import { deprecatedFunction } from './deprecated';\n" +
				"\n" +
				"export { deprecatedFunction };\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "deprecatedFunction", reason: ""},
			},
		},
		{
			name: "invalid:165",
			sourceText: "\n" +
				"export { deprecatedFunction } from './deprecated';\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "deprecatedFunction", reason: ""},
			},
		},
		{
			name: "invalid:166",
			sourceText: "\n" +
				"export type { T, U } from './deprecated';\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecatedWithReason", name: "T", reason: "Reason"},
			},
		},
		{
			name: "invalid:167",
			sourceText: "\n" +
				"export { default as foo } from './deprecated';\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "foo", reason: ""},
			},
		},
		{
			name: "invalid:168",
			sourceText: "\n" +
				"export { deprecatedFunction as bar } from './deprecated';\n" +
				"      ",
			withDeprecatedFixture: true,
			expected: []noDeprecatedFinding{
				{messageId: "deprecated", name: "bar", reason: ""},
			},
		},
	}
	runNoDeprecatedCases(test, cases)
}
