package core

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

func TestNoUseBeforeDefineStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []useBeforeDefineCase{
		{
			name:     "case 0",
			source:   "unresolved",
			fileName: "fixture.ts",
		},
		{
			name:     "case 1",
			source:   "Array",
			fileName: "fixture.ts",
		},
		{
			name:     "case 2",
			source:   "function foo () { arguments; }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 3",
			source:   "var a=10; alert(a);",
			fileName: "fixture.ts",
		},
		{
			name:     "case 4",
			source:   "function b(a) { alert(a); }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 5",
			source:   "Object.hasOwnProperty.call(a);",
			fileName: "fixture.ts",
		},
		{
			name:     "case 6",
			source:   "function a() { alert(arguments);}",
			fileName: "fixture.ts",
		},
		{
			name:       "case 7",
			source:     "a(); function a() { alert(arguments); }",
			fileName:   "fixture.ts",
			rawOptions: "\"nofunc\"",
		},
		{
			name:     "case 8",
			source:   "(() => { var a = 42; alert(a); })();",
			fileName: "fixture.ts",
		},
		{
			name:     "case 9",
			source:   "a(); try { throw new Error() } catch (a) {}",
			fileName: "fixture.ts",
		},
		{
			name:     "case 10",
			source:   "class A {} new A();",
			fileName: "fixture.ts",
		},
		{
			name:     "case 11",
			source:   "var a = 0, b = a;",
			fileName: "fixture.ts",
		},
		{
			name:     "case 12",
			source:   "var {a = 0, b = a} = {};",
			fileName: "fixture.ts",
		},
		{
			name:     "case 13",
			source:   "var [a = 0, b = a] = {};",
			fileName: "fixture.ts",
		},
		{
			name:     "case 14",
			source:   "function foo() { foo(); }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 15",
			source:   "var foo = function() { foo(); };",
			fileName: "fixture.ts",
		},
		{
			name:     "case 16",
			source:   "var a; for (a in a) {}",
			fileName: "fixture.ts",
		},
		{
			name:     "case 17",
			source:   "var a; for (a of a) {}",
			fileName: "fixture.ts",
		},
		{
			name:     "case 18",
			source:   "let a; class C { static { a; } }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 19",
			source:   "class C { static { let a; a; } }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 20",
			source:   "\"use strict\"; a(); { function a() {} }",
			fileName: "fixture.ts",
		},
		{
			name:       "case 21",
			source:     "\"use strict\"; { a(); function a() {} }",
			fileName:   "fixture.ts",
			rawOptions: "\"nofunc\"",
		},
		{
			name:     "case 22",
			source:   "switch (foo) { case 1:  { a(); } default: { let a; }}",
			fileName: "fixture.ts",
		},
		{
			name:     "case 23",
			source:   "a(); { let a = function () {}; }",
			fileName: "fixture.ts",
		},
		{
			name:       "case 24",
			source:     "a(); function a() { alert(arguments); }",
			fileName:   "fixture.ts",
			rawOptions: "{\"functions\": false}",
		},
		{
			name:       "case 25",
			source:     "\"use strict\"; { a(); function a() {} }",
			fileName:   "fixture.ts",
			rawOptions: "{\"functions\": false}",
		},
		{
			name:       "case 26",
			source:     "function foo() { new A(); } class A {};",
			fileName:   "fixture.ts",
			rawOptions: "{\"classes\": false}",
		},
		{
			name:       "case 27",
			source:     "function foo() { bar; } var bar;",
			fileName:   "fixture.ts",
			rawOptions: "{\"variables\": false}",
		},
		{
			name:       "case 28",
			source:     "var foo = () => bar; var bar;",
			fileName:   "fixture.ts",
			rawOptions: "{\"variables\": false}",
		},
		{
			name:       "case 29",
			source:     "class C { static { () => foo; let foo; } }",
			fileName:   "fixture.ts",
			rawOptions: "{\"variables\": false}",
		},
		{
			name:     "case 30",
			source:   "class C extends (class { method() { C; } }) {}",
			fileName: "fixture.ts",
		},
		{
			name:     "case 31",
			source:   "(class extends (class { method() { C; } }) {});",
			fileName: "fixture.ts",
		},
		{
			name:     "case 32",
			source:   "const C = (class extends (class { method() { C; } }) {});",
			fileName: "fixture.ts",
		},
		{
			name:     "case 33",
			source:   "class C extends (class { field = C; }) {}",
			fileName: "fixture.ts",
		},
		{
			name:     "case 34",
			source:   "(class extends (class { field = C; }) {});",
			fileName: "fixture.ts",
		},
		{
			name:     "case 35",
			source:   "const C = (class extends (class { field = C; }) {});",
			fileName: "fixture.ts",
		},
		{
			name:     "case 36",
			source:   "class C { [() => C](){} }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 37",
			source:   "(class C { [() => C](){} });",
			fileName: "fixture.ts",
		},
		{
			name:     "case 38",
			source:   "const C = class { [() => C](){} };",
			fileName: "fixture.ts",
		},
		{
			name:     "case 39",
			source:   "class C { static [() => C](){} }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 40",
			source:   "(class C { static [() => C](){} });",
			fileName: "fixture.ts",
		},
		{
			name:     "case 41",
			source:   "const C = class { static [() => C](){} };",
			fileName: "fixture.ts",
		},
		{
			name:     "case 42",
			source:   "class C { [() => C]; }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 43",
			source:   "(class C { [() => C]; });",
			fileName: "fixture.ts",
		},
		{
			name:     "case 44",
			source:   "const C = class { [() => C]; };",
			fileName: "fixture.ts",
		},
		{
			name:     "case 45",
			source:   "class C { static [() => C]; }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 46",
			source:   "(class C { static [() => C]; });",
			fileName: "fixture.ts",
		},
		{
			name:     "case 47",
			source:   "const C = class { static [() => C]; };",
			fileName: "fixture.ts",
		},
		{
			name:     "case 48",
			source:   "class C { method() { C; } }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 49",
			source:   "(class C { method() { C; } });",
			fileName: "fixture.ts",
		},
		{
			name:     "case 50",
			source:   "const C = class { method() { C; } };",
			fileName: "fixture.ts",
		},
		{
			name:     "case 51",
			source:   "class C { static method() { C; } }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 52",
			source:   "(class C { static method() { C; } });",
			fileName: "fixture.ts",
		},
		{
			name:     "case 53",
			source:   "const C = class { static method() { C; } };",
			fileName: "fixture.ts",
		},
		{
			name:     "case 54",
			source:   "class C { field = C; }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 55",
			source:   "(class C { field = C; });",
			fileName: "fixture.ts",
		},
		{
			name:     "case 56",
			source:   "const C = class { field = C; };",
			fileName: "fixture.ts",
		},
		{
			name:     "case 57",
			source:   "class C { static field = C; }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 58",
			source:   "(class C { static field = C; });",
			fileName: "fixture.ts",
		},
		{
			name:     "case 59",
			source:   "class C { static field = class { static field = C; }; }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 60",
			source:   "(class C { static field = class { static field = C; }; });",
			fileName: "fixture.ts",
		},
		{
			name:     "case 61",
			source:   "class C { field = () => C; }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 62",
			source:   "(class C { field = () => C; });",
			fileName: "fixture.ts",
		},
		{
			name:     "case 63",
			source:   "const C = class { field = () => C; };",
			fileName: "fixture.ts",
		},
		{
			name:     "case 64",
			source:   "class C { static field = () => C; }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 65",
			source:   "(class C { static field = () => C; });",
			fileName: "fixture.ts",
		},
		{
			name:     "case 66",
			source:   "const C = class { static field = () => C; };",
			fileName: "fixture.ts",
		},
		{
			name:     "case 67",
			source:   "class C { field = class extends C {}; }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 68",
			source:   "(class C { field = class extends C {}; });",
			fileName: "fixture.ts",
		},
		{
			name:     "case 69",
			source:   "const C = class { field = class extends C {}; }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 70",
			source:   "class C { static field = class extends C {}; }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 71",
			source:   "(class C { static field = class extends C {}; });",
			fileName: "fixture.ts",
		},
		{
			name:     "case 72",
			source:   "class C { static field = class { [C]; }; }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 73",
			source:   "(class C { static field = class { [C]; }; });",
			fileName: "fixture.ts",
		},
		{
			name:     "case 74",
			source:   "const C = class { static field = class { field = C; }; };",
			fileName: "fixture.ts",
		},
		{
			name:       "case 75",
			source:     "class C { method() { a; } } let a;",
			fileName:   "fixture.ts",
			rawOptions: "{\"variables\": false}",
		},
		{
			name:       "case 76",
			source:     "class C { static method() { a; } } let a;",
			fileName:   "fixture.ts",
			rawOptions: "{\"variables\": false}",
		},
		{
			name:       "case 77",
			source:     "class C { field = a; } let a;",
			fileName:   "fixture.ts",
			rawOptions: "{\"variables\": false}",
		},
		{
			name:       "case 78",
			source:     "class C { field = D; } class D {}",
			fileName:   "fixture.ts",
			rawOptions: "{\"classes\": false}",
		},
		{
			name:       "case 79",
			source:     "class C { field = class extends D {}; } class D {}",
			fileName:   "fixture.ts",
			rawOptions: "{\"classes\": false}",
		},
		{
			name:       "case 80",
			source:     "class C { field = () => a; } let a;",
			fileName:   "fixture.ts",
			rawOptions: "{\"variables\": false}",
		},
		{
			name:       "case 81",
			source:     "class C { static field = () => a; } let a;",
			fileName:   "fixture.ts",
			rawOptions: "{\"variables\": false}",
		},
		{
			name:       "case 82",
			source:     "class C { field = () => D; } class D {}",
			fileName:   "fixture.ts",
			rawOptions: "{\"classes\": false}",
		},
		{
			name:       "case 83",
			source:     "class C { static field = () => D; } class D {}",
			fileName:   "fixture.ts",
			rawOptions: "{\"classes\": false}",
		},
		{
			name:       "case 84",
			source:     "class C { static field = class { field = a; }; } let a;",
			fileName:   "fixture.ts",
			rawOptions: "{\"variables\": false}",
		},
		{
			name:     "case 85",
			source:   "class C { static { C; } }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 86",
			source:   "class C { static { C; } static {} static { C; } }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 87",
			source:   "(class C { static { C; } })",
			fileName: "fixture.ts",
		},
		{
			name:     "case 88",
			source:   "class C { static { class D extends C {} } }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 89",
			source:   "class C { static { (class { static { C } }) } }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 90",
			source:   "class C { static { () => C; } }",
			fileName: "fixture.ts",
		},
		{
			name:     "case 91",
			source:   "(class C { static { () => C; } })",
			fileName: "fixture.ts",
		},
		{
			name:     "case 92",
			source:   "const C = class { static { () => C; } }",
			fileName: "fixture.ts",
		},
		{
			name:       "case 93",
			source:     "class C { static { () => D; } } class D {}",
			fileName:   "fixture.ts",
			rawOptions: "{\"classes\": false}",
		},
		{
			name:       "case 94",
			source:     "class C { static { () => a; } } let a;",
			fileName:   "fixture.ts",
			rawOptions: "{\"variables\": false}",
		},
		{
			name:     "case 95",
			source:   "const C = class C { static { C.x; } }",
			fileName: "fixture.ts",
		},
		{
			name:       "case 96",
			source:     "export { a }; const a = 1;",
			fileName:   "fixture.ts",
			rawOptions: "{\"allowNamedExports\": true}",
		},
		{
			name:       "case 97",
			source:     "export { a as b }; const a = 1;",
			fileName:   "fixture.ts",
			rawOptions: "{\"allowNamedExports\": true}",
		},
		{
			name:       "case 98",
			source:     "export { a, b }; let a, b;",
			fileName:   "fixture.ts",
			rawOptions: "{\"allowNamedExports\": true}",
		},
		{
			name:       "case 99",
			source:     "export { a }; var a;",
			fileName:   "fixture.ts",
			rawOptions: "{\"allowNamedExports\": true}",
		},
		{
			name:       "case 100",
			source:     "export { f }; function f() {}",
			fileName:   "fixture.ts",
			rawOptions: "{\"allowNamedExports\": true}",
		},
		{
			name:       "case 101",
			source:     "export { C }; class C {}",
			fileName:   "fixture.ts",
			rawOptions: "{\"allowNamedExports\": true}",
		},
		{
			name:     "case 102",
			source:   "const App = () => <div/>; <App />;",
			fileName: "fixture.tsx",
		},
		{
			name:     "case 103",
			source:   "let Foo, Bar; <Foo><Bar /></Foo>;",
			fileName: "fixture.tsx",
		},
		{
			name:     "case 104",
			source:   "function App() { return <div/> } <App />;",
			fileName: "fixture.tsx",
		},
		{
			name:       "case 105",
			source:     "<App />; function App() { return <div/> }",
			fileName:   "fixture.tsx",
			rawOptions: "{\"functions\": false}",
		},
		{
			name:     "case 222",
			source:   "\n\ttype foo = 1;\n\tconst x: foo = 1;\n\t\t",
			fileName: "fixture.ts",
		},
		{
			name:     "case 223",
			source:   "\n\ttype foo = 1;\n\ttype bar = foo;\n\t\t",
			fileName: "fixture.ts",
		},
		{
			name:     "case 224",
			source:   "\n\tinterface Foo {}\n\tconst x: Foo = {};\n\t\t",
			fileName: "fixture.ts",
		},
		{
			name:     "case 225",
			source:   "\n\tvar a = 10;\n\talert(a);\n\t\t",
			fileName: "fixture.ts",
		},
		{
			name:     "case 226",
			source:   "\n\tfunction b(a) {\n\t  alert(a);\n\t}\n\t\t",
			fileName: "fixture.ts",
		},
		{
			name:     "case 227",
			source:   "Object.hasOwnProperty.call(a);",
			fileName: "fixture.ts",
		},
		{
			name:     "case 228",
			source:   "\n\tfunction a() {\n\t  alert(arguments);\n\t}\n\t\t",
			fileName: "fixture.ts",
		},
		{
			name:     "case 229",
			source:   "declare function a();",
			fileName: "fixture.ts",
		},
		{
			name:     "case 230",
			source:   "\n\tdeclare class a {\n\t  foo();\n\t}\n\t\t",
			fileName: "fixture.ts",
		},
		{
			name:     "case 231",
			source:   "const updatedAt = data?.updatedAt;",
			fileName: "fixture.ts",
		},
		{
			name:     "case 232",
			source:   "\n\tfunction f() {\n\t  return function t() {};\n\t}\n\tf()?.();\n\t\t",
			fileName: "fixture.ts",
		},
		{
			name:     "case 233",
			source:   "\n\tvar a = { b: 5 };\n\talert(a?.b);\n\t\t",
			fileName: "fixture.ts",
		},
		{
			name:       "case 234",
			source:     "\n\ta();\n\tfunction a() {\n\t  alert(arguments);\n\t}\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "\"nofunc\"",
		},
		{
			name:     "case 235",
			source:   "\n\t(() => {\n\t  var a = 42;\n\t  alert(a);\n\t})();\n\t\t  ",
			fileName: "fixture.ts",
		},
		{
			name:     "case 236",
			source:   "\n\ta();\n\ttry {\n\t  throw new Error();\n\t} catch (a) {}\n\t\t",
			fileName: "fixture.ts",
		},
		{
			name:     "case 237",
			source:   "\n\tclass A {}\n\tnew A();\n\t\t  ",
			fileName: "fixture.ts",
		},
		{
			name:     "case 238",
			source:   "\n\tvar a = 0,\n\t  b = a;\n\t\t",
			fileName: "fixture.ts",
		},
		{
			name:     "case 239",
			source:   "var { a = 0, b = a } = {};",
			fileName: "fixture.ts",
		},
		{
			name:     "case 240",
			source:   "var [a = 0, b = a] = {};",
			fileName: "fixture.ts",
		},
		{
			name:     "case 241",
			source:   "\n\tfunction foo() {\n\t  foo();\n\t}\n\t\t",
			fileName: "fixture.ts",
		},
		{
			name:     "case 242",
			source:   "\n\tvar foo = function () {\n\t  foo();\n\t};\n\t\t",
			fileName: "fixture.ts",
		},
		{
			name:     "case 243",
			source:   "\n\tvar a;\n\tfor (a in a) {\n\t}\n\t\t",
			fileName: "fixture.ts",
		},
		{
			name:     "case 244",
			source:   "\n\tvar a;\n\tfor (a of a) {\n\t}\n\t\t  ",
			fileName: "fixture.ts",
		},
		{
			name:     "case 245",
			source:   "\n\t'use strict';\n\ta();\n\t{\n\t  function a() {}\n\t}\n\t\t  ",
			fileName: "fixture.ts",
		},
		{
			name:       "case 246",
			source:     "\n\t'use strict';\n\t{\n\t  a();\n\t  function a() {}\n\t}\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "\"nofunc\"",
		},
		{
			name:     "case 247",
			source:   "\n\tswitch (foo) {\n\t  case 1: {\n\t\ta();\n\t  }\n\t  default: {\n\t\tlet a;\n\t  }\n\t}\n\t\t  ",
			fileName: "fixture.ts",
		},
		{
			name:     "case 248",
			source:   "\n\ta();\n\t{\n\t  let a = function () {};\n\t}\n\t\t  ",
			fileName: "fixture.ts",
		},
		{
			name:       "case 249",
			source:     "\n\ta();\n\tfunction a() {\n\t  alert(arguments);\n\t}\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"functions\": false}",
		},
		{
			name:       "case 250",
			source:     "\n\t'use strict';\n\t{\n\t  a();\n\t  function a() {}\n\t}\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"functions\": false}",
		},
		{
			name:       "case 251",
			source:     "\n\tfunction foo() {\n\t  new A();\n\t}\n\tclass A {}\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"classes\": false}",
		},
		{
			name:       "case 252",
			source:     "\n\tfunction foo() {\n\t  bar;\n\t}\n\tvar bar;\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"variables\": false}",
		},
		{
			name:       "case 253",
			source:     "\n\tvar foo = () => bar;\n\tvar bar;\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"variables\": false}",
		},
		{
			name:       "case 254",
			source:     "\n\tvar x: Foo = 2;\n\ttype Foo = string | number;\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"typedefs\": false}",
		},
		{
			name:       "case 255",
			source:     "\n\tvar x: Foo = {};\n\tinterface Foo {}\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"typedefs\": false, \"ignoreTypeReferences\": false}",
		},
		{
			name:       "case 256",
			source:     "\n\tlet myVar: String;\n\ttype String = string;\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"typedefs\": false, \"ignoreTypeReferences\": false}",
		},
		{
			name:       "case 257",
			source:     "\n\tinterface Bar {\n\t  type: typeof Foo;\n\t}\n\t\n\tconst Foo = 2;\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"ignoreTypeReferences\": true}",
		},
		{
			name:       "case 258",
			source:     "\n\tinterface Bar {\n\t  type: typeof Foo.FOO;\n\t}\n\t\n\tclass Foo {\n\t  public static readonly FOO = '';\n\t}\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"ignoreTypeReferences\": true}",
		},
		{
			name:       "case 259",
			source:     "\n\tinterface Bar {\n\t  type: typeof Foo.Bar.Baz;\n\t}\n\t\n\tconst Foo = {\n\t  Bar: {\n\t\tBaz: 1,\n\t  },\n\t};\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"ignoreTypeReferences\": true}",
		},
		{
			name:     "case 260",
			source:   "\n\tinterface ITest {\n\t  first: boolean;\n\t  second: string;\n\t  third: boolean;\n\t}\n\t\n\tlet first = () => console.log('first');\n\t\n\texport let second = () => console.log('second');\n\t\n\texport namespace Third {\n\t  export let third = () => console.log('third');\n\t}\n\t\t  ",
			fileName: "fixture.ts",
		},
		{
			name:     "case 261",
			source:   "\n\tfunction test(file: Blob) {\n\t  const slice: typeof file.slice =\n\t\tfile.slice || (file as any).webkitSlice || (file as any).mozSlice;\n\t  return slice;\n\t}\n\t\t",
			fileName: "fixture.ts",
		},
		{
			name:     "case 262",
			source:   "\n\tinterface Foo {\n\t  bar: string;\n\t}\n\tconst bar = 'blah';\n\t\t",
			fileName: "fixture.ts",
		},
		{
			name:       "case 263",
			source:     "\n\tfunction foo(): Foo {\n\t  return Foo.FOO;\n\t}\n\t\n\tenum Foo {\n\t  FOO,\n\t}\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"enums\": false}",
		},
		{
			name:       "case 264",
			source:     "\n\tlet foo: Foo;\n\t\n\tenum Foo {\n\t  FOO,\n\t}\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"enums\": false}",
		},
		{
			name:       "case 265",
			source:     "\n\tclass Test {\n\t  foo(args: Foo): Foo {\n\t\treturn Foo.FOO;\n\t  }\n\t}\n\t\n\tenum Foo {\n\t  FOO,\n\t}\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"enums\": false}",
		},
		{
			name:       "case 266",
			source:     "\n\texport { a };\n\tconst a = 1;\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"allowNamedExports\": true}",
		},
		{
			name:       "case 267",
			source:     "\n\texport { a as b };\n\tconst a = 1;\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"allowNamedExports\": true}",
		},
		{
			name:       "case 268",
			source:     "\n\texport { a, b };\n\tlet a, b;\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"allowNamedExports\": true}",
		},
		{
			name:       "case 269",
			source:     "\n\texport { a };\n\tvar a;\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"allowNamedExports\": true}",
		},
		{
			name:       "case 270",
			source:     "\n\texport { f };\n\tfunction f() {}\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"allowNamedExports\": true}",
		},
		{
			name:       "case 271",
			source:     "\n\texport { C };\n\tclass C {}\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"allowNamedExports\": true}",
		},
		{
			name:       "case 272",
			source:     "\n\texport { Foo };\n\t\n\tenum Foo {\n\t  BAR,\n\t}\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"allowNamedExports\": true}",
		},
		{
			name:       "case 273",
			source:     "\n\texport { Foo };\n\t\n\tnamespace Foo {\n\t  export let bar = () => console.log('bar');\n\t}\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"allowNamedExports\": true}",
		},
		{
			name:       "case 274",
			source:     "\n\texport { Foo, baz };\n\t\n\tenum Foo {\n\t  BAR,\n\t}\n\t\n\tlet baz: Enum;\n\tenum Enum {}\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"allowNamedExports\": true}",
		},
		{
			name:     "case 275",
			source:   "\n\timport * as React from 'react';\n\t\n\t<div />;\n\t\t  ",
			fileName: "fixture.tsx",
		},
		{
			name:     "case 276",
			source:   "\n\timport React from 'react';\n\t\n\t<div />;\n\t\t  ",
			fileName: "fixture.tsx",
		},
		{
			name:     "case 277",
			source:   "\n\timport { h } from 'preact';\n\t\n\t<div />;\n\t\t  ",
			fileName: "fixture.tsx",
		},
		{
			name:     "case 278",
			source:   "\n\tconst React = require('react');\n\t\n\t<div />;\n\t\t  ",
			fileName: "fixture.tsx",
		},
		{
			name:     "case 279",
			source:   "\n\ttype T = (value: unknown) => value is Id;\n\t\t",
			fileName: "fixture.ts",
		},
		{
			name:     "case 280",
			source:   "\n\tglobal.foo = true;\n\t\n\tdeclare global {\n\t  namespace NodeJS {\n\t\tinterface Global {\n\t\t  foo?: boolean;\n\t\t}\n\t  }\n\t}\n\t\t",
			fileName: "fixture.ts",
		},
		{
			name:     "case 281",
			source:   "\n\t@Directive({\n\t  selector: '[rcCidrIpPattern]',\n\t  providers: [\n\t\t{\n\t\t  provide: NG_VALIDATORS,\n\t\t  useExisting: CidrIpPatternDirective,\n\t\t  multi: true,\n\t\t},\n\t  ],\n\t})\n\texport class CidrIpPatternDirective implements Validator {}\n\t\t",
			fileName: "fixture.ts",
		},
		{
			name:       "case 282",
			source:     "\n\t@Directive({\n\t  selector: '[rcCidrIpPattern]',\n\t  providers: [\n\t\t{\n\t\t  provide: NG_VALIDATORS,\n\t\t  useExisting: CidrIpPatternDirective,\n\t\t  multi: true,\n\t\t},\n\t  ],\n\t})\n\texport class CidrIpPatternDirective implements Validator {}\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"classes\": false}",
		},
		{
			name:     "case 283",
			source:   "\n\tclass A {\n\t  constructor(printName) {\n\t\tthis.printName = printName;\n\t  }\n\t\n\t  openPort(printerName = this.printerName) {\n\t\tthis.tscOcx.ActiveXopenport(printerName);\n\t\n\t\treturn this;\n\t  }\n\t}\n\t\t",
			fileName: "fixture.ts",
		},
		{
			name:       "case 284",
			source:     "\n\tconst obj = {\n\t  foo: 'foo-value',\n\t  bar: 'bar-value',\n\t} satisfies {\n\t  [key in 'foo' | 'bar']: `${key}-value`;\n\t};\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"ignoreTypeReferences\": false}",
		},
		{
			name:       "case 285",
			source:     "\n\tconst obj = {\n\t  foo: 'foo-value',\n\t  bar: 'bar-value',\n\t} as {\n\t  [key in 'foo' | 'bar']: `${key}-value`;\n\t};\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"ignoreTypeReferences\": false}",
		},
		{
			name:       "case 286",
			source:     "\n\tconst obj = {\n\t  foo: {\n\t\tfoo: 'foo',\n\t  } as {\n\t\t[key in 'foo' | 'bar']: key;\n\t  },\n\t};\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"ignoreTypeReferences\": false}",
		},
		{
			name:       "case 287",
			source:     "\n\tconst foo = {\n\t  bar: 'bar',\n\t} satisfies {\n\t  bar: typeof baz;\n\t};\n\t\n\tconst baz = '';\n\t\t  ",
			fileName:   "fixture.ts",
			rawOptions: "{\"ignoreTypeReferences\": true}",
		},
		{
			name:     "case 288",
			source:   "\n\tnamespace A.X.Y {}\n\t\n\timport Z = A.X.Y;\n\t\n\tconst X = 23;\n\t\t",
			fileName: "fixture.ts",
		},
		{
			name:     "case 289",
			source:   "\n\t\tnamespace A {\n\t\t\texport namespace X {\n\t\t\t\texport namespace Y {\n\t\t\t\t\texport const foo = 40;\n\t\t\t\t}\n\t\t\t}\n\t\t}\n\n\t\timport Z = A.X.Y;\n\n\t\tconst X = 23;\n\t\t",
			fileName: "fixture.ts",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := runUseBeforeDefineCase(test, testCase)
			rule_testing.ExpectClean(test, result)
		})
	}
}
