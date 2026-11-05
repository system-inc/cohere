package typescript

// preferNullishCoalescingOrCases are the invalid cases whose findings are ALL preferNullishOverOr.
//
// The ternary and if-statement paths' 221 cases live in prefer_nullish_coalescing_ternary_cases_test.go
// and prefer_nullish_coalescing_if_cases_test.go, with their spans and suggestions as the installed
// build replayed them. The one left over is `noStrictNullCheck`, which needs a non-strict program.
var preferNullishCoalescingOrCases = []preferNullishCoalescingCase{
	{name: "upstream invalid[0]", source: `
declare let x: string | null | undefined;
x || 'foo';
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[1]", source: `
declare let x: string | null | undefined;
x ||= 'foo';
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[2]", source: `
declare let x: number | null | undefined;
x || 'foo';
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[3]", source: `
declare let x: number | null | undefined;
x ||= 'foo';
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[4]", source: `
declare let x: boolean | null | undefined;
x || 'foo';
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[5]", source: `
declare let x: boolean | null | undefined;
x ||= 'foo';
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[6]", source: `
declare let x: object | null | undefined;
x || 'foo';
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[7]", source: `
declare let x: object | null | undefined;
x ||= 'foo';
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[156]", source: `
declare let x: string | null | undefined;
x || 'foo' ? null : null;
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[157]", source: `
declare let x: string | null | undefined;
(x ||= 'foo') ? null : null;
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[158]", source: `
declare let x: number | null | undefined;
x || 'foo' ? null : null;
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[159]", source: `
declare let x: number | null | undefined;
(x ||= 'foo') ? null : null;
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[160]", source: `
declare let x: boolean | null | undefined;
x || 'foo' ? null : null;
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[161]", source: `
declare let x: boolean | null | undefined;
(x ||= 'foo') ? null : null;
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[162]", source: `
declare let x: object | null | undefined;
x || 'foo' ? null : null;
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[163]", source: `
declare let x: object | null | undefined;
(x ||= 'foo') ? null : null;
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[164]", source: `
declare let x: string | null | undefined;
if (x || 'foo') {
}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[165]", source: `
declare let x: string | null | undefined;
if ((x ||= 'foo')) {
}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[166]", source: `
declare let x: number | null | undefined;
if (x || 'foo') {
}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[167]", source: `
declare let x: number | null | undefined;
if ((x ||= 'foo')) {
}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[168]", source: `
declare let x: boolean | null | undefined;
if (x || 'foo') {
}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[169]", source: `
declare let x: boolean | null | undefined;
if ((x ||= 'foo')) {
}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[170]", source: `
declare let x: object | null | undefined;
if (x || 'foo') {
}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[171]", source: `
declare let x: object | null | undefined;
if ((x ||= 'foo')) {
}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[172]", source: `
declare let x: string | null | undefined;
do {} while (x || 'foo');
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[173]", source: `
declare let x: string | null | undefined;
do {} while ((x ||= 'foo'));
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[174]", source: `
declare let x: number | null | undefined;
do {} while (x || 'foo');
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[175]", source: `
declare let x: number | null | undefined;
do {} while ((x ||= 'foo'));
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[176]", source: `
declare let x: boolean | null | undefined;
do {} while (x || 'foo');
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[177]", source: `
declare let x: boolean | null | undefined;
do {} while ((x ||= 'foo'));
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[178]", source: `
declare let x: object | null | undefined;
do {} while (x || 'foo');
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[179]", source: `
declare let x: object | null | undefined;
do {} while ((x ||= 'foo'));
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[180]", source: `
declare let x: string | null | undefined;
for (; x || 'foo';) {}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[181]", source: `
declare let x: string | null | undefined;
for (; (x ||= 'foo');) {}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[182]", source: `
declare let x: number | null | undefined;
for (; x || 'foo';) {}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[183]", source: `
declare let x: number | null | undefined;
for (; (x ||= 'foo');) {}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[184]", source: `
declare let x: boolean | null | undefined;
for (; x || 'foo';) {}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[185]", source: `
declare let x: boolean | null | undefined;
for (; (x ||= 'foo');) {}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[186]", source: `
declare let x: object | null | undefined;
for (; x || 'foo';) {}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[187]", source: `
declare let x: object | null | undefined;
for (; (x ||= 'foo');) {}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[188]", source: `
declare let x: string | null | undefined;
while (x || 'foo') {}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[189]", source: `
declare let x: string | null | undefined;
while ((x ||= 'foo')) {}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[190]", source: `
declare let x: number | null | undefined;
while (x || 'foo') {}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[191]", source: `
declare let x: number | null | undefined;
while ((x ||= 'foo')) {}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[192]", source: `
declare let x: boolean | null | undefined;
while (x || 'foo') {}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[193]", source: `
declare let x: boolean | null | undefined;
while ((x ||= 'foo')) {}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[194]", source: `
declare let x: object | null | undefined;
while (x || 'foo') {}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[195]", source: `
declare let x: object | null | undefined;
while ((x ||= 'foo')) {}
      `, optionsJson: `{"ignoreConditionalTests": false}`, findings: 1},
	{name: "upstream invalid[196]", source: `
declare let a: string | null | undefined;
declare let b: string | null | undefined;
declare let c: string | null | undefined;
a || (b && c);
      `, optionsJson: `{"ignoreMixedLogicalExpressions": false}`, findings: 1},
	{name: "upstream invalid[197]", source: `
declare let a: number | null | undefined;
declare let b: number | null | undefined;
declare let c: number | null | undefined;
a || (b && c);
      `, optionsJson: `{"ignoreMixedLogicalExpressions": false}`, findings: 1},
	{name: "upstream invalid[198]", source: `
declare let a: boolean | null | undefined;
declare let b: boolean | null | undefined;
declare let c: boolean | null | undefined;
a || (b && c);
      `, optionsJson: `{"ignoreMixedLogicalExpressions": false}`, findings: 1},
	{name: "upstream invalid[199]", source: `
declare let a: object | null | undefined;
declare let b: object | null | undefined;
declare let c: object | null | undefined;
a || (b && c);
      `, optionsJson: `{"ignoreMixedLogicalExpressions": false}`, findings: 1},
	{name: "upstream invalid[200]", source: `
declare let a: string | null | undefined;
declare let b: string | null | undefined;
declare let c: string | null | undefined;
declare let d: string | null | undefined;
a || b || (c && d);
      `, optionsJson: `{"ignoreMixedLogicalExpressions": false}`, findings: 2},
	{name: "upstream invalid[201]", source: `
declare let a: number | null | undefined;
declare let b: number | null | undefined;
declare let c: number | null | undefined;
declare let d: number | null | undefined;
a || b || (c && d);
      `, optionsJson: `{"ignoreMixedLogicalExpressions": false}`, findings: 2},
	{name: "upstream invalid[202]", source: `
declare let a: boolean | null | undefined;
declare let b: boolean | null | undefined;
declare let c: boolean | null | undefined;
declare let d: boolean | null | undefined;
a || b || (c && d);
      `, optionsJson: `{"ignoreMixedLogicalExpressions": false}`, findings: 2},
	{name: "upstream invalid[203]", source: `
declare let a: object | null | undefined;
declare let b: object | null | undefined;
declare let c: object | null | undefined;
declare let d: object | null | undefined;
a || b || (c && d);
      `, optionsJson: `{"ignoreMixedLogicalExpressions": false}`, findings: 2},
	{name: "upstream invalid[204]", source: `
declare let a: string | null | undefined;
declare let b: string | null | undefined;
declare let c: string | null | undefined;
declare let d: string | null | undefined;
(a && b) || c || d;
      `, optionsJson: `{"ignoreMixedLogicalExpressions": false}`, findings: 2},
	{name: "upstream invalid[205]", source: `
declare let a: number | null | undefined;
declare let b: number | null | undefined;
declare let c: number | null | undefined;
declare let d: number | null | undefined;
(a && b) || c || d;
      `, optionsJson: `{"ignoreMixedLogicalExpressions": false}`, findings: 2},
	{name: "upstream invalid[206]", source: `
declare let a: boolean | null | undefined;
declare let b: boolean | null | undefined;
declare let c: boolean | null | undefined;
declare let d: boolean | null | undefined;
(a && b) || c || d;
      `, optionsJson: `{"ignoreMixedLogicalExpressions": false}`, findings: 2},
	{name: "upstream invalid[207]", source: `
declare let a: object | null | undefined;
declare let b: object | null | undefined;
declare let c: object | null | undefined;
declare let d: object | null | undefined;
(a && b) || c || d;
      `, optionsJson: `{"ignoreMixedLogicalExpressions": false}`, findings: 2},
	{name: "upstream invalid[208]", source: `
declare let x: string | null | undefined;
if (() => x || 'foo') {
}
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[209]", source: `
declare let x: string | null | undefined;
if (() => (x ||= 'foo')) {
}
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[210]", source: `
declare let x: number | null | undefined;
if (() => x || 'foo') {
}
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[211]", source: `
declare let x: number | null | undefined;
if (() => (x ||= 'foo')) {
}
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[212]", source: `
declare let x: boolean | null | undefined;
if (() => x || 'foo') {
}
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[213]", source: `
declare let x: boolean | null | undefined;
if (() => (x ||= 'foo')) {
}
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[214]", source: `
declare let x: object | null | undefined;
if (() => x || 'foo') {
}
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[215]", source: `
declare let x: object | null | undefined;
if (() => (x ||= 'foo')) {
}
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[216]", source: `
declare let x: string | null | undefined;
if (
  function weird() {
    return x || 'foo';
  }
) {
}
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[217]", source: `
declare let x: string | null | undefined;
if (
  function weird() {
    return (x ||= 'foo');
  }
) {
}
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[218]", source: `
declare let x: number | null | undefined;
if (
  function weird() {
    return x || 'foo';
  }
) {
}
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[219]", source: `
declare let x: number | null | undefined;
if (
  function weird() {
    return (x ||= 'foo');
  }
) {
}
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[220]", source: `
declare let x: boolean | null | undefined;
if (
  function weird() {
    return x || 'foo';
  }
) {
}
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[221]", source: `
declare let x: boolean | null | undefined;
if (
  function weird() {
    return (x ||= 'foo');
  }
) {
}
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[222]", source: `
declare let x: object | null | undefined;
if (
  function weird() {
    return x || 'foo';
  }
) {
}
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[223]", source: `
declare let x: object | null | undefined;
if (
  function weird() {
    return (x ||= 'foo');
  }
) {
}
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[224]", source: `
declare let a: string | null | undefined;
declare let b: string;
declare let c: string;
a || b || c;
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[225]", source: `
declare let a: number | null | undefined;
declare let b: number;
declare let c: number;
a || b || c;
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[226]", source: `
declare let a: boolean | null | undefined;
declare let b: boolean;
declare let c: boolean;
a || b || c;
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[227]", source: `
declare let a: object | null | undefined;
declare let b: object;
declare let c: object;
a || b || c;
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[228]", source: `
declare let x: string | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true, "boolean": true, "number": true}}`, findings: 1},
	{name: "upstream invalid[229]", source: `
