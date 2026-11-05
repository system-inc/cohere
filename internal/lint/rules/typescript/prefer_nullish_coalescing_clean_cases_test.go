package typescript

// preferNullishCoalescingCleanCases is upstream's whole `valid` list, verbatim.
var preferNullishCoalescingCleanCases = []preferNullishCoalescingCase{
	{name: "upstream valid[0]", source: `
declare let x: string;
x || 'foo';
    `, optionsJson: ""},
	{name: "upstream valid[1]", source: `
declare let x: number;
x || 'foo';
    `, optionsJson: ""},
	{name: "upstream valid[2]", source: `
declare let x: boolean;
x || 'foo';
    `, optionsJson: ""},
	{name: "upstream valid[3]", source: `
declare let x: object;
x || 'foo';
    `, optionsJson: ""},
	{name: "upstream valid[4]", source: `
declare let x: string;
x ||= 'foo';
    `, optionsJson: ""},
	{name: "upstream valid[5]", source: `
declare let x: number;
x ||= 'foo';
    `, optionsJson: ""},
	{name: "upstream valid[6]", source: `
declare let x: boolean;
x ||= 'foo';
    `, optionsJson: ""},
	{name: "upstream valid[7]", source: `
declare let x: object;
x ||= 'foo';
    `, optionsJson: ""},
	{name: "upstream valid[8]", source: `
declare let x: string | null | undefined;
x ?? 'foo';
    `, optionsJson: ""},
	{name: "upstream valid[9]", source: `
declare let x: string | null | undefined;
x ??= 'foo';
    `, optionsJson: ""},
	{name: "upstream valid[10]", source: `
declare let x: number | null | undefined;
x ?? 'foo';
    `, optionsJson: ""},
	{name: "upstream valid[11]", source: `
declare let x: number | null | undefined;
x ??= 'foo';
    `, optionsJson: ""},
	{name: "upstream valid[12]", source: `
declare let x: boolean | null | undefined;
x ?? 'foo';
    `, optionsJson: ""},
	{name: "upstream valid[13]", source: `
declare let x: boolean | null | undefined;
x ??= 'foo';
    `, optionsJson: ""},
	{name: "upstream valid[14]", source: `
declare let x: object | null | undefined;
x ?? 'foo';
    `, optionsJson: ""},
	{name: "upstream valid[15]", source: `
declare let x: object | null | undefined;
x ??= 'foo';
    `, optionsJson: ""},
	{name: "upstream valid[16]", source: `x !== undefined && x !== null ? x : y;`, optionsJson: `{"ignoreTernaryTests": true}`},
	{name: "upstream valid[17]", source: `x !== undefined && x !== null ? 'foo' : 'bar';`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[18]", source: `x !== null && x !== undefined && x !== 5 ? x : y;`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[19]", source: `x === null || x === undefined || x === 5 ? x : y;`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[20]", source: `x === undefined && x !== null ? x : y;`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[21]", source: `x === undefined && x === null ? x : y;`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[22]", source: `x !== undefined && x === null ? x : y;`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[23]", source: `x === undefined || x !== null ? x : y;`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[24]", source: `x === undefined || x === null ? x : y;`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[25]", source: `x !== undefined || x === null ? x : y;`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[26]", source: `x !== undefined || x === null ? y : x;`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[27]", source: `x === null || x === null ? y : x;`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[28]", source: `x === undefined || x === undefined ? y : x;`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[29]", source: `x == null ? x : y;`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[30]", source: `undefined == null ? x : y;`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[31]", source: `undefined != z ? x : y;`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[32]", source: `x == undefined ? x : y;`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[33]", source: `x != null ? y : x;`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[34]", source: `x != undefined ? y : x;`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[35]", source: `null == x ? x : y;`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[36]", source: `undefined == x ? x : y;`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[37]", source: `null != x ? y : x;`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[38]", source: `undefined != x ? y : x;`, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[39]", source: `
declare let x: number | undefined;
x !== 15 && x !== undefined ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[40]", source: `
declare let x: number | undefined;
x !== undefined && x !== 15 ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[41]", source: `
declare let x: number | undefined;
15 !== x && undefined !== x ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[42]", source: `
declare let x: number | undefined;
undefined !== x && 15 !== x ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[43]", source: `
declare let x: number | undefined;
15 !== x && x !== undefined ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[44]", source: `
declare let x: number | undefined;
undefined !== x && x !== 15 ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[45]", source: `
declare let x: string | undefined;
x !== 'foo' && x !== undefined ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[46]", source: `
function test(value: number | undefined): number {
  return value !== foo() && value !== undefined ? value : 1;
}
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[47]", source: `
const test = (value: boolean | undefined): boolean =>
  value !== undefined && value !== false ? value : false;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[48]", source: `
declare let x: string;
x === null ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[49]", source: `
declare let x: string | undefined | null;
x === null ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[50]", source: `
declare let x: string | undefined | null;
x === undefined ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[51]", source: `
declare let x: string | undefined | null;
x !== null ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[52]", source: `
declare let x: string | undefined | null;
x !== undefined ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[53]", source: `
declare let x: any;
x === null ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[54]", source: `
declare let x: unknown;
x === null ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[55]", source: `
declare let x: string;
x ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[56]", source: `
declare let x: string;
!x ? y : x;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[57]", source: `
declare let x: string | object;
x ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[58]", source: `
declare let x: string | object;
!x ? y : x;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[59]", source: `
declare let x: number;
x ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[60]", source: `
declare let x: number;
!x ? y : x;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[61]", source: `
declare let x: bigint;
x ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[62]", source: `
declare let x: bigint;
!x ? y : x;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[63]", source: `
declare let x: boolean;
x ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[64]", source: `
declare let x: boolean;
!x ? y : x;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[65]", source: `
declare let x: object;
x ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[66]", source: `
declare let x: object;
!x ? y : x;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[67]", source: `
declare let x: string[];
x ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[68]", source: `
declare let x: string[];
!x ? y : x;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[69]", source: `
declare let x: Function;
x ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[70]", source: `
declare let x: Function;
!x ? y : x;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[71]", source: `
declare let x: () => string;
x ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[72]", source: `
declare let x: () => string;
!x ? y : x;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[73]", source: `
declare let x: () => string | null | undefined;
x ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[74]", source: `
declare let x: () => string | null | undefined;
!x ? y : x;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[75]", source: `
declare let x: () => string | null;
x() ? x() : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[76]", source: `
declare let x: () => string | null;
!x() ? y : x();
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[77]", source: `
const a = 'foo';
declare let x: (a: string | null) => string | null;
x(a) ? x(a) : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[78]", source: `
const a = 'foo';
declare let x: (a: string | null) => string | null;
!x(a) ? y : x(a);
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[79]", source: `
declare let x: { n: string };
x.n ? x.n : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[80]", source: `
declare let x: { n: string };
!x.n ? y : x.n;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[81]", source: `
declare let x: { n: string | object };
x.n ? x.n : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[82]", source: `
declare let x: { n: string | object };
!x.n ? y : x.n;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[83]", source: `
declare let x: { n: number };
x.n ? x.n : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[84]", source: `
declare let x: { n: number };
!x.n ? y : x.n;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[85]", source: `
declare let x: { n: bigint };
x.n ? x.n : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[86]", source: `
declare let x: { n: bigint };
!x.n ? y : x.n;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[87]", source: `
declare let x: { n: boolean };
x.n ? x.n : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[88]", source: `
declare let x: { n: boolean };
!x.n ? y : x.n;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[89]", source: `
declare let x: { n: object };
x ? x : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[90]", source: `
declare let x: { n: object };
!x ? y : x;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[91]", source: `
declare let x: { n: string[] };
x.n ? x.n : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[92]", source: `
declare let x: { n: string[] };
!x.n ? y : x.n;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[93]", source: `
declare let x: { n: Function };
x.n ? x.n : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[94]", source: `
declare let x: { n: Function };
!x.n ? y : x.n;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[95]", source: `
declare let x: { n: () => string };
x.n ? x.n : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[96]", source: `
declare let x: { n: () => string };
!x.n ? y : x.n;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[97]", source: `
declare let x: { n: () => string | null | undefined };
x.n ? x.n : y;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[98]", source: `
declare let x: { n: () => string | null | undefined };
!x.n ? y : x.n;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[99]", source: `
declare let foo: string;
declare function makeFoo(): string;

function lazyInitialize() {
  if (!foo) {
    foo = makeFoo();
  }
}
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[100]", source: `
declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  if (foo) {
    foo = makeFoo();
  }
}
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[101]", source: `
declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  if (foo != null) {
    foo = makeFoo();
  }
}
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[102]", source: `
declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  if (foo == null) {
    foo = makeFoo();
    return foo;
  }
}
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[103]", source: `
declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  if (foo == null) {
    foo = makeFoo();
  } else {
    return 'bar';
  }
}
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[104]", source: `
declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  if (foo == null) {
    foo = makeFoo();
  } else if (foo.a) {
    return 'bar';
  }
}
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[105]", source: `
declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };
function shadowed() {
  if (foo == null) {
    const foo = makeFoo();
  }
}
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[106]", source: `
declare let foo: { foo: string } | null;
declare function makeFoo(): { foo: { foo: string } };
function weirdDestructuringAssignment() {
  if (foo == null) {
    ({ foo } = makeFoo());
  }
}
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[107]", source: `
declare const nullOrObject: null | { a: string };

