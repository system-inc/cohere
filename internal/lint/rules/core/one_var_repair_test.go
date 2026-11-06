package core

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// oneVariableRepairCases are the rows whose `output` upstream pins to a rewritten string.
//
// `output` is the specification for the repair, and it is the only coverage a fixer has: a fixer
// that reports in the right place and rewrites wrongly is indistinguishable, at the message-id
// layer, from a correct one.
//
// These assert a single fix pass, which is what upstream's RuleTester records. The real engine
// loops to a fixed point, and three of these cases converge somewhere else because the second
// pass merges declarations the first one only just created. Asserting the fixed point here would
// disagree with upstream's own expectations on exactly those three.
var oneVariableRepairCases = []oneVariableFixCase{
	{name: "invalid-152", source: "var bar = true, baz = false;", options: "\"never\"",
		wanted: "var bar = true; var baz = false;"},
	{name: "invalid-153", source: "function foo() { var bar = true, baz = false; }", options: "\"never\"",
		wanted: "function foo() { var bar = true; var baz = false; }"},
	{name: "invalid-154", source: "if (foo) { var bar = true, baz = false; }", options: "\"never\"",
		wanted: "if (foo) { var bar = true; var baz = false; }"},
	{name: "invalid-155", source: "switch (foo) { case bar: var baz = true, quux = false; }", options: "\"never\"",
		wanted: "switch (foo) { case bar: var baz = true; var quux = false; }"},
	{name: "invalid-156", source: "switch (foo) { default: var baz = true, quux = false; }", options: "\"never\"",
		wanted: "switch (foo) { default: var baz = true; var quux = false; }"},
	{name: "invalid-157", source: "function foo() { var bar = true; var baz = false; }", options: "\"always\"",
		wanted: "function foo() { var bar = true,  baz = false; }"},
	{
		name:    "invalid-159",
		source:  "function foo() { var foo = true, bar = false; }",
		options: "{\"initialized\": \"never\"}",
		wanted:  "function foo() { var foo = true; var bar = false; }",
	},
	{name: "invalid-160", source: "function foo() { var foo, bar; }", options: "{\"uninitialized\": \"never\"}",
		wanted: "function foo() { var foo; var bar; }"},
	{
		name:    "invalid-161",
		source:  "function foo() { var bar, baz; var a = true; var b = false; var c, d;}",
		options: "{\"uninitialized\": \"always\", \"initialized\": \"never\"}",
		wanted:  "function foo() { var bar, baz; var a = true; var b = false,  c, d;}",
	},
	{
		name:    "invalid-162",
		source:  "function foo() { var bar = true, baz = false; var a; var b; var c = true, d = false; }",
		options: "{\"uninitialized\": \"never\", \"initialized\": \"always\"}",
		wanted:  "function foo() { var bar = true, baz = false; var a; var b,  c = true, d = false; }",
	},
	{
		name:    "invalid-163",
		source:  "function foo() { var bar = true, baz = false; var a, b;}",
		options: "{\"uninitialized\": \"never\", \"initialized\": \"never\"}",
		wanted:  "function foo() { var bar = true; var baz = false; var a; var b;}",
	},
	{
		name:    "invalid-164",
		source:  "function foo() { var bar = true; var baz = false; var a; var b;}",
		options: "{\"uninitialized\": \"always\", \"initialized\": \"always\"}",
		wanted:  "function foo() { var bar = true,  baz = false,  a,  b;}",
	},
	{name: "invalid-165", source: "function foo() { var a = [1, 2, 3]; var [b, c, d] = a; }", options: "\"always\"",
		wanted: "function foo() { var a = [1, 2, 3],  [b, c, d] = a; }"},
	{name: "invalid-166", source: "function foo() { let a = 1; let b = 2; }", options: "\"always\"",
		wanted: "function foo() { let a = 1,  b = 2; }"},
	{name: "invalid-167", source: "function foo() { const a = 1; const b = 2; }", options: "\"always\"",
		wanted: "function foo() { const a = 1,  b = 2; }"},
	{name: "invalid-168", source: "function foo() { let a = 1; let b = 2; }", options: "{\"let\": \"always\"}",
		wanted: "function foo() { let a = 1,  b = 2; }"},
	{name: "invalid-169", source: "function foo() { const a = 1; const b = 2; }", options: "{\"const\": \"always\"}",
		wanted: "function foo() { const a = 1,  b = 2; }"},
	{name: "invalid-170", source: "function foo() { let a = 1, b = 2; }", options: "{\"let\": \"never\"}",
		wanted: "function foo() { let a = 1; let b = 2; }"},
	{name: "invalid-171", source: "function foo() { let a = 1, b = 2; }", options: "{\"initialized\": \"never\"}",
		wanted: "function foo() { let a = 1; let b = 2; }"},
	{name: "invalid-172", source: "function foo() { let a, b; }", options: "{\"uninitialized\": \"never\"}",
		wanted: "function foo() { let a; let b; }"},
	{name: "invalid-173", source: "function foo() { const a = 1, b = 2; }", options: "{\"initialized\": \"never\"}",
		wanted: "function foo() { const a = 1; const b = 2; }"},
	{name: "invalid-174", source: "function foo() { const a = 1, b = 2; }", options: "{\"const\": \"never\"}",
		wanted: "function foo() { const a = 1; const b = 2; }"},
	{name: "invalid-176", source: "var one = 1, two = 2;\nvar three;", options: "\"always\"",
		wanted: "var one = 1, two = 2,\n three;"},
	{name: "invalid-177", source: "var i = [0], j;", options: "{\"initialized\": \"never\"}",
		wanted: "var i = [0]; var j;"},
	{name: "invalid-178", source: "var i = [0], j;", options: "{\"uninitialized\": \"never\"}",
		wanted: "var i = [0]; var j;"},
	{name: "invalid-181", source: "var foo = function() { var bar = true; var baz = false; }", options: "",
		wanted: "var foo = function() { var bar = true,  baz = false; }"},
	{name: "invalid-183", source: "var foo = () => { var bar = true; var baz = false; }", options: "",
		wanted: "var foo = () => { var bar = true,  baz = false; }"},
	{name: "invalid-185", source: "var foo; var bar;", options: "",
		wanted: "var foo,  bar;"},
	{
		name:    "invalid-186",
		source:  "var x = 1, y = 2; for (var z in foo) {}",
		options: "{\"initialized\": \"never\", \"uninitialized\": \"always\"}",
		wanted:  "var x = 1; var y = 2; for (var z in foo) {}",
	},
	{
		name:    "invalid-187",
		source:  "var x = 1, y = 2; for (var z of foo) {}",
		options: "{\"initialized\": \"never\", \"uninitialized\": \"always\"}",
		wanted:  "var x = 1; var y = 2; for (var z of foo) {}",
	},
	{
		name:    "invalid-188",
		source:  "var x; var y; for (var z in foo) {}",
		options: "{\"initialized\": \"never\", \"uninitialized\": \"always\"}",
		wanted:  "var x,  y; for (var z in foo) {}",
	},
	{
		name:    "invalid-189",
		source:  "var x; var y; for (var z of foo) {}",
		options: "{\"initialized\": \"never\", \"uninitialized\": \"always\"}",
		wanted:  "var x,  y; for (var z of foo) {}",
	},
	{
		name:    "invalid-190",
		source:  "var x; for (var y in foo) {var bar = y; var a; for (var z of bar) {}}",
		options: "{\"initialized\": \"never\", \"uninitialized\": \"always\"}",
		wanted:  "var x; for (var y in foo) {var bar = y,  a; for (var z of bar) {}}",
	},
	{
		name:    "invalid-191",
		source:  "var a = 1; var b = 2; var x, y; for (var z of foo) {var c = 3, baz = z; for (var d in baz) {}}",
		options: "{\"initialized\": \"never\", \"uninitialized\": \"always\"}",
		wanted:  "var a = 1; var b = 2; var x, y; for (var z of foo) {var c = 3; var baz = z; for (var d in baz) {}}",
	},
	{name: "invalid-192", source: "var {foo} = 1, [bar] = 2;", options: "{\"initialized\": \"never\"}",
		wanted: "var {foo} = 1; var [bar] = 2;"},
	{name: "invalid-193", source: "const foo = 1,\n    bar = 2;", options: "{\"initialized\": \"never\"}",
		wanted: "const foo = 1;\n    const bar = 2;"},
	{name: "invalid-194", source: "var foo = 1,\n    bar = 2;", options: "{\"initialized\": \"never\"}",
		wanted: "var foo = 1;\n    var bar = 2;"},
	{name: "invalid-195", source: "var foo = 1, // comment\n    bar = 2;", options: "{\"initialized\": \"never\"}",
		wanted: "var foo = 1; // comment\n    var bar = 2;"},
	{name: "invalid-196", source: "var f, k /* test */, l;", options: "\"never\"",
		wanted: "var f; var k /* test */; var l;"},
	{name: "invalid-197", source: "var f,          /* test */ l;", options: "\"never\"",
		wanted: "var f;          /* test */ var l;"},
	{
		name:    "invalid-198",
		source:  "var f, k /* test \n some more comment \n even more */, l = 1, P;",
		options: "\"never\"",
		wanted:  "var f; var k /* test \n some more comment \n even more */; var l = 1; var P;",
	},
	{name: "invalid-199", source: "var a = 1, b = 2", options: "\"never\"",
		wanted: "var a = 1; var b = 2"},
	{
		name:    "invalid-204",
		source:  "const foo = require('foo'); const bar = require('bar');",
		options: "{\"separateRequires\": true, \"const\": \"always\"}",
		wanted:  "const foo = require('foo'),  bar = require('bar');",
	},
	{name: "invalid-205", source: "var a = 1, b; var c;", options: "\"consecutive\"",
		wanted: "var a = 1, b,  c;"},
	{name: "invalid-206", source: "var a = 0, b = 1; var c = 2;", options: "\"consecutive\"",
		wanted: "var a = 0, b = 1,  c = 2;"},
	{name: "invalid-207", source: "let a = 1, b; let c;", options: "\"consecutive\"",
		wanted: "let a = 1, b,  c;"},
	{name: "invalid-208", source: "let a = 0, b = 1; let c = 2;", options: "\"consecutive\"",
		wanted: "let a = 0, b = 1,  c = 2;"},
	{name: "invalid-209", source: "const a = 0, b = 1; const c = 2;", options: "\"consecutive\"",
		wanted: "const a = 0, b = 1,  c = 2;"},
	{name: "invalid-210", source: "const a = 0; var b = 1; var c = 2; const d = 3;", options: "\"consecutive\"",
		wanted: "const a = 0; var b = 1,  c = 2; const d = 3;"},
	{
		name:    "invalid-211",
		source:  "var a = true; var b = false;",
		options: "{\"separateRequires\": true, \"var\": \"always\"}",
		wanted:  "var a = true,  b = false;",
	},
	{name: "invalid-212", source: "const a = 0; let b = 1; let c = 2; const d = 3;", options: "\"consecutive\"",
		wanted: "const a = 0; let b = 1,  c = 2; const d = 3;"},
	{name: "invalid-213", source: "let a = 0; const b = 1; const c = 1; var d = 2;", options: "\"consecutive\"",
		wanted: "let a = 0; const b = 1,  c = 1; var d = 2;"},
	{
		name:    "invalid-214",
		source:  "var a = 0; var b; var c; var d = 1",
		options: "{\"initialized\": \"consecutive\", \"uninitialized\": \"always\"}",
		wanted:  "var a = 0; var b,  c; var d = 1",
	},
	{
		name:    "invalid-215",
		source:  "var a = 0; var b = 1; var c; var d;",
		options: "{\"initialized\": \"consecutive\", \"uninitialized\": \"always\"}",
		wanted:  "var a = 0,  b = 1; var c,  d;",
	},
	{
		name:    "invalid-216",
		source:  "let a = 0; let b; let c; let d = 1;",
		options: "{\"initialized\": \"consecutive\", \"uninitialized\": \"always\"}",
		wanted:  "let a = 0; let b,  c; let d = 1;",
	},
	{
		name:    "invalid-217",
		source:  "let a = 0; let b = 1; let c; let d;",
		options: "{\"initialized\": \"consecutive\", \"uninitialized\": \"always\"}",
		wanted:  "let a = 0,  b = 1; let c,  d;",
	},
	{
		name:    "invalid-218",
		source:  "const a = 0; let b; let c; const d = 1;",
		options: "{\"initialized\": \"consecutive\", \"uninitialized\": \"always\"}",
		wanted:  "const a = 0; let b,  c; const d = 1;",
	},
	{
		name:    "invalid-219",
		source:  "const a = 0; const b = 1; let c; let d;",
		options: "{\"initialized\": \"consecutive\", \"uninitialized\": \"always\"}",
		wanted:  "const a = 0,  b = 1; let c,  d;",
	},
	{
		name:    "invalid-220",
		source:  "var a = 0; var b = 1; var c, d;",
		options: "{\"initialized\": \"consecutive\", \"uninitialized\": \"never\"}",
		wanted:  "var a = 0,  b = 1; var c; var d;",
	},
	{
		name:    "invalid-221",
		source:  "var a = 0; var b, c; var d = 1;",
		options: "{\"initialized\": \"consecutive\", \"uninitialized\": \"never\"}",
		wanted:  "var a = 0; var b; var c; var d = 1;",
	},
	{
		name:    "invalid-222",
		source:  "let a = 0; let b = 1; let c, d;",
		options: "{\"initialized\": \"consecutive\", \"uninitialized\": \"never\"}",
		wanted:  "let a = 0,  b = 1; let c; let d;",
	},
	{
		name:    "invalid-223",
		source:  "let a = 0; let b, c; let d = 1;",
		options: "{\"initialized\": \"consecutive\", \"uninitialized\": \"never\"}",
		wanted:  "let a = 0; let b; let c; let d = 1;",
	},
	{
		name:    "invalid-224",
		source:  "const a = 0; const b = 1; let c, d;",
		options: "{\"initialized\": \"consecutive\", \"uninitialized\": \"never\"}",
		wanted:  "const a = 0,  b = 1; let c; let d;",
	},
	{
		name:    "invalid-225",
		source:  "const a = 0; let b, c; const d = 1;",
		options: "{\"initialized\": \"consecutive\", \"uninitialized\": \"never\"}",
		wanted:  "const a = 0; let b; let c; const d = 1;",
	},
	{
		name:    "invalid-226",
		source:  "var a; var b; var c = 0; var d = 1;",
		options: "{\"uninitialized\": \"consecutive\", \"initialized\": \"always\"}",
		wanted:  "var a,  b; var c = 0,  d = 1;",
	},
	{
		name:    "invalid-227",
		source:  "var a; var b = 0; var c = 1; var d;",
		options: "{\"uninitialized\": \"consecutive\", \"initialized\": \"always\"}",
		wanted:  "var a; var b = 0,  c = 1; var d;",
	},
	{
		name:    "invalid-228",
		source:  "let a; let b; let c = 0; let d = 1;",
		options: "{\"uninitialized\": \"consecutive\", \"initialized\": \"always\"}",
		wanted:  "let a,  b; let c = 0,  d = 1;",
	},
	{
		name:    "invalid-229",
		source:  "let a; let b = 0; let c = 1; let d;",
		options: "{\"uninitialized\": \"consecutive\", \"initialized\": \"always\"}",
		wanted:  "let a; let b = 0,  c = 1; let d;",
	},
	{
		name:    "invalid-230",
		source:  "let a; let b; const c = 0; const d = 1;",
		options: "{\"uninitialized\": \"consecutive\", \"initialized\": \"always\"}",
		wanted:  "let a,  b; const c = 0,  d = 1;",
	},
	{
		name:    "invalid-231",
		source:  "let a; const b = 0; const c = 1; let d;",
		options: "{\"uninitialized\": \"consecutive\", \"initialized\": \"always\"}",
		wanted:  "let a; const b = 0,  c = 1; let d;",
	},
	{
		name:    "invalid-232",
		source:  "var a; var b; var c = 0, d = 1;",
		options: "{\"uninitialized\": \"consecutive\", \"initialized\": \"never\"}",
		wanted:  "var a,  b; var c = 0; var d = 1;",
	},
	{
		name:    "invalid-233",
		source:  "var a; var b = 0, c = 1; var d;",
		options: "{\"uninitialized\": \"consecutive\", \"initialized\": \"never\"}",
		wanted:  "var a; var b = 0; var c = 1; var d;",
	},
	{
		name:    "invalid-234",
		source:  "let a; let b; let c = 0, d = 1;",
		options: "{\"uninitialized\": \"consecutive\", \"initialized\": \"never\"}",
		wanted:  "let a,  b; let c = 0; let d = 1;",
	},
	{
		name:    "invalid-235",
		source:  "let a; let b = 0, c = 1; let d;",
		options: "{\"uninitialized\": \"consecutive\", \"initialized\": \"never\"}",
		wanted:  "let a; let b = 0; let c = 1; let d;",
	},
	{
		name:    "invalid-236",
		source:  "let a; let b; const c = 0, d = 1;",
		options: "{\"uninitialized\": \"consecutive\", \"initialized\": \"never\"}",
		wanted:  "let a,  b; const c = 0; const d = 1;",
	},
	{
		name:    "invalid-237",
		source:  "let a; const b = 0, c = 1; let d;",
		options: "{\"uninitialized\": \"consecutive\", \"initialized\": \"never\"}",
		wanted:  "let a; const b = 0; const c = 1; let d;",
	},
	{name: "invalid-238", source: "var a = 0; var b = 1;", options: "{\"var\": \"consecutive\"}",
		wanted: "var a = 0,  b = 1;"},
	{name: "invalid-239", source: "let a = 0; let b = 1;", options: "{\"let\": \"consecutive\"}",
		wanted: "let a = 0,  b = 1;"},
	{name: "invalid-240", source: "const a = 0; const b = 1;", options: "{\"const\": \"consecutive\"}",
		wanted: "const a = 0,  b = 1;"},
	{
		name:    "invalid-241",
		source:  "let a; let b; const c = 0; const d = 1;",
		options: "{\"let\": \"consecutive\", \"const\": \"always\"}",
		wanted:  "let a,  b; const c = 0,  d = 1;",
	},
	{
		name:    "invalid-242",
		source:  "let a; const b = 0; const c = 1; let d;",
		options: "{\"let\": \"consecutive\", \"const\": \"always\"}",
		wanted:  "let a; const b = 0,  c = 1; let d;",
	},
	{
		name:    "invalid-243",
		source:  "let a; let b; const c = 0, d = 1;",
		options: "{\"let\": \"consecutive\", \"const\": \"never\"}",
		wanted:  "let a,  b; const c = 0; const d = 1;",
	},
	{
		name:    "invalid-244",
		source:  "let a; const b = 0, c = 1; let d;",
		options: "{\"let\": \"consecutive\", \"const\": \"never\"}",
		wanted:  "let a; const b = 0; const c = 1; let d;",
	},
	{
		name:    "invalid-245",
		source:  "const a = 0; const b = 1; let c; let d;",
		options: "{\"const\": \"consecutive\", \"let\": \"always\"}",
		wanted:  "const a = 0,  b = 1; let c,  d;",
	},
	{
		name:    "invalid-246",
		source:  "const a = 0; let b; let c; const d = 1;",
		options: "{\"const\": \"consecutive\", \"let\": \"always\"}",
		wanted:  "const a = 0; let b,  c; const d = 1;",
	},
	{
		name:    "invalid-247",
		source:  "const a = 0; const b = 1; let c, d;",
		options: "{\"const\": \"consecutive\", \"let\": \"never\"}",
		wanted:  "const a = 0,  b = 1; let c; let d;",
	},
	{
		name:    "invalid-248",
		source:  "const a = 0; let b, c; const d = 1;",
		options: "{\"const\": \"consecutive\", \"let\": \"never\"}",
		wanted:  "const a = 0; let b; let c; const d = 1;",
	},
	{name: "invalid-249", source: "var bar; var baz;", options: "\"consecutive\"",
		wanted: "var bar,  baz;"},
	{name: "invalid-250", source: "var bar = 1; var baz = 2; qux(); var qux = 3; var quux;", options: "\"consecutive\"",
		wanted: "var bar = 1,  baz = 2; qux(); var qux = 3,  quux;"},
	{
		name:    "invalid-251",
		source:  "let a, b; let c; var d, e;",
		options: "{\"var\": \"never\", \"let\": \"consecutive\", \"const\": \"consecutive\"}",
		wanted:  "let a, b,  c; var d; var e;",
	},
	{name: "invalid-252", source: "var a; var b;", options: "{\"var\": \"consecutive\"}",
		wanted: "var a,  b;"},
	{
		name:    "invalid-253",
		source:  "var a = 1; var b = 2; var c, d; var e = 3; var f = 4;",
		options: "{\"initialized\": \"consecutive\", \"uninitialized\": \"never\"}",
		wanted:  "var a = 1,  b = 2; var c; var d; var e = 3,  f = 4;",
	},
	{
		name:    "invalid-254",
		source:  "var a = 1; var b = 2; foo(); var c = 3; var d = 4;",
		options: "{\"initialized\": \"consecutive\"}",
		wanted:  "var a = 1,  b = 2; foo(); var c = 3,  d = 4;",
	},
	{name: "invalid-255", source: "var a\nvar b", options: "\"always\"",
		wanted: "var a,\n b"},
	{name: "invalid-256", source: "export const foo=1, bar=2;", options: "\"never\"",
		wanted: "export const foo=1; export const bar=2;"},
	{name: "invalid-257", source: "const foo=1,\n bar=2;", options: "\"never\"",
		wanted: "const foo=1;\n const bar=2;"},
	{name: "invalid-258", source: "export const foo=1,\n bar=2;", options: "\"never\"",
		wanted: "export const foo=1;\n export const bar=2;"},
	{name: "invalid-259", source: "export const foo=1\n, bar=2;", options: "\"never\"",
		wanted: "export const foo=1\n; export const bar=2;"},
	{name: "invalid-260", source: "export const foo= a, bar=2;", options: "\"never\"",
		wanted: "export const foo= a; export const bar=2;"},
	{name: "invalid-261", source: "export const foo=() => a, bar=2;", options: "\"never\"",
		wanted: "export const foo=() => a; export const bar=2;"},
	{name: "invalid-262", source: "export const foo= a, bar=2, bar2=2;", options: "\"never\"",
		wanted: "export const foo= a; export const bar=2; export const bar2=2;"},
	{name: "invalid-263", source: "export const foo = 1,bar = 2;", options: "\"never\"",
		wanted: "export const foo = 1; export const bar = 2;"},
	{name: "invalid-277", source: "class C { static { let x, y; } }", options: "\"never\"",
		wanted: "class C { static { let x; let y; } }"},
	{name: "invalid-278", source: "class C { static { var x, y; } }", options: "\"never\"",
		wanted: "class C { static { var x; var y; } }"},
	{name: "invalid-279", source: "class C { static { let x; let y; } }", options: "\"always\"",
		wanted: "class C { static { let x,  y; } }"},
	{name: "invalid-280", source: "class C { static { var x; var y; } }", options: "\"always\"",
		wanted: "class C { static { var x,  y; } }"},
	{name: "invalid-284", source: "class C { static { let x; let y; } }", options: "\"consecutive\"",
		wanted: "class C { static { let x,  y; } }"},
	{name: "invalid-285", source: "class C { static { var x; var y; } }", options: "\"consecutive\"",
		wanted: "class C { static { var x,  y; } }"},
	{
		name:    "invalid-286",
		source:  "class C { static { let a = 0; let b = 1; } }",
		options: "{\"initialized\": \"consecutive\"}",
		wanted:  "class C { static { let a = 0,  b = 1; } }",
	},
	{
		name:    "invalid-287",
		source:  "class C { static { var a = 0; var b = 1; } }",
		options: "{\"initialized\": \"consecutive\"}",
		wanted:  "class C { static { var a = 0,  b = 1; } }",
	},
	{name: "invalid-288", source: "using a = 0; using b = 1;", options: "\"always\"",
		wanted: "using a = 0,  b = 1;"},
	{name: "invalid-289", source: "await using a = 0; await using b = 1;", options: "\"always\"",
		wanted: "await using a = 0,   b = 1;"},
	{name: "invalid-290", source: "using a = 0, b = 1;", options: "\"never\"",
		wanted: "using a = 0; using b = 1;"},
	{name: "invalid-291", source: "await using a = 0, b = 1;", options: "\"never\"",
		wanted: "await using a = 0; await using b = 1;"},
	{name: "invalid-292", source: "using a = 0; using b = 1;", options: "\"consecutive\"",
		wanted: "using a = 0,  b = 1;"},
	{name: "invalid-293", source: "await using a = 0; await using b = 1;", options: "\"consecutive\"",
		wanted: "await using a = 0,   b = 1;"},
	{name: "invalid-294", source: "using a = 0, b = 1;", options: "{\"initialized\": \"never\"}",
		wanted: "using a = 0; using b = 1;"},
	{name: "invalid-295", source: "await using a = 0, b = 1;", options: "{\"initialized\": \"never\"}",
		wanted: "await using a = 0; await using b = 1;"},
}

