package typescript

// noDeprecatedOrdinaryInvalidCases are the reporting rows of upstream's single-file corpus, run by
// TestNoDeprecatedAgainstUpstreamOrdinaryCases.
var noDeprecatedOrdinaryInvalidCases = []noDeprecatedCase{
	{
		name: "invalid:1",
		sourceText: "\n" +
			"/** @deprecated */ var a = undefined;\n" +
			"a;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:2",
		sourceText: "\n" +
			"/** @deprecated */ export var a = undefined;\n" +
			"a;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:3",
		sourceText: "\n" +
			"/** @deprecated */ let a = undefined;\n" +
			"a;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:4",
		sourceText: "\n" +
			"/** @deprecated */ export let a = undefined;\n" +
			"a;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:5",
		sourceText: "\n" +
			"/** @deprecated */ let aLongName = undefined;\n" +
			"aLongName;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "aLongName", reason: ""},
		},
	},
	{
		name: "invalid:6",
		sourceText: "\n" +
			"/** @deprecated */ const a = { b: 1 };\n" +
			"const c = a;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:7",
		sourceText: "\n" +
			"/** @deprecated Reason. */ const a = { b: 1 };\n" +
			"const c = a;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecatedWithReason", name: "a", reason: "Reason."},
		},
	},
	{
		name: "invalid:8",
		sourceText: "\n" +
			"/** @deprecated */ const a = { b: 1 };\n" +
			"const { c = a } = {};\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:9",
		sourceText: "\n" +
			"/** @deprecated */ const a = { b: 1 };\n" +
			"const [c = a] = [];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:10",
		sourceText: "\n" +
			"/** @deprecated */ const a = { b: 1 };\n" +
			"a;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:11",
		sourceText: "\n" +
			"/** @deprecated */ const a = 'foo';\n" +
			"import(`./path/${a}.js`);\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:12",
		sourceText: "\n" +
			"declare function log(...args: unknown): void;\n" +
			"\n" +
			"/** @deprecated */ const a = { b: 1 };\n" +
			"\n" +
			"log(a);\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:13",
		sourceText: "\n" +
			"/** @deprecated */ const a = { b: 1 };\n" +
			"a.b;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:14",
		sourceText: "\n" +
			"/** @deprecated */ const a = { b: 1 };\n" +
			"a['b'];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:15",
		sourceText: "\n" +
			"/** @deprecated */ const a = { b: 1 };\n" +
			"a?.b;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:16",
		sourceText: "\n" +
			"/** @deprecated */ const a = { b: 1 };\n" +
			"a?.['b'];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:17",
		sourceText: "\n" +
			"/** @deprecated */ const a = { b: { c: 1 } };\n" +
			"a.b.c;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:18",
		sourceText: "\n" +
			"/** @deprecated */ const a = { b: { c: 1 } };\n" +
			"a.b?.c;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:19",
		sourceText: "\n" +
			"/** @deprecated */ const a = { b: { c: 1 } };\n" +
			"a?.b?.c;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:20",
		sourceText: "\n" +
			"/** @deprecated */ const a = { b: { c: 1 } };\n" +
			"a?.['b']?.['c'];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:21",
		sourceText: "\n" +
			"const a = {\n" +
			"  /** @deprecated */ b: { c: 1 },\n" +
			"};\n" +
			"a.b.c;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:22",
		sourceText: "\n" +
			"const a = {\n" +
			"  /** @deprecated */ b: 1,\n" +
			"};\n" +
			"const key = 'b';\n" +
			"a[key];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:23",
		sourceText: "\n" +
			"const a = {\n" +
			"  /** @deprecated */ b: { c: 1 },\n" +
			"};\n" +
			"a['b']['c'];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:24",
		sourceText: "\n" +
			"declare const a: {\n" +
			"  /** @deprecated */ b: { c: 1 };\n" +
			"};\n" +
			"a.b.c;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:25",
		sourceText: "\n" +
			"/** @deprecated */ const a = { b: 1 };\n" +
			"const c = a.b;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:26",
		sourceText: "\n" +
			"/** @deprecated */ const a = { b: 1 };\n" +
			"const { c } = a.b;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:27",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"declare const test: string;\n" +
			"const myObj = {\n" +
			"  prop: test,\n" +
			"  deep: {\n" +
			"    prop: test,\n" +
			"  },\n" +
			"};\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "test", reason: ""},
			{messageId: "deprecated", name: "test", reason: ""},
		},
	},
	{
		name: "invalid:28",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"declare const test: string;\n" +
			"const bar = {\n" +
			"  test,\n" +
			"};\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "test", reason: ""},
		},
	},
	{
		name: "invalid:29",
		sourceText: "\n" +
			"/** @deprecated */ const a = { b: 1 };\n" +
			"const { c = 'd' } = a.b;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:30",
		sourceText: "\n" +
			"/** @deprecated */ const a = { b: 1 };\n" +
			"const { c: d } = a.b;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:31",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"declare const a: string[];\n" +
			"const [b] = [a];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:32",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"class A {}\n" +
			"\n" +
			"new A();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "A", reason: ""},
		},
	},
	{
		name: "invalid:33",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"export class A {}\n" +
			"\n" +
			"new A();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "A", reason: ""},
		},
	},
	{
		name: "invalid:34",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"const A = class {};\n" +
			"\n" +
			"new A();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "A", reason: ""},
		},
	},
	{
		name: "invalid:35",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"declare class A {}\n" +
			"\n" +
			"new A();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "A", reason: ""},
		},
	},
	{
		name: "invalid:36",
		sourceText: "\n" +
			"const A = class {\n" +
			"  /** @deprecated */\n" +
			"  constructor() {}\n" +
			"};\n" +
			"\n" +
			"new A();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "A", reason: ""},
		},
	},
	{
		name: "invalid:37",
		sourceText: "\n" +
			"const A = class {\n" +
			"  /** @deprecated */\n" +
			"  constructor();\n" +
			"  constructor(arg: string);\n" +
			"  constructor(arg?: string) {}\n" +
			"};\n" +
			"\n" +
			"new A();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "A", reason: ""},
		},
	},
	{
		name: "invalid:38",
		sourceText: "\n" +
			"declare const A: {\n" +
			"  /** @deprecated */\n" +
			"  new (): string;\n" +
			"};\n" +
			"\n" +
			"new A();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "A", reason: ""},
		},
	},
	{
		name: "invalid:39",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"declare class A {\n" +
			"  constructor();\n" +
			"}\n" +
			"\n" +
			"new A();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "A", reason: ""},
		},
	},
	{
		name: "invalid:40",
		sourceText: "\n" +
			"class A {\n" +
			"  /** @deprecated */\n" +
			"  b: string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"const { b } = a;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:41",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  b(): string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"a.b;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:42",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  b(): string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"const key = 'b';\n" +
			"\n" +
			"a[key];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:43",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  b(): string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"const key = 'b';\n" +
			"\n" +
			"a[key]();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:44",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  b(): string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"a.b();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:45",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  b: () => string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"a.b;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:46",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  b: () => string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"a['b'];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:47",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  b(): string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"const key = 'b';\n" +
			"\n" +
			"a[`${key}`];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:48",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  computed(): string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"const k1 = 'comp';\n" +
			"const k2 = 'uted';\n" +
			"\n" +
			"a[`${k1}${k2}`];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "computed", reason: ""},
		},
	},
	{
		name: "invalid:49",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  b(): string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"const c = `${a.b}`;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:50",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  b: () => string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"a.b();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:51",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  b: () => string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"a['b']();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:52",
		sourceText: "\n" +
			"interface A {\n" +
			"  /** @deprecated */\n" +
			"  b: () => string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"a.b();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:53",
		sourceText: "\n" +
			"interface A {\n" +
			"  /** @deprecated */\n" +
			"  b: () => string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"const key = 'b';\n" +
			"\n" +
			"a[key];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:54",
		sourceText: "\n" +
			"interface A {\n" +
			"  /** @deprecated */\n" +
			"  b: () => string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"const key = 'b';\n" +
			"\n" +
			"a[key]();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:55",
		sourceText: "\n" +
			"interface A {\n" +
			"  /** @deprecated */\n" +
			"  b: () => string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"a['b']();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:56",
		sourceText: "\n" +
			"class A {\n" +
			"  /** @deprecated */\n" +
			"  b(): string {\n" +
			"    return '';\n" +
			"  }\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"a.b();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:57",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated Use b(value). */\n" +
			"  b(): string;\n" +
			"  b(value: string): string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"a.b();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecatedWithReason", name: "b", reason: "Use b(value)."},
		},
	},
	{
		name: "invalid:58",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  static b: string;\n" +
			"}\n" +
			"\n" +
			"A.b;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:59",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  static b: string;\n" +
			"}\n" +
			"\n" +
			"A['b'];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:60",
		sourceText: "\n" +
			"declare const a: {\n" +
			"  /** @deprecated */\n" +
			"  b: string;\n" +
			"};\n" +
			"\n" +
			"a.b;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:61",
		sourceText: "\n" +
			"declare const a: {\n" +
			"  /** @deprecated */\n" +
			"  b: string;\n" +
			"};\n" +
			"\n" +
			"a['b'];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:62",
		sourceText: "\n" +
			"interface A {\n" +
			"  /** @deprecated */\n" +
			"  b: string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"a.b;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:63",
		sourceText: "\n" +
			"export interface A {\n" +
			"  /** @deprecated */\n" +
			"  b: string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"a.b;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:64",
		sourceText: "\n" +
			"interface A {\n" +
			"  /** @deprecated */\n" +
			"  b: string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"const { b } = a;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:65",
		sourceText: "\n" +
			"type A = {\n" +
			"  /** @deprecated */\n" +
			"  b: string;\n" +
			"};\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"const { b } = a;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:66",
		sourceText: "\n" +
			"export type A = {\n" +
			"  /** @deprecated */\n" +
			"  b: string;\n" +
			"};\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"const { b } = a;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:67",
		sourceText: "\n" +
			"type A = () => {\n" +
			"  /** @deprecated */\n" +
			"  b: string;\n" +
			"};\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"const { b } = a();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:68",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"type A = string[];\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"const [b] = a;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "A", reason: ""},
		},
	},
	{
		name: "invalid:69",
		sourceText: "\n" +
			"namespace A {\n" +
			"  /** @deprecated */\n" +
			"  export const b = '';\n" +
			"}\n" +
			"\n" +
			"A.b;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:70",
		sourceText: "\n" +
			"namespace A {\n" +
			"  /** @deprecated */\n" +
			"  export const b = '';\n" +
			"}\n" +
			"\n" +
			"A['b'];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:71",
		sourceText: "\n" +
			"export namespace A {\n" +
			"  /** @deprecated */\n" +
			"  export const b = '';\n" +
			"}\n" +
			"\n" +
			"A.b;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:72",
		sourceText: "\n" +
			"namespace A {\n" +
			"  /** @deprecated */\n" +
			"  export function b() {}\n" +
			"}\n" +
			"\n" +
			"A.b();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:73",
		sourceText: "\n" +
			"namespace A {\n" +
			"  /** @deprecated */\n" +
			"  export function b() {}\n" +
			"}\n" +
			"\n" +
			"A['b']();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:74",
		sourceText: "\n" +
			"namespace assert {\n" +
			"  export function fail(message?: string | Error): never;\n" +
			"  /** @deprecated since v10.0.0 - use fail([message]) or other assert functions instead. */\n" +
			"  export function fail(actual: unknown, expected: unknown): never;\n" +
			"}\n" +
			"\n" +
			"assert.fail({}, {});\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecatedWithReason", name: "fail", reason: "since v10.0.0 - use fail([message]) or other assert functions instead."},
		},
	},
	{
		name: "invalid:76",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"enum A {\n" +
			"  a,\n" +
			"}\n" +
			"\n" +
			"A.a;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "A", reason: ""},
		},
	},
	{
		name: "invalid:77",
		sourceText: "\n" +
			"enum A {\n" +
			"  /** @deprecated */\n" +
			"  a,\n" +
			"}\n" +
			"\n" +
			"A.a;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:78",
		sourceText: "\n" +
			"enum A {\n" +
			"  /** @deprecated */\n" +
			"  a,\n" +
			"}\n" +
			"\n" +
			"const key = 'a';\n" +
			"\n" +
			"A[key];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:79",
		sourceText: "\n" +
			"enum A {\n" +
			"  /** @deprecated */\n" +
			"  a,\n" +
			"}\n" +
			"\n" +
			"A['a'];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:80",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"function a() {}\n" +
			"\n" +
			"a();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:81",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"function a(): void;\n" +
			"function a() {}\n" +
			"\n" +
			"a();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:82",
		sourceText: "\n" +
			"function a(): void;\n" +
			"/** @deprecated */\n" +
			"function a(value: string): void;\n" +
			"function a(value?: string) {}\n" +
			"\n" +
			"a('');\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:83",
		sourceText: "\n" +
			"type A = {\n" +
			"  (value: 'b'): void;\n" +
			"  /** @deprecated */\n" +
			"  (value: 'c'): void;\n" +
			"};\n" +
			"declare const foo: A;\n" +
			"foo('c');\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "foo", reason: ""},
		},
	},
	{
		name: "invalid:84",
		sourceText: "\n" +
			"function a(\n" +
			"  /** @deprecated */\n" +
			"  b?: boolean,\n" +
			") {\n" +
			"  return b;\n" +
			"}\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:85",
		sourceText: "\n" +
			"export function isTypeFlagSet(\n" +
			"  type: ts.Type,\n" +
			"  flagsToCheck: ts.TypeFlags,\n" +
			"  /** @deprecated This param is not used and will be removed in the future. */\n" +
			"  isReceiver?: boolean,\n" +
			"): boolean {\n" +
			"  const flags = getTypeFlags(type);\n" +
			"\n" +
			"  if (isReceiver && flags & ANY_OR_UNKNOWN) {\n" +
			"    return true;\n" +
			"  }\n" +
			"\n" +
			"  return (flags & flagsToCheck) !== 0;\n" +
			"}\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecatedWithReason", name: "isReceiver", reason: "This param is not used and will be removed in the future."},
		},
	},
	{
		name: "invalid:86",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"declare function a(...args: unknown[]): string;\n" +
			"\n" +
			"a``;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:91",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"export type A = string;\n" +
			"export type B = string;\n" +
			"export type C = string;\n" +
			"\n" +
			"export type D = A | B | C;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "A", reason: ""},
		},
	},
	{
		name: "invalid:92",
		sourceText: "\n" +
			"namespace A {\n" +
			"  /** @deprecated */\n" +
			"  export type B = string;\n" +
			"  export type C = string;\n" +
			"  export type D = string;\n" +
			"}\n" +
			"\n" +
			"export type D = A.B | A.C | A.D;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "B", reason: ""},
		},
	},
	{
		name: "invalid:93",
		sourceText: "\n" +
			"interface Props {\n" +
			"  /** @deprecated */\n" +
			"  anchor: 'foo';\n" +
			"}\n" +
			"declare const x: Props;\n" +
			"const { anchor = '' } = x;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "anchor", reason: ""},
		},
	},
	{
		name: "invalid:94",
		sourceText: "\n" +
			"interface Props {\n" +
			"  /** @deprecated */\n" +
			"  anchor: 'foo';\n" +
			"}\n" +
			"declare const x: { bar: Props };\n" +
			"const {\n" +
			"  bar: { anchor = '' },\n" +
			"} = x;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "anchor", reason: ""},
		},
	},
	{
		name: "invalid:95",
		sourceText: "\n" +
			"interface Props {\n" +
			"  /** @deprecated */\n" +
			"  anchor: 'foo';\n" +
			"}\n" +
			"declare const x: [item: Props];\n" +
			"const [{ anchor = 'bar' }] = x;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "anchor", reason: ""},
		},
	},
	{
		name: "invalid:96",
		sourceText: "\n" +
			"interface Props {\n" +
			"  /** @deprecated */\n" +
			"  foo: Props;\n" +
			"}\n" +
			"declare const x: Props;\n" +
			"const { foo = x } = x;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "foo", reason: ""},
		},
	},
	{
		name: "invalid:135",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"interface Foo {}\n" +
			"\n" +
			"class Bar implements Foo {}\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "Foo", reason: ""},
		},
	},
	{
		name: "invalid:136",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"interface Foo {}\n" +
			"\n" +
			"export class Bar implements Foo {}\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "Foo", reason: ""},
		},
	},
	{
		name: "invalid:137",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"interface Foo {}\n" +
			"\n" +
			"interface Baz {}\n" +
			"\n" +
			"export class Bar implements Baz, Foo {}\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "Foo", reason: ""},
		},
	},
	{
		name: "invalid:138",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"class Foo {}\n" +
			"\n" +
			"export class Bar extends Foo {}\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "Foo", reason: ""},
		},
	},
	{
		name: "invalid:139",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"declare function decorator(constructor: Function);\n" +
			"\n" +
			"@decorator\n" +
			"export class Foo {}\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "decorator", reason: ""},
		},
	},
	{
		name: "invalid:140",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"function a(): object {\n" +
			"  return {};\n" +
			"}\n" +
			"\n" +
			"export default a();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "a", reason: ""},
		},
	},
	{
		name: "invalid:141",
		sourceText: "\n" +
			"class A {\n" +
			"  /** @deprecated */\n" +
			"  constructor() {}\n" +
			"}\n" +
			"\n" +
			"class B extends A {\n" +
			"  constructor() {\n" +
			"    /** should report but does not */\n" +
			"    super();\n" +
			"  }\n" +
			"}\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "super", reason: ""},
		},
	},
	{
		name: "invalid:142",
		sourceText: "\n" +
			"class A {\n" +
			"  /** @deprecated test reason*/\n" +
			"  constructor() {}\n" +
			"}\n" +
			"\n" +
			"class B extends A {\n" +
			"  constructor() {\n" +
			"    /** should report but does not */\n" +
			"    super();\n" +
			"  }\n" +
			"}\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecatedWithReason", name: "super", reason: "test reason"},
		},
	},
	{
		name: "invalid:147",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  accessor b: () => string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"a.b;\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:148",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  accessor b: () => string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"a['b'];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:149",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  accessor b: () => string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"a.b();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:150",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  accessor b: () => string;\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"a['b']();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:151",
		sourceText: "\n" +
			"class A {\n" +
			"  /** @deprecated */\n" +
			"  accessor b = (): string => {\n" +
			"    return '';\n" +
			"  };\n" +
			"}\n" +
			"\n" +
			"declare const a: A;\n" +
			"\n" +
			"a.b();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:152",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  static accessor b: () => string;\n" +
			"}\n" +
			"\n" +
			"A.b();\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:153",
		sourceText: "\n" +
			"class A {\n" +
			"  /** @deprecated */\n" +
			"  #b = () => {};\n" +
			"\n" +
			"  c() {\n" +
			"    this.#b();\n" +
			"  }\n" +
			"}\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "#b", reason: ""},
		},
	},
	{
		name: "invalid:154",
		sourceText: "\n" +
			"const a = {\n" +
			"  /** @deprecated */\n" +
			"  b: 'string',\n" +
			"};\n" +
			"\n" +
			"const c = a['b'];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:155",
		sourceText: "\n" +
			"const a = {\n" +
			"  /** @deprecated */\n" +
			"  b: 'string',\n" +
			"};\n" +
			"const x = 'b';\n" +
			"const c = a[x];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:156",
		sourceText: "\n" +
			"const a = {\n" +
			"  /** @deprecated */\n" +
			"  [2]: 'string',\n" +
			"};\n" +
			"const x = 'b';\n" +
			"const c = a[2];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "2", reason: ""},
		},
	},
	{
		name: "invalid:157",
		sourceText: "\n" +
			"const a = {\n" +
			"  /** @deprecated reason for deprecation */\n" +
			"  b: 'string',\n" +
			"};\n" +
			"\n" +
			"const key = 'b';\n" +
			"const stringKey = key as const;\n" +
			"const c = a[stringKey];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecatedWithReason", name: "b", reason: "reason for deprecation"},
		},
	},
	{
		name: "invalid:158",
		sourceText: "\n" +
			"enum Keys {\n" +
			"  B = 'b',\n" +
			"}\n" +
			"\n" +
			"const a = {\n" +
			"  /** @deprecated reason for deprecation */\n" +
			"  b: 'string',\n" +
			"};\n" +
			"\n" +
			"const key = Keys.B;\n" +
			"const c = a[key];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecatedWithReason", name: "b", reason: "reason for deprecation"},
		},
	},
	{
		name: "invalid:159",
		sourceText: "\n" +
			"declare const Keys: {\n" +
			"  a: 1;\n" +
			"};\n" +
			"\n" +
			"const a = {\n" +
			"  /** @deprecated reason for deprecation */\n" +
			"  [1]: 'string',\n" +
			"};\n" +
			"\n" +
			"const key = Keys.a;\n" +
			"const c = a[key];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecatedWithReason", name: "1", reason: "reason for deprecation"},
		},
	},
	{
		name: "invalid:160",
		sourceText: "\n" +
			"const a = {\n" +
			"  /** @deprecated */\n" +
			"  b: 'string',\n" +
			"};\n" +
			"\n" +
			"const key = `b`;\n" +
			"const c = a[key];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:161",
		sourceText: "\n" +
			"const stringObj = 'b';\n" +
			"const a = {\n" +
			"  /** @deprecated */\n" +
			"  b: 'string',\n" +
			"};\n" +
			"const c = a[stringObj];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:162",
		sourceText: "\n" +
			"declare function x(): 'b';\n" +
			"\n" +
			"const a = {\n" +
			"  /** @deprecated */\n" +
			"  b: 'string',\n" +
			"};\n" +
			"\n" +
			"const c = a[x()];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
	{
		name: "invalid:163",
		sourceText: "\n" +
			"declare const x: { y: 'b' };\n" +
			"\n" +
			"const a = {\n" +
			"  /** @deprecated */\n" +
			"  b: 'string',\n" +
			"};\n" +
			"\n" +
			"const c = a[x.y];\n" +
			"      ",
		expected: []noDeprecatedFinding{
			{messageId: "deprecated", name: "b", reason: ""},
		},
	},
}