const test = nullOrObject !== undefined && null !== null ? nullOrObject : 42;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[108]", source: `
declare const nullOrObject: null | { a: string };

const test = nullOrObject !== undefined && null != null ? nullOrObject : 42;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[109]", source: `
declare const nullOrObject: null | { a: string };

const test =
  nullOrObject !== undefined && null != undefined ? nullOrObject : 42;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[110]", source: `
declare const nullOrObject: null | { a: string };

const test = nullOrObject === undefined || null === null ? 42 : nullOrObject;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[111]", source: `
declare const nullOrObject: null | { a: string };

const test = nullOrObject === undefined || null == null ? 42 : nullOrObject;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[112]", source: `
declare const nullOrObject: null | { a: string };

const test =
  nullOrObject === undefined || null == undefined ? 42 : nullOrObject;
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[113]", source: `
const a = 'b';
declare let x: { a: string; b: string } | null;

x?.a != null ? x[a] : 'foo';
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[114]", source: `
const a = 'b';
declare let x: { a: string; b: string } | null;

x?.[a] != null ? x.a : 'foo';
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[115]", source: `
declare let x: { a: string } | null;
declare let y: { a: string } | null;

x?.a ? y?.a : 'foo';
      `, optionsJson: `{"ignoreTernaryTests": false}`},
	{name: "upstream valid[116]", source: `
declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  if (!foo) {
    foo = makeFoo();
  }
}
      `, optionsJson: `{"ignoreIfStatements": true}`},
	{name: "upstream valid[117]", source: `
declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  if (!foo) foo = makeFoo();
}
      `, optionsJson: `{"ignoreIfStatements": true}`},
	{name: "upstream valid[118]", source: `
declare let x: string | null | undefined;
x || 'foo' ? null : null;
    `, optionsJson: ""},
	{name: "upstream valid[119]", source: `
declare let x: string | null | undefined;
(x ||= 'foo') ? null : null;
    `, optionsJson: ""},
	{name: "upstream valid[120]", source: `
declare let x: number | null | undefined;
x || 'foo' ? null : null;
    `, optionsJson: ""},
	{name: "upstream valid[121]", source: `
declare let x: number | null | undefined;
(x ||= 'foo') ? null : null;
    `, optionsJson: ""},
	{name: "upstream valid[122]", source: `
declare let x: boolean | null | undefined;
x || 'foo' ? null : null;
    `, optionsJson: ""},
	{name: "upstream valid[123]", source: `
declare let x: boolean | null | undefined;
(x ||= 'foo') ? null : null;
    `, optionsJson: ""},
	{name: "upstream valid[124]", source: `
declare let x: object | null | undefined;
x || 'foo' ? null : null;
    `, optionsJson: ""},
	{name: "upstream valid[125]", source: `
declare let x: object | null | undefined;
(x ||= 'foo') ? null : null;
    `, optionsJson: ""},
	{name: "upstream valid[126]", source: `
declare let x: string | null | undefined;
if (x || 'foo') {
}
    `, optionsJson: ""},
	{name: "upstream valid[127]", source: `
declare let x: string | null | undefined;
if ((x ||= 'foo')) {
}
    `, optionsJson: ""},
	{name: "upstream valid[128]", source: `
declare let x: number | null | undefined;
if (x || 'foo') {
}
    `, optionsJson: ""},
	{name: "upstream valid[129]", source: `
declare let x: number | null | undefined;
if ((x ||= 'foo')) {
}
    `, optionsJson: ""},
	{name: "upstream valid[130]", source: `
declare let x: boolean | null | undefined;
if (x || 'foo') {
}
    `, optionsJson: ""},
	{name: "upstream valid[131]", source: `
declare let x: boolean | null | undefined;
if ((x ||= 'foo')) {
}
    `, optionsJson: ""},
	{name: "upstream valid[132]", source: `
declare let x: object | null | undefined;
if (x || 'foo') {
}
    `, optionsJson: ""},
	{name: "upstream valid[133]", source: `
declare let x: object | null | undefined;
if ((x ||= 'foo')) {
}
    `, optionsJson: ""},
	{name: "upstream valid[134]", source: `
declare let x: string | null | undefined;
do {} while (x || 'foo');
    `, optionsJson: ""},
	{name: "upstream valid[135]", source: `