// oneVariableDeclineCases are the rows upstream reports and deliberately does not repair.
//
// `output: null` is a decision rather than an omission. Twenty-six of them here, more than any
// other rule in this batch, and they are asserted as the absence of a proposed fix -- never as
// "the output equals the input", because `ExpectFixedSource` fatals when no fix was proposed and
// would therefore agree with any expectation at all.
var oneVariableDeclineCases = []oneVariableCase{
	{name: "invalid-158", source: "var a = 1; for (var b = 2;;) {}", options: "\"always\""},
	{
		name:    "invalid-175",
		source:  "let foo = true; switch(foo) { case true: let bar = 2; break; case false: let baz = 3; break; }",
		options: "{\"var\": \"always\", \"let\": \"always\", \"const\": \"never\"}",
	},
	{name: "invalid-179", source: "for (var x of foo) {}; for (var y of foo) {}", options: "\"always\""},
	{name: "invalid-180", source: "for (var x in foo) {}; for (var y in foo) {}", options: "\"always\""},
	{
		name:    "invalid-182",
		source:  "function foo() { var bar = true; if (qux) { var baz = false; } else { var quxx = 42; } }",
		options: "",
	},
	{
		name:    "invalid-184",
		source:  "var foo = function() { var bar = true; if (qux) { var baz = false; } }",
		options: "",
	},
	{
		name:    "invalid-200",
		source:  "var foo = require('foo'), bar;",
		options: "{\"separateRequires\": true, \"var\": \"always\"}",
	},
	{
		name:    "invalid-201",
		source:  "var foo, bar = require('bar');",
		options: "{\"separateRequires\": true, \"var\": \"always\"}",
	},
	{
		name:    "invalid-202",
		source:  "let foo, bar = require('bar');",
		options: "{\"separateRequires\": true, \"let\": \"always\"}",
	},
	{
		name:    "invalid-203",
		source:  "const foo = 0, bar = require('bar');",
		options: "{\"separateRequires\": true, \"const\": \"always\"}",
	},
	{name: "invalid-264", source: "if (foo) var x, y;", options: "\"never\""},
	{name: "invalid-265", source: "if (foo) var x, y;", options: "{\"var\": \"never\"}"},
	{name: "invalid-266", source: "if (foo) var x, y;", options: "{\"uninitialized\": \"never\"}"},
	{name: "invalid-267", source: "if (foo) var x = 1, y = 1;", options: "{\"initialized\": \"never\"}"},
	{name: "invalid-268", source: "if (foo) {} else var x, y;", options: "\"never\""},
	{name: "invalid-269", source: "while (foo) var x, y;", options: "\"never\""},
	{name: "invalid-270", source: "do var x, y; while (foo);", options: "\"never\""},
	{name: "invalid-271", source: "do var x = f(), y = b(); while (x < y);", options: "\"never\""},
	{name: "invalid-272", source: "for (;;) var x, y;", options: "\"never\""},
	{name: "invalid-273", source: "for (foo in bar) var x, y;", options: "\"never\""},
	{name: "invalid-274", source: "for (foo of bar) var x, y;", options: "\"never\""},
	{name: "invalid-275", source: "with (foo) var x, y;", options: "\"never\""},
	{name: "invalid-276", source: "label: var x, y;", options: "\"never\""},
	{name: "invalid-281", source: "class C { static { let x; foo; let y; } }", options: "\"always\""},
	{name: "invalid-282", source: "class C { static { var x; foo; var y; } }", options: "\"always\""},
	{name: "invalid-283", source: "class C { static { var x; if (foo) { var y; } } }", options: "\"always\""},
}

