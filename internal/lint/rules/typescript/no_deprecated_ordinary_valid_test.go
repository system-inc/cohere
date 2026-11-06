package typescript

// noDeprecatedOrdinaryValidCases are the clean rows of upstream's single-file corpus, run by
// TestNoDeprecatedAgainstUpstreamOrdinaryCases.
var noDeprecatedOrdinaryValidCases = []noDeprecatedCase{
	{
		name:       "valid:0",
		sourceText: "/** @deprecated */ var a;",
		expected:   nil,
	},
	{
		name:       "valid:1",
		sourceText: "/** @deprecated */ var a = 1;",
		expected:   nil,
	},
	{
		name:       "valid:2",
		sourceText: "/** @deprecated */ let a;",
		expected:   nil,
	},
	{
		name:       "valid:3",
		sourceText: "/** @deprecated */ let a = 1;",
		expected:   nil,
	},
	{
		name:       "valid:4",
		sourceText: "/** @deprecated */ const a = 1;",
		expected:   nil,
	},
	{
		name:       "valid:5",
		sourceText: "/** @deprecated */ declare var a: number;",
		expected:   nil,
	},
	{
		name:       "valid:6",
		sourceText: "/** @deprecated */ declare let a: number;",
		expected:   nil,
	},
	{
		name:       "valid:7",
		sourceText: "/** @deprecated */ declare const a: number;",
		expected:   nil,
	},
	{
		name:       "valid:8",
		sourceText: "/** @deprecated */ export var a = 1;",
		expected:   nil,
	},
	{
		name:       "valid:9",
		sourceText: "/** @deprecated */ export let a = 1;",
		expected:   nil,
	},
	{
		name:       "valid:10",
		sourceText: "/** @deprecated */ export const a = 1;",
		expected:   nil,
	},
	{
		name:       "valid:11",
		sourceText: "const [/** @deprecated */ a] = [b];",
		expected:   nil,
	},
	{
		name:       "valid:12",
		sourceText: "const [/** @deprecated */ a] = b;",
		expected:   nil,
	},
	{
		name: "valid:13",
		sourceText: "\n" +
			"const a = {\n" +
			"  b: 1,\n" +
			"  /** @deprecated */ c: 2,\n" +
			"};\n" +
			"\n" +
			"a.b;\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:14",
		sourceText: "\n" +
			"const a = {\n" +
			"  b: 1,\n" +
			"  /** @deprecated */ c: 2,\n" +
			"};\n" +
			"\n" +
			"a['b'];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:15",
		sourceText: "\n" +
			"const a = {\n" +
			"  b: 1,\n" +
			"  /** @deprecated */ c: 2,\n" +
			"};\n" +
			"\n" +
			"a['b' + 'c'];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:16",
		sourceText: "\n" +
			"const a = {\n" +
			"  b: 1,\n" +
			"  /** @deprecated */ c: 2,\n" +
			"};\n" +
			"\n" +
			"const key = 'b';\n" +
			"\n" +
			"a[key];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:17",
		sourceText: "\n" +
			"const a = {\n" +
			"  b: 1,\n" +
			"  /** @deprecated */ c: 2,\n" +
			"};\n" +
			"\n" +
			"a?.b;\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:18",
		sourceText: "\n" +
			"const a = {\n" +
			"  b: 1,\n" +
			"  /** @deprecated */ c: 2,\n" +
			"};\n" +
			"\n" +
			"a?.['b'];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:19",
		sourceText: "\n" +
			"declare const a: {\n" +
			"  b: 1;\n" +
			"  /** @deprecated */ c: 2;\n" +
			"};\n" +
			"\n" +
			"a.b;\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:20",
		sourceText: "\n" +
			"declare const a: {\n" +
			"  b: 1;\n" +
			"  /** @deprecated */ c: 2;\n" +
			"};\n" +
			"\n" +
			"a['b'];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:21",
		sourceText: "\n" +
			"declare const a: {\n" +
			"  b: 1;\n" +
			"  /** @deprecated */ c: 2;\n" +
			"};\n" +
			"\n" +
			"a[`${'b'}`];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:22",
		sourceText: "\n" +
			"declare const a: {\n" +
			"  b: 1;\n" +
			"  /** @deprecated */ c: 2;\n" +
			"};\n" +
			"\n" +
			"const key = 'b';\n" +
			"\n" +
			"a[`${key}`];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:23",
		sourceText: "\n" +
			"declare const a: {\n" +
			"  /** @deprecated */ c: 1;\n" +
			"  cc: 2;\n" +
			"};\n" +
			"\n" +
			"const key = 'c';\n" +
			"\n" +
			"a[`${key + key}`];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:24",
		sourceText: "\n" +
			"declare const a: {\n" +
			"  /** @deprecated */ c: 1;\n" +
			"  cc: 2;\n" +
			"};\n" +
			"\n" +
			"const key = 'c';\n" +
			"\n" +
			"a[`${key}${key}`];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:25",
		sourceText: "\n" +
			"class A {\n" +
			"  b: 1;\n" +
			"  /** @deprecated */ c: 2;\n" +
			"}\n" +
			"\n" +
			"new A().b;\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:26",
		sourceText: "\n" +
			"class A {\n" +
			"  b: 1;\n" +
			"  /** @deprecated */ c: 2;\n" +
			"}\n" +
			"\n" +
			"new A()['b'];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:27",
		sourceText: "\n" +
			"class A {\n" +
			"  b: 1;\n" +
			"  /** @deprecated */ c: 2;\n" +
			"}\n" +
			"const key = 'b';\n" +
			"\n" +
			"new A()[b];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:28",
		sourceText: "\n" +
			"class A {\n" +
			"  c: 1;\n" +
			"}\n" +
			"class B {\n" +
			"  /** @deprecated */ c: 2;\n" +
			"}\n" +
			"\n" +
			"new A()['c'];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:29",
		sourceText: "\n" +
			"class A {\n" +
			"  b: () => {};\n" +
			"  /** @deprecated */ c: () => {};\n" +
			"}\n" +
			"\n" +
			"new A()['b']();\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:30",
		sourceText: "\n" +
			"class A {\n" +
			"  accessor b: 1;\n" +
			"  /** @deprecated */ accessor c: 2;\n" +
			"}\n" +
			"\n" +
			"new A().b;\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:31",
		sourceText: "\n" +
			"class A {\n" +
			"  accessor b: 1;\n" +
			"  /** @deprecated */ accessor c: 2;\n" +
			"}\n" +
			"\n" +
			"new A()['b'];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:32",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  static b: string;\n" +
			"  static c: string;\n" +
			"}\n" +
			"\n" +
			"A.c;\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:33",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  static b: string;\n" +
			"  static c: string;\n" +
			"}\n" +
			"\n" +
			"A['c'];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:34",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  static accessor b: string;\n" +
			"  static accessor c: string;\n" +
			"}\n" +
			"\n" +
			"A.c;\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:35",
		sourceText: "\n" +
			"declare class A {\n" +
			"  /** @deprecated */\n" +
			"  static accessor b: string;\n" +
			"  static accessor c: string;\n" +
			"}\n" +
			"\n" +
			"A['c'];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:36",
		sourceText: "\n" +
			"namespace A {\n" +
			"  /** @deprecated */\n" +
			"  export const b = '';\n" +
			"  export const c = '';\n" +
			"}\n" +
			"\n" +
			"A.c;\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:37",
		sourceText: "\n" +
			"namespace A {\n" +
			"  /** @deprecated */\n" +
			"  export const b = '';\n" +
			"  export const c = '';\n" +
			"}\n" +
			"\n" +
			"A['c'];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:38",
		sourceText: "\n" +
			"enum A {\n" +
			"  /** @deprecated */\n" +
			"  b = 'b',\n" +
			"  c = 'c',\n" +
			"}\n" +
			"\n" +
			"A.c;\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:39",
		sourceText: "\n" +
			"enum A {\n" +
			"  /** @deprecated */\n" +
			"  b = 'b',\n" +
			"  c = 'c',\n" +
			"}\n" +
			"\n" +
			"A['c'];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:40",
		sourceText: "\n" +
			"function a(value: 'b' | undefined): void;\n" +
			"/** @deprecated */\n" +
			"function a(value: 'c' | undefined): void;\n" +
			"function a(value: string | undefined): void {\n" +
			"  // ...\n" +
			"}\n" +
			"\n" +
			"a('b');\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:41",
		sourceText: "\n" +
			"function a(value: 'b' | undefined): void;\n" +
			"/** @deprecated */\n" +
			"function a(value: 'c' | undefined): void;\n" +
			"function a(value: string | undefined): void {\n" +
			"  // ...\n" +
			"}\n" +
			"\n" +
			"export default a('b');\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:42",
		sourceText: "\n" +
			"function notDeprecated(): object {\n" +
			"  return {};\n" +
			"}\n" +
			"\n" +
			"export default notDeprecated();\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:47",
		sourceText: "\n" +
			"class A {\n" +
			"  a(value: 'b'): void;\n" +
			"  /** @deprecated */\n" +
			"  a(value: 'c'): void;\n" +
			"}\n" +
			"declare const foo: A;\n" +
			"foo.a('b');\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:48",
		sourceText: "\n" +
			"const A = class {\n" +
			"  /** @deprecated */\n" +
			"  constructor();\n" +
			"  constructor(arg: string);\n" +
			"  constructor(arg?: string) {}\n" +
			"};\n" +
			"\n" +
			"new A('a');\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:49",
		sourceText: "\n" +
			"type A = {\n" +
			"  (value: 'b'): void;\n" +
			"  /** @deprecated */\n" +
			"  (value: 'c'): void;\n" +
			"};\n" +
			"declare const foo: A;\n" +
			"foo('b');\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:50",
		sourceText: "\n" +
			"declare const a: {\n" +
			"  new (value: 'b'): void;\n" +
			"  /** @deprecated */\n" +
			"  new (value: 'c'): void;\n" +
			"};\n" +
			"new a('b');\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:51",
		sourceText: "\n" +
			"namespace assert {\n" +
			"  export function fail(message?: string | Error): never;\n" +
			"  /** @deprecated since v10.0.0 - use fail([message]) or other assert functions instead. */\n" +
			"  export function fail(actual: unknown, expected: unknown): never;\n" +
			"}\n" +
			"\n" +
			"assert.fail('');\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:53",
		sourceText: "\n" +
			"declare module 'deprecations' {\n" +
			"  /** @deprecated */\n" +
			"  export const value = true;\n" +
			"}\n" +
			"\n" +
			"import { value } from 'deprecations';\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:54",
		sourceText: "\n" +
			"/** @deprecated Use ts directly. */\n" +
			"export * as ts from 'typescript';\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:55",
		sourceText: "\n" +
			"export {\n" +
			"  /** @deprecated Use ts directly. */\n" +
			"  default as ts,\n" +
			"} from 'typescript';\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:58",
		sourceText: "\n" +
			"namespace A {\n" +
			"  /** @deprecated */\n" +
			"  export type B = string;\n" +
			"  export type C = string;\n" +
			"  export type D = string;\n" +
			"}\n" +
			"\n" +
			"export type D = A.C | A.D;\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:59",
		sourceText: "\n" +
			"interface Props {\n" +
			"  anchor: 'foo';\n" +
			"}\n" +
			"declare const x: Props;\n" +
			"const { anchor = '' } = x;\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:60",
		sourceText: "\n" +
			"namespace Foo {}\n" +
			"\n" +
			"/**\n" +
			" * @deprecated\n" +
			" */\n" +
			"export import Bar = Foo;\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:62",
		sourceText: "\n" +
			"interface Props {\n" +
			"  anchor: 'foo';\n" +
			"}\n" +
			"declare const x: { bar: Props };\n" +
			"const {\n" +
			"  bar: { anchor = '' },\n" +
			"} = x;\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:63",
		sourceText: "\n" +
			"interface Props {\n" +
			"  anchor: 'foo';\n" +
			"}\n" +
			"declare const x: [item: Props];\n" +
			"const [{ anchor = 'bar' }] = x;\n" +
			"    ",
		expected: nil,
	},
	{
		name:       "valid:64",
		sourceText: "function fn(/** @deprecated */ foo = 4) {}",
		expected:   nil,
	},
	{
		name:       "valid:66",
		sourceText: "call();",
		expected:   nil,
	},
	{
		name: "valid:67",
		sourceText: "\n" +
			"class Foo implements Foo {\n" +
			"  get bar(): number {\n" +
			"    return 42;\n" +
			"  }\n" +
			"\n" +
			"  baz(): number {\n" +
			"    return this.bar;\n" +
			"  }\n" +
			"}\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:73",
		sourceText: "\n" +
			"export {\n" +
			"  /** @deprecated */\n" +
			"  foo,\n" +
			"};\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:75",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"declare class A {}\n" +
			"\n" +
			"new A();\n" +
			"      ",
		options:  "{\"allow\":[{\"from\":\"file\",\"name\":\"A\"}]}",
		expected: nil,
	},
	{
		name: "valid:76",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"const deprecatedValue = 45;\n" +
			"const bar = deprecatedValue;\n" +
			"      ",
		options:  "{\"allow\":[{\"from\":\"file\",\"name\":\"deprecatedValue\"}]}",
		expected: nil,
	},
	{
		name: "valid:77",
		sourceText: "\n" +
			"class MyClass {\n" +
			"  /** @deprecated */\n" +
			"  #privateProp = 42;\n" +
			"  value = this.#privateProp;\n" +
			"}\n" +
			"      ",
		options:  "{\"allow\":[{\"from\":\"file\",\"name\":\"privateProp\"}]}",
		expected: nil,
	},
	{
		name: "valid:78",
		sourceText: "\n" +
			"/** @deprecated */\n" +
			"const deprecatedValue = 45;\n" +
			"const bar = deprecatedValue;\n" +
			"      ",
		options:  "{\"allow\":[\"deprecatedValue\"]}",
		expected: nil,
	},
	{
		name: "valid:80",
		sourceText: "\n" +
			"const { exists } = import('fs');\n" +
			"exists('/foo');\n" +
			"      ",
		options:  "{\"allow\":[{\"from\":\"package\",\"name\":\"exists\",\"package\":\"fs\"}]}",
		expected: nil,
	},
	{
		name: "valid:81",
		sourceText: "\n" +
			"declare const test: string;\n" +
			"const bar = { test };\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:82",
		sourceText: "\n" +
			"const a = {\n" +
			"  /** @deprecated */\n" +
			"  b: 'string',\n" +
			"};\n" +
			"\n" +
			"const complex = Symbol() as any;\n" +
			"const c = a[complex];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:83",
		sourceText: "\n" +
			"const a = {\n" +
			"  b: 'string',\n" +
			"};\n" +
			"\n" +
			"const c = a['b'];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:84",
		sourceText: "\n" +
			"const a = {\n" +
			"  /** @deprecated */\n" +
			"  b: 'string',\n" +
			"};\n" +
			"\n" +
			"const key = {};\n" +
			"const c = a[key as any];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:85",
		sourceText: "\n" +
			"const a = {\n" +
			"  /** @deprecated */\n" +
			"  b: 'string',\n" +
			"};\n" +
			"\n" +
			"const key = Symbol();\n" +
			"const c = a[key as any];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:86",
		sourceText: "\n" +
			"const a = {\n" +
			"  /** @deprecated */\n" +
			"  b: 'string',\n" +
			"};\n" +
			"\n" +
			"const key = undefined;\n" +
			"const c = a[key as any];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:87",
		sourceText: "\n" +
			"const a = {\n" +
			"  /** @deprecated */\n" +
			"  b: 'string',\n" +
			"};\n" +
			"\n" +
			"const c = a['nonExistentProperty'];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:88",
		sourceText: "\n" +
			"const a = {\n" +
			"  /** @deprecated */\n" +
			"  b: 'string',\n" +
			"};\n" +
			"\n" +
			"function getKey() {\n" +
			"  return 'c';\n" +
			"}\n" +
			"\n" +
			"const c = a[getKey()];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:89",
		sourceText: "\n" +
			"const a = {\n" +
			"  /** @deprecated */\n" +
			"  b: 'string',\n" +
			"};\n" +
			"\n" +
			"const key = {};\n" +
			"const c = a[key];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:90",
		sourceText: "\n" +
			"const stringObj = new String('b');\n" +
			"const a = {\n" +
			"  /** @deprecated */\n" +
			"  b: 'string',\n" +
			"};\n" +
			"const c = a[stringObj];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:91",
		sourceText: "\n" +
			"const a = {\n" +
			"  /** @deprecated */\n" +
			"  b: 'string',\n" +
			"};\n" +
			"\n" +
			"const key = Symbol('key');\n" +
			"const c = a[key];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:92",
		sourceText: "\n" +
			"const a = {\n" +
			"  /** @deprecated */\n" +
			"  b: 'string',\n" +
			"};\n" +
			"\n" +
			"const key = null;\n" +
			"const c = a[key as any];\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:93",
		sourceText: "\n" +
			"interface Foo {\n" +
			"  /** @deprecated */\n" +
			"  deprecatedProperty: string;\n" +
			"  notDeprecatedProperty: string;\n" +
			"}\n" +
			"\n" +
			"declare const a: Foo;\n" +
			"const { notDeprecatedProperty: deprecatedProperty } = a;\n" +
			"    ",
		expected: nil,
	},
	{
		name: "valid:94",
		sourceText: "\n" +
			"interface Foo {\n" +
			"  /** @deprecated */\n" +
			"  deprecatedProperty: string;\n" +
			"  notDeprecatedProperty: string;\n" +
			"}\n" +
			"\n" +
			"declare const a: { foo: Foo };\n" +
			"const {\n" +
			"  foo: { notDeprecatedProperty: deprecatedProperty },\n" +
			"} = a;\n" +
			"    ",
		expected: nil,
	},
}