declare let x: string | null | undefined;
do {} while ((x ||= 'foo'));
    `, optionsJson: ""},
	{name: "upstream valid[136]", source: `
declare let x: number | null | undefined;
do {} while (x || 'foo');
    `, optionsJson: ""},
	{name: "upstream valid[137]", source: `
declare let x: number | null | undefined;
do {} while ((x ||= 'foo'));
    `, optionsJson: ""},
	{name: "upstream valid[138]", source: `
declare let x: boolean | null | undefined;
do {} while (x || 'foo');
    `, optionsJson: ""},
	{name: "upstream valid[139]", source: `
declare let x: boolean | null | undefined;
do {} while ((x ||= 'foo'));
    `, optionsJson: ""},
	{name: "upstream valid[140]", source: `
declare let x: object | null | undefined;
do {} while (x || 'foo');
    `, optionsJson: ""},
	{name: "upstream valid[141]", source: `
declare let x: object | null | undefined;
do {} while ((x ||= 'foo'));
    `, optionsJson: ""},
	{name: "upstream valid[142]", source: `
declare let x: string | null | undefined;
for (; x || 'foo';) {}
    `, optionsJson: ""},
	{name: "upstream valid[143]", source: `
declare let x: string | null | undefined;
for (; (x ||= 'foo');) {}
    `, optionsJson: ""},
	{name: "upstream valid[144]", source: `
declare let x: number | null | undefined;
for (; x || 'foo';) {}
    `, optionsJson: ""},
	{name: "upstream valid[145]", source: `
declare let x: number | null | undefined;
for (; (x ||= 'foo');) {}
    `, optionsJson: ""},
	{name: "upstream valid[146]", source: `
declare let x: boolean | null | undefined;
for (; x || 'foo';) {}
    `, optionsJson: ""},
	{name: "upstream valid[147]", source: `
declare let x: boolean | null | undefined;
for (; (x ||= 'foo');) {}
    `, optionsJson: ""},
	{name: "upstream valid[148]", source: `
declare let x: object | null | undefined;
for (; x || 'foo';) {}
    `, optionsJson: ""},
	{name: "upstream valid[149]", source: `
declare let x: object | null | undefined;
for (; (x ||= 'foo');) {}
    `, optionsJson: ""},
	{name: "upstream valid[150]", source: `
declare let x: string | null | undefined;
while (x || 'foo') {}
    `, optionsJson: ""},
	{name: "upstream valid[151]", source: `
declare let x: string | null | undefined;
while ((x ||= 'foo')) {}
    `, optionsJson: ""},
	{name: "upstream valid[152]", source: `
declare let x: number | null | undefined;
while (x || 'foo') {}
    `, optionsJson: ""},
	{name: "upstream valid[153]", source: `
declare let x: number | null | undefined;
while ((x ||= 'foo')) {}
    `, optionsJson: ""},
	{name: "upstream valid[154]", source: `
declare let x: boolean | null | undefined;
while (x || 'foo') {}
    `, optionsJson: ""},
	{name: "upstream valid[155]", source: `
declare let x: boolean | null | undefined;
while ((x ||= 'foo')) {}
    `, optionsJson: ""},
	{name: "upstream valid[156]", source: `
declare let x: object | null | undefined;
while (x || 'foo') {}
    `, optionsJson: ""},
	{name: "upstream valid[157]", source: `
declare let x: object | null | undefined;
while ((x ||= 'foo')) {}
    `, optionsJson: ""},
	{name: "upstream valid[158]", source: `
declare let a: string | null | undefined;
declare let b: string | null | undefined;
declare let c: string | null | undefined;
a || (b && c);
      `, optionsJson: `{"ignoreMixedLogicalExpressions": true}`},
	{name: "upstream valid[159]", source: `
declare let a: number | null | undefined;
declare let b: number | null | undefined;
declare let c: number | null | undefined;
a || (b && c);
      `, optionsJson: `{"ignoreMixedLogicalExpressions": true}`},
	{name: "upstream valid[160]", source: `
declare let a: boolean | null | undefined;
declare let b: boolean | null | undefined;
declare let c: boolean | null | undefined;
a || (b && c);
      `, optionsJson: `{"ignoreMixedLogicalExpressions": true}`},
	{name: "upstream valid[161]", source: `
declare let a: object | null | undefined;
declare let b: object | null | undefined;
declare let c: object | null | undefined;
a || (b && c);
      `, optionsJson: `{"ignoreMixedLogicalExpressions": true}`},
	{name: "upstream valid[162]", source: `