declare let x: number | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true, "boolean": true, "string": true}}`, findings: 1},
	{name: "upstream invalid[230]", source: `
declare let x: boolean | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true, "number": true, "string": true}}`, findings: 1},
	{name: "upstream invalid[231]", source: `
declare let x: bigint | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"boolean": true, "number": true, "string": true}}`, findings: 1},
	{name: "upstream invalid[236]", source: `
declare let x: '' | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true, "boolean": true, "number": true, "string": false}}`, findings: 1},
	{name: "upstream invalid[237]", source: "\ndeclare let x: `` | undefined;\nx || y;\n      ", optionsJson: `{"ignorePrimitives": {"bigint": true, "boolean": true, "number": true, "string": false}}`, findings: 1},
	{name: "upstream invalid[238]", source: `
declare let x: 0 | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true, "boolean": true, "number": false, "string": true}}`, findings: 1},
	{name: "upstream invalid[239]", source: `
declare let x: 0n | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": false, "boolean": true, "number": true, "string": true}}`, findings: 1},
	{name: "upstream invalid[240]", source: `
declare let x: false | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true, "boolean": false, "number": true, "string": true}}`, findings: 1},
	{name: "upstream invalid[246]", source: `
declare let x: 'a' | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true, "boolean": true, "number": true, "string": false}}`, findings: 1},
	{name: "upstream invalid[247]", source: "\ndeclare let x: `hello${'string'}` | undefined;\nx || y;\n      ", optionsJson: `{"ignorePrimitives": {"bigint": true, "boolean": true, "number": true, "string": false}}`, findings: 1},
	{name: "upstream invalid[248]", source: `
declare let x: 1 | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true, "boolean": true, "number": false, "string": true}}`, findings: 1},
	{name: "upstream invalid[249]", source: `
declare let x: 1n | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": false, "boolean": true, "number": true, "string": true}}`, findings: 1},
	{name: "upstream invalid[250]", source: `
declare let x: true | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true, "boolean": false, "number": true, "string": true}}`, findings: 1},
	{name: "upstream invalid[261]", source: `
declare let x: 'a' | 'b' | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true, "boolean": true, "number": true, "string": false}}`, findings: 1},
	{name: "upstream invalid[262]", source: "\ndeclare let x: 'a' | `b` | undefined;\nx || y;\n      ", optionsJson: `{"ignorePrimitives": {"bigint": true, "boolean": true, "number": true, "string": false}}`, findings: 1},
	{name: "upstream invalid[263]", source: `
declare let x: 0 | 1 | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true, "boolean": true, "number": false, "string": true}}`, findings: 1},
	{name: "upstream invalid[264]", source: `
declare let x: 1 | 2 | 3 | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true, "boolean": true, "number": false, "string": true}}`, findings: 1},
	{name: "upstream invalid[265]", source: `
declare let x: 0n | 1n | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": false, "boolean": true, "number": true, "string": true}}`, findings: 1},
	{name: "upstream invalid[266]", source: `
declare let x: 1n | 2n | 3n | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": false, "boolean": true, "number": true, "string": true}}`, findings: 1},
	{name: "upstream invalid[267]", source: `
declare let x: true | false | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true, "boolean": false, "number": true, "string": true}}`, findings: 1},
	{name: "upstream invalid[282]", source: `
declare let x: 0 | 1 | 0n | 1n | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": false, "boolean": true, "number": false, "string": true}}`, findings: 1},
	{name: "upstream invalid[283]", source: `
declare let x: true | false | null | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true, "boolean": false, "number": true, "string": true}}`, findings: 1},
	{name: "upstream invalid[288]", source: `
declare let x: null;
x || y;
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[289]", source: `
const x = undefined;
x || y;
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[290]", source: `
null || y;
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[291]", source: `
undefined || y;
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[292]", source: `
enum Enum {
  A = 0,
  B = 1,
  C = 2,
}
declare let x: Enum | undefined;
x || y;
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[293]", source: `
enum Enum {
  A = 0,
  B = 1,
  C = 2,
}
declare let x: Enum.A | Enum.B | undefined;
x || y;
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[294]", source: `
enum Enum {
  A = 'a',
  B = 'b',
  C = 'c',
}
declare let x: Enum | undefined;
x || y;
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[295]", source: `
enum Enum {
  A = 'a',
  B = 'b',
  C = 'c',
}
declare let x: Enum.A | Enum.B | undefined;
x || y;
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[296]", source: `
let a: string | true | undefined;
let b: string | boolean | undefined;
let c: boolean | undefined;

const x = Boolean(a || b);
      `, optionsJson: `{"ignoreBooleanCoercion": false}`, findings: 1},
	{name: "upstream invalid[297]", source: `
function outer() {
  const Boolean = (x: unknown) => x;

  return (a: string | null, b: string) => Boolean(a || b);
}
      `, optionsJson: `{"ignoreBooleanCoercion": true}`, findings: 1},
	{name: "upstream invalid[298]", source: `
let a: string | true | undefined;
let b: string | boolean | undefined;

const x = String(a || b);
      `, optionsJson: `{"ignoreBooleanCoercion": true}`, findings: 1},
	{name: "upstream invalid[299]", source: `
let a: string | true | undefined;
let b: string | boolean | undefined;

const x = Boolean(() => a || b);
      `, optionsJson: `{"ignoreBooleanCoercion": true}`, findings: 1},
	{name: "upstream invalid[300]", source: `
let a: string | true | undefined;
let b: string | boolean | undefined;

const x = Boolean(function weird() {
  return a || b;
});
      `, optionsJson: `{"ignoreBooleanCoercion": true}`, findings: 1},
	{name: "upstream invalid[301]", source: `
let a: string | true | undefined;
let b: string | boolean | undefined;

declare function f(x: unknown): unknown;

const x = Boolean(f(a || b));
      `, optionsJson: `{"ignoreBooleanCoercion": true}`, findings: 1},
	{name: "upstream invalid[302]", source: `
let a: string | true | undefined;
let b: string | boolean | undefined;

const x = Boolean(1 + (a || b));
      `, optionsJson: `{"ignoreBooleanCoercion": true}`, findings: 1},
	{name: "upstream invalid[305]", source: `
let a: string | true | undefined;
let b: string | boolean | undefined;

declare function f(x: unknown): unknown;

if (f(a || b)) {
}
      `, optionsJson: `{"ignoreConditionalTests": true}`, findings: 1},
	{name: "upstream invalid[306]", source: `
declare const a: string | undefined;
declare const b: string;

if (+(a || b)) {
}
      `, optionsJson: `{"ignoreConditionalTests": true}`, findings: 1},
	{name: "upstream invalid[307]", source: `
interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBox: Box | undefined;

defaultBox || getFallbackBox();
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[310]", source: `
declare const x: any;
declare const y: any;
x || y;
      `, optionsJson: "", findings: 1},
	{name: "upstream invalid[311]", source: `
declare const x: unknown;
declare const y: any;
x || y;
      `, optionsJson: "", findings: 1},
}