func TestOneVariableRepairsWhatUpstreamRepairs(test *testing.T) {
	test.Parallel()
	for _, testCase := range oneVariableRepairCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunWithOptions(test, OneVariable, oneVariableFile, testCase.source,
				decodedOneVariable(test, testCase.options))
			rule_testing.ExpectFixedSource(test, result, testCase.wanted)
		})
	}
}

// TestOneVariableDeclinesTheRepairsUpstreamDeclines asserts the absence of a fix.
//
// Not "the output equals the input": `ExpectFixedSource` fatals when no fix was proposed, so
// routing a decline through it would make the assertion agree with any expectation at all. The
// decline withholds the repair, not the report, so the finding must still be there.
func TestOneVariableDeclinesTheRepairsUpstreamDeclines(test *testing.T) {
	test.Parallel()
	for _, testCase := range oneVariableDeclineCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunWithOptions(test, OneVariable, oneVariableFile, testCase.source,
				decodedOneVariable(test, testCase.options))
			if len(result.Diagnostics) == 0 {
				test.Fatalf("expected a finding; a decline withholds the FIX, not the report")
			}
			for _, diagnostic := range result.Diagnostics {
				if len(diagnostic.Fixes) != 0 {
					test.Errorf("proposed %d fix(es); upstream declines this case", len(diagnostic.Fixes))
				}
			}
		})
	}
}