declare let a: string | null | undefined;
declare let b: string | null | undefined;
declare let c: string | null | undefined;
declare let d: string | null | undefined;
a || b || (c && d);
      `, optionsJson: `{"ignoreMixedLogicalExpressions": true}`},
	{name: "upstream valid[163]", source: `
declare let a: number | null | undefined;
declare let b: number | null | undefined;
declare let c: number | null | undefined;
declare let d: number | null | undefined;
a || b || (c && d);
      `, optionsJson: `{"ignoreMixedLogicalExpressions": true}`},
	{name: "upstream valid[164]", source: `
declare let a: boolean | null | undefined;
declare let b: boolean | null | undefined;
declare let c: boolean | null | undefined;
declare let d: boolean | null | undefined;
a || b || (c && d);
      `, optionsJson: `{"ignoreMixedLogicalExpressions": true}`},
	{name: "upstream valid[165]", source: `
declare let a: object | null | undefined;
declare let b: object | null | undefined;
declare let c: object | null | undefined;
declare let d: object | null | undefined;
a || b || (c && d);
      `, optionsJson: `{"ignoreMixedLogicalExpressions": true}`},
	{name: "upstream valid[166]", source: `
declare let a: string | null | undefined;
declare let b: string | null | undefined;
declare let c: string | null | undefined;
declare let d: string | null | undefined;
(a && b) || c || d;
      `, optionsJson: `{"ignoreMixedLogicalExpressions": true}`},
	{name: "upstream valid[167]", source: `
declare let a: number | null | undefined;
declare let b: number | null | undefined;
declare let c: number | null | undefined;
declare let d: number | null | undefined;
(a && b) || c || d;
      `, optionsJson: `{"ignoreMixedLogicalExpressions": true}`},
	{name: "upstream valid[168]", source: `
declare let a: boolean | null | undefined;
declare let b: boolean | null | undefined;
declare let c: boolean | null | undefined;
declare let d: boolean | null | undefined;
(a && b) || c || d;
      `, optionsJson: `{"ignoreMixedLogicalExpressions": true}`},
	{name: "upstream valid[169]", source: `
declare let a: object | null | undefined;
declare let b: object | null | undefined;
declare let c: object | null | undefined;
declare let d: object | null | undefined;
(a && b) || c || d;
      `, optionsJson: `{"ignoreMixedLogicalExpressions": true}`},
	{name: "upstream valid[170]", source: `
declare let x: string | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"string": true}}`},
	{name: "upstream valid[171]", source: `
declare let x: number | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"number": true}}`},
	{name: "upstream valid[172]", source: `
declare let x: boolean | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"boolean": true}}`},
	{name: "upstream valid[173]", source: `
declare let x: bigint | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true}}`},
	{name: "upstream valid[174]", source: `
declare let x: string | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[175]", source: `
declare let x: number | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[176]", source: `
declare let x: boolean | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[177]", source: `
declare let x: bigint | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[178]", source: `
declare let x: (string & { __brand?: any }) | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"string": true}}`},
	{name: "upstream valid[179]", source: `
declare let x: (number & { __brand?: any }) | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"number": true}}`},
	{name: "upstream valid[180]", source: `
declare let x: (boolean & { __brand?: any }) | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"boolean": true}}`},
	{name: "upstream valid[181]", source: `
declare let x: (bigint & { __brand?: any }) | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true}}`},
	{name: "upstream valid[182]", source: `
declare let x: (string & { __brand?: any }) | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[183]", source: `
declare let x: (number & { __brand?: any }) | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[184]", source: `
declare let x: (boolean & { __brand?: any }) | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[185]", source: `
declare let x: (bigint & { __brand?: any }) | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[186]", source: `
declare let x: string | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": {"string": true}}`},
	{name: "upstream valid[187]", source: `
declare let x: number | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": {"number": true}}`},
	{name: "upstream valid[188]", source: `
declare let x: boolean | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": {"boolean": true}}`},
	{name: "upstream valid[189]", source: `
declare let x: bigint | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true}}`},
	{name: "upstream valid[190]", source: `
declare let x: string | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": {"string": true}}`},
	{name: "upstream valid[191]", source: `
declare let x: number | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": {"number": true}}`},
	{name: "upstream valid[192]", source: `
declare let x: boolean | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": {"boolean": true}}`},
	{name: "upstream valid[193]", source: `
declare let x: bigint | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true}}`},
	{name: "upstream valid[194]", source: `
declare let x: string | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[195]", source: `
declare let x: number | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[196]", source: `
declare let x: boolean | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[197]", source: `
declare let x: bigint | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[198]", source: `
declare let x: string | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[199]", source: `
declare let x: number | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[200]", source: `
declare let x: boolean | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[201]", source: `
declare let x: bigint | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[202]", source: `
declare let x: (string & { __brand?: any }) | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": {"string": true}}`},
	{name: "upstream valid[203]", source: `
declare let x: (number & { __brand?: any }) | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": {"number": true}}`},
	{name: "upstream valid[204]", source: `
declare let x: (boolean & { __brand?: any }) | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": {"boolean": true}}`},
	{name: "upstream valid[205]", source: `
declare let x: (bigint & { __brand?: any }) | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true}}`},
	{name: "upstream valid[206]", source: `
declare let x: (string & { __brand?: any }) | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": {"string": true}}`},
	{name: "upstream valid[207]", source: `
declare let x: (number & { __brand?: any }) | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": {"number": true}}`},
	{name: "upstream valid[208]", source: `
declare let x: (boolean & { __brand?: any }) | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": {"boolean": true}}`},
	{name: "upstream valid[209]", source: `
declare let x: (bigint & { __brand?: any }) | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true}}`},
	{name: "upstream valid[210]", source: `
declare let x: (string & { __brand?: any }) | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[211]", source: `
declare let x: (number & { __brand?: any }) | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[212]", source: `
declare let x: (boolean & { __brand?: any }) | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[213]", source: `
declare let x: (bigint & { __brand?: any }) | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[214]", source: `
declare let x: (string & { __brand?: any }) | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[215]", source: `
declare let x: (number & { __brand?: any }) | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[216]", source: `
declare let x: (boolean & { __brand?: any }) | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[217]", source: `
declare let x: (bigint & { __brand?: any }) | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[218]", source: `
declare let x: never;
declare let y: number;
x || y;
    `, optionsJson: ""},
	{name: "upstream valid[219]", source: `
declare let x: never;
declare let y: number;
x ? x : y;
    `, optionsJson: ""},
	{name: "upstream valid[220]", source: `
declare let x: never;
declare let y: number;
!x ? y : x;
    `, optionsJson: ""},
	{name: "upstream valid[221]", source: `
interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBoxOptional: { a?: { b?: Box | undefined } };

defaultBoxOptional.a?.b !== null ? defaultBoxOptional.a?.b : getFallbackBox();
    `, optionsJson: ""},
	{name: "upstream valid[222]", source: `
interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBoxOptional: { a?: { b?: Box | null } };

defaultBoxOptional.a?.b !== null ? defaultBoxOptional.a?.b : getFallbackBox();
    `, optionsJson: ""},
	{name: "upstream valid[223]", source: `
interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBoxOptional: { a?: { b?: Box | null } };

defaultBoxOptional.a?.b !== undefined
  ? defaultBoxOptional.a?.b
  : getFallbackBox();
    `, optionsJson: ""},
	{name: "upstream valid[224]", source: `
interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBoxOptional: { a?: { b?: Box | null } };

defaultBoxOptional.a?.b !== undefined
  ? defaultBoxOptional.a.b
  : getFallbackBox();
    `, optionsJson: ""},
	{name: "upstream valid[225]", source: `
declare let x: 0 | 1 | 0n | 1n | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true, "boolean": true, "number": false, "string": true}}`},
	{name: "upstream valid[226]", source: `
declare let x: 0 | 1 | 0n | 1n | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": false, "boolean": true, "number": true, "string": true}}`},
	{name: "upstream valid[227]", source: `
declare let x: 0 | 'foo' | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"number": true, "string": true}}`},
	{name: "upstream valid[228]", source: `
declare let x: 0 | 'foo' | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"number": true, "string": false}}`},
	{name: "upstream valid[229]", source: `
enum Enum {
  A = 0,
  B = 1,
  C = 2,
}
declare let x: Enum | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"number": true}}`},
	{name: "upstream valid[230]", source: `
enum Enum {
  A = 0,
  B = 1,
  C = 2,
}
declare let x: Enum.A | Enum.B | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"number": true}}`},
	{name: "upstream valid[231]", source: `
enum Enum {
  A = 'a',
  B = 'b',
  C = 'c',
}
declare let x: Enum | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"string": true}}`},
	{name: "upstream valid[232]", source: `
enum Enum {
  A = 'a',
  B = 'b',
  C = 'c',
}
declare let x: Enum.A | Enum.B | undefined;
x || y;
      `, optionsJson: `{"ignorePrimitives": {"string": true}}`},
	{name: "upstream valid[233]", source: `
declare let x: 0 | 1 | 0n | 1n | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true, "boolean": true, "number": false, "string": true}}`},
	{name: "upstream valid[234]", source: `
declare let x: 0 | 1 | 0n | 1n | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": {"bigint": true, "boolean": true, "number": false, "string": true}}`},
	{name: "upstream valid[235]", source: `
declare let x: 0 | 1 | 0n | 1n | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": {"bigint": false, "boolean": true, "number": true, "string": true}}`},
	{name: "upstream valid[236]", source: `
declare let x: 0 | 1 | 0n | 1n | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": {"bigint": false, "boolean": true, "number": true, "string": true}}`},
	{name: "upstream valid[237]", source: `
declare let x: 0 | 'foo' | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": {"number": true, "string": true}}`},
	{name: "upstream valid[238]", source: `
declare let x: 0 | 'foo' | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": {"number": true, "string": true}}`},
	{name: "upstream valid[239]", source: `
declare let x: 0 | 'foo' | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": {"number": true, "string": false}}`},
	{name: "upstream valid[240]", source: `
declare let x: 0 | 'foo' | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": {"number": true, "string": false}}`},
	{name: "upstream valid[241]", source: `
enum Enum {
  A = 0,
  B = 1,
  C = 2,
}
declare let x: Enum | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": {"number": true}}`},
	{name: "upstream valid[242]", source: `
enum Enum {
  A = 0,
  B = 1,
  C = 2,
}
declare let x: Enum | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": {"number": true}}`},
	{name: "upstream valid[243]", source: `
enum Enum {
  A = 0,
  B = 1,
  C = 2,
}
declare let x: Enum.A | Enum.B | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": {"number": true}}`},
	{name: "upstream valid[244]", source: `
enum Enum {
  A = 0,
  B = 1,
  C = 2,
}
declare let x: Enum.A | Enum.B | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": {"number": true}}`},
	{name: "upstream valid[245]", source: `
enum Enum {
  A = 'a',
  B = 'b',
  C = 'c',
}
declare let x: Enum | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": {"string": true}}`},
	{name: "upstream valid[246]", source: `
enum Enum {
  A = 'a',
  B = 'b',
  C = 'c',
}
declare let x: Enum | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": {"string": true}}`},
	{name: "upstream valid[247]", source: `
enum Enum {
  A = 'a',
  B = 'b',
  C = 'c',
}
declare let x: Enum.A | Enum.B | undefined;
x ? x : y;
      `, optionsJson: `{"ignorePrimitives": {"string": true}}`},
	{name: "upstream valid[248]", source: `
enum Enum {
  A = 'a',
  B = 'b',
  C = 'c',
}
declare let x: Enum.A | Enum.B | undefined;
!x ? y : x;
      `, optionsJson: `{"ignorePrimitives": {"string": true}}`},
	{name: "upstream valid[249]", source: `
let a: string | true | undefined;
let b: string | boolean | undefined;

const x = Boolean(a || b);
      `, optionsJson: `{"ignoreBooleanCoercion": true}`},
	{name: "upstream valid[250]", source: `
let a: string | boolean | undefined;
let b: string | boolean | undefined;
let c: string | boolean | undefined;

const test = Boolean(a || b || c);
      `, optionsJson: `{"ignoreBooleanCoercion": true}`},
	{name: "upstream valid[251]", source: `
let a: string | boolean | undefined;
let b: string | boolean | undefined;
let c: string | boolean | undefined;

const test = Boolean(a || (b && c));
      `, optionsJson: `{"ignoreBooleanCoercion": true}`},
	{name: "upstream valid[252]", source: `
let a: string | boolean | undefined;
let b: string | boolean | undefined;
let c: string | boolean | undefined;

const test = Boolean((a || b) ?? c);
      `, optionsJson: `{"ignoreBooleanCoercion": true}`},
	{name: "upstream valid[253]", source: `
let a: string | boolean | undefined;
let b: string | boolean | undefined;
let c: string | boolean | undefined;

const test = Boolean(a ?? (b || c));
      `, optionsJson: `{"ignoreBooleanCoercion": true}`},
	{name: "upstream valid[254]", source: `
let a: string | boolean | undefined;
let b: string | boolean | undefined;
let c: string | boolean | undefined;

const test = Boolean(a ? b || c : 'fail');
      `, optionsJson: `{"ignoreBooleanCoercion": true}`},
	{name: "upstream valid[255]", source: `
let a: string | boolean | undefined;
let b: string | boolean | undefined;
let c: string | boolean | undefined;

const test = Boolean(a ? 'success' : b || c);
      `, optionsJson: `{"ignoreBooleanCoercion": true}`},
	{name: "upstream valid[256]", source: `
let a: string | boolean | undefined;
let b: string | boolean | undefined;
let c: string | boolean | undefined;

const test = Boolean(((a = b), b || c));
      `, optionsJson: `{"ignoreBooleanCoercion": true}`},
	{name: "upstream valid[257]", source: `
let a: string | boolean | undefined;
let b: string | boolean | undefined;
let c: string | boolean | undefined;

const test = Boolean((a ? a : b) || c);
      `, optionsJson: `{"ignoreBooleanCoercion": true}`},
	{name: "upstream valid[258]", source: `
let a: string | boolean | undefined;
let b: string | boolean | undefined;
let c: string | boolean | undefined;

const test = Boolean(c || (!a ? b : a));
      `, optionsJson: `{"ignoreBooleanCoercion": true}`},
	{name: "upstream valid[259]", source: `
let a: string | boolean | undefined;
let b: string | boolean | undefined;
let c: string | boolean | undefined;

if (a || b || c) {
}
      `, optionsJson: `{"ignoreConditionalTests": true}`},
	{name: "upstream valid[260]", source: `
let a: string | boolean | undefined;
let b: string | boolean | undefined;
let c: string | boolean | undefined;

if (a || (b && c)) {
}
      `, optionsJson: `{"ignoreConditionalTests": true}`},
	{name: "upstream valid[261]", source: `
let a: string | boolean | undefined;
let b: string | boolean | undefined;
let c: string | boolean | undefined;

if ((a || b) ?? c) {
}
      `, optionsJson: `{"ignoreConditionalTests": true}`},
	{name: "upstream valid[262]", source: `
let a: string | boolean | undefined;
let b: string | boolean | undefined;
let c: string | boolean | undefined;

if (a ?? (b || c)) {
}
      `, optionsJson: `{"ignoreConditionalTests": true}`},
	{name: "upstream valid[263]", source: `
let a: string | boolean | undefined;
let b: string | boolean | undefined;
let c: string | boolean | undefined;

if (a ? b || c : 'fail') {
}
      `, optionsJson: `{"ignoreConditionalTests": true}`},
	{name: "upstream valid[264]", source: `
let a: string | boolean | undefined;
let b: string | boolean | undefined;
let c: string | boolean | undefined;

if (a ? 'success' : b || c) {
}
      `, optionsJson: `{"ignoreConditionalTests": true}`},
	{name: "upstream valid[265]", source: `
let a: string | boolean | undefined;
let b: string | boolean | undefined;
let c: string | boolean | undefined;

if (((a = b), b || c)) {
}
      `, optionsJson: `{"ignoreConditionalTests": true}`},
	{name: "upstream valid[266]", source: `
let a: string | undefined;
let b: string | undefined;

if (!(a || b)) {
}
      `, optionsJson: `{"ignoreConditionalTests": true}`},
	{name: "upstream valid[267]", source: `
let a: string | undefined;
let b: string | undefined;

if (!!(a || b)) {
}
      `, optionsJson: `{"ignoreConditionalTests": true}`},
	{name: "upstream valid[268]", source: `
let a: string | true | undefined;
let b: string | boolean | undefined;

if (a ? a : b) {
}
      `, optionsJson: `{"ignoreConditionalTests": true}`},
	{name: "upstream valid[269]", source: `
let a: string | boolean | undefined;
let b: string | boolean | undefined;

if (!a ? b : a) {
}
      `, optionsJson: `{"ignoreConditionalTests": true}`},
	{name: "upstream valid[270]", source: `
let a: string | boolean | undefined;
let b: string | boolean | undefined;
let c: string | boolean | undefined;

if ((a ? a : b) || c) {
}
      `, optionsJson: `{"ignoreConditionalTests": true}`},
	{name: "upstream valid[271]", source: `
let a: string | boolean | undefined;
let b: string | boolean | undefined;
let c: string | boolean | undefined;

if (c || (!a ? b : a)) {
}
      `, optionsJson: `{"ignoreConditionalTests": true}`},
	{name: "upstream valid[272]", source: `
declare const a: any;
declare const b: any;
a ? a : b;
      `, optionsJson: `{"ignorePrimitives": true}`},
	{name: "upstream valid[273]", source: `
declare const a: any;
declare const b: any;
a ? a : b;
      `, optionsJson: `{"ignorePrimitives": {"number": true}}`},
	{name: "upstream valid[274]", source: `
declare const a: unknown;
const b = a || 'bar';
      `, optionsJson: `{"ignorePrimitives": {"bigint": true, "boolean": false, "number": false, "string": false}}`},
}
