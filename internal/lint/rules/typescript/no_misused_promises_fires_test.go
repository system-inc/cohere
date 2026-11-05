package typescript

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// TestNoMisusedPromisesFires carries eighty-nine of tsgolint's ninety invalid cases and asserts the
// ordered list of message ids each one produces.
//
// An ordered list rather than a count, because twelve of these cases report more than once on one
// input and four of them mix two different message ids in a single case. A fixture asserting "this
// case reports" would pass while dropping half the findings, and one asserting only a total would
// pass while attributing a `voidReturnArgument` to the `conditional` check.
//
// The one exclusion is upstream's own: `invalid` case 31 is marked `Skip: true` there, so upstream
// does not run it either. TestNoMisusedPromisesExclusionsAreStated names it rather than leaving the
// count unexplained.
func TestNoMisusedPromisesFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		fileName   string
		sourceText string
		options    any
		wantIds    []string
	}{
		{
			name:     "upstream invalid 0",
			fileName: noMisusedPromisesFile,
			sourceText: `
if (Promise.resolve()) {
}
      `,
			options: nil,
			wantIds: []string{"conditional"},
		},
		{
			name:     "upstream invalid 1",
			fileName: noMisusedPromisesFile,
			sourceText: `
if (Promise.resolve()) {
} else if (Promise.resolve()) {
} else {
}
      `,
			options: nil,
			wantIds: []string{"conditional", "conditional"},
		},
		{
			name:       "upstream invalid 2",
			fileName:   noMisusedPromisesFile,
			sourceText: `for (let i; Promise.resolve(); i++) {}`,
			options:    nil,
			wantIds:    []string{"conditional"},
		},
		{
			name:       "upstream invalid 3",
			fileName:   noMisusedPromisesFile,
			sourceText: `do {} while (Promise.resolve());`,
			options:    nil,
			wantIds:    []string{"conditional"},
		},
		{
			name:       "upstream invalid 4",
			fileName:   noMisusedPromisesFile,
			sourceText: `while (Promise.resolve()) {}`,
			options:    nil,
			wantIds:    []string{"conditional"},
		},
		{
			name:       "upstream invalid 5",
			fileName:   noMisusedPromisesFile,
			sourceText: `Promise.resolve() ? 123 : 456;`,
			options:    nil,
			wantIds:    []string{"conditional"},
		},
		{
			name:     "upstream invalid 6",
			fileName: noMisusedPromisesFile,
			sourceText: `
if (!Promise.resolve()) {
}
      `,
			options: nil,
			wantIds: []string{"conditional"},
		},
		{
			name:       "upstream invalid 7",
			fileName:   noMisusedPromisesFile,
			sourceText: `Promise.resolve() || false;`,
			options:    nil,
			wantIds:    []string{"conditional"},
		},
		{
			name:     "upstream invalid 8",
			fileName: noMisusedPromisesFile,
			sourceText: `
[Promise.resolve(), Promise.reject()].forEach(async val => {
  await val;
});
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 9",
			fileName: noMisusedPromisesFile,
			sourceText: `
new Promise(async (resolve, reject) => {
  await Promise.resolve();
  resolve();
});
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 10",
			fileName: noMisusedPromisesFile,
			sourceText: `
const fnWithCallback = (arg: string, cb: (err: any, res: string) => void) => {
  cb(null, arg);
};

fnWithCallback('val', async (err, res) => {
  await res;
});
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 11",
			fileName: noMisusedPromisesFile,
			sourceText: `
const fnWithCallback = (arg: string, cb: (err: any, res: string) => void) => {
  cb(null, arg);
};

fnWithCallback('val', (err, res) => Promise.resolve(res));
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 12",
			fileName: noMisusedPromisesFile,
			sourceText: `
const fnWithCallback = (arg: string, cb: (err: any, res: string) => void) => {
  cb(null, arg);
};

fnWithCallback('val', (err, res) => {
  if (err) {
    return 'abc';
  } else {
    return Promise.resolve(res);
  }
});
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 13",
			fileName: noMisusedPromisesFile,
			sourceText: `
const fnWithCallback:
  | ((arg: string, cb: (err: any, res: string) => void) => void)
  | null = (arg, cb) => {
  cb(null, arg);
};

fnWithCallback?.('val', (err, res) => Promise.resolve(res));
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 14",
			fileName: noMisusedPromisesFile,
			sourceText: `
const fnWithCallback:
  | ((arg: string, cb: (err: any, res: string) => void) => void)
  | null = (arg, cb) => {
  cb(null, arg);
};

fnWithCallback('val', (err, res) => {
  if (err) {
    return 'abc';
  } else {
    return Promise.resolve(res);
  }
});
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 15",
			fileName: noMisusedPromisesFile,
			sourceText: `
function test(bool: boolean, p: Promise<void>) {
  if (bool || p) {
  }
}
      `,
			options: nil,
			wantIds: []string{"conditional"},
		},
		{
			name:     "upstream invalid 16",
			fileName: noMisusedPromisesFile,
			sourceText: `
function test(bool: boolean, p: Promise<void>) {
  if (bool && p) {
  }
}
      `,
			options: nil,
			wantIds: []string{"conditional"},
		},
		{
			name:     "upstream invalid 17",
			fileName: noMisusedPromisesFile,
			sourceText: `
function test(a: any, p: Promise<void>) {
  if (a ?? p) {
  }
}
      `,
			options: nil,
			wantIds: []string{"conditional"},
		},
		{
			name:     "upstream invalid 18",
			fileName: noMisusedPromisesFile,
			sourceText: `
function test(p: Promise<void> | undefined) {
  if (p ?? Promise.reject()) {
  }
}
      `,
			options: nil,
			wantIds: []string{"conditional"},
		},
		{
			name:     "upstream invalid 19",
			fileName: noMisusedPromisesFile,
			sourceText: `
let f: () => void;
f = async () => {
  return 3;
};
      `,
			options: nil,
			wantIds: []string{"voidReturnVariable"},
		},
		{
			name:     "upstream invalid 20 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
let f: () => void;
f = async () => {
  return 3;
};
      `,
			options: NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(NoMisusedPromisesChecksVoidReturnOptions{Variables: type_checking.Ref(true)})},
			wantIds: []string{"voidReturnVariable"},
		},
		{
			name:     "upstream invalid 21",
			fileName: noMisusedPromisesFile,
			sourceText: `
const f: () => void = async () => {
  return 0;
};
const g = async () => 1,
  h: () => void = async () => {};
      `,
			options: nil,
			wantIds: []string{"voidReturnVariable", "voidReturnVariable"},
		},
		{
			name:     "upstream invalid 22",
			fileName: noMisusedPromisesFile,
			sourceText: `
const obj: {
  f?: () => void;
} = {};
obj.f = async () => {
  return 0;
};
      `,
			options: nil,
			wantIds: []string{"voidReturnVariable"},
		},
		{
			name:     "upstream invalid 23",
			fileName: noMisusedPromisesFile,
			sourceText: `
type O = { f: () => void };
const obj: O = {
  f: async () => 'foo',
};
      `,
			options: nil,
			wantIds: []string{"voidReturnProperty"},
		},
		{
			name:     "upstream invalid 24 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
type O = { f: () => void };
const obj: O = {
  f: async () => 'foo',
};
      `,
			options: NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(NoMisusedPromisesChecksVoidReturnOptions{Properties: type_checking.Ref(true)})},
			wantIds: []string{"voidReturnProperty"},
		},
		{
			name:     "upstream invalid 25",
			fileName: noMisusedPromisesFile,
			sourceText: `
type O = { f: () => void };
const f = async () => 0;
const obj: O = {
  f,
};
      `,
			options: nil,
			wantIds: []string{"voidReturnProperty"},
		},
		{
			name:     "upstream invalid 26",
			fileName: noMisusedPromisesFile,
			sourceText: `
type O = { f: () => void };
const obj: O = {
  async f() {
    return 0;
  },
};
      `,
			options: nil,
			wantIds: []string{"voidReturnProperty"},
		},
		{
			name:     "upstream invalid 27",
			fileName: noMisusedPromisesFile,
			sourceText: `
type O = { f: () => void; g: () => void; h: () => void };
function f(): O {
  const h = async () => 0;
  return {
    async f() {
      return 123;
    },
    g: async () => 0,
    h,
  };
}
      `,
			options: nil,
			wantIds: []string{"voidReturnProperty", "voidReturnProperty", "voidReturnProperty"},
		},
		{
			name:     "upstream invalid 28",
			fileName: noMisusedPromisesFile,
			sourceText: `
function f(): () => void {
  return async () => 0;
}
      `,
			options: nil,
			wantIds: []string{"voidReturnReturnValue"},
		},
		{
			name:     "upstream invalid 29 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
function f(): () => void {
  return async () => 0;
}
      `,
			options: NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(NoMisusedPromisesChecksVoidReturnOptions{Returns: type_checking.Ref(true)})},
			wantIds: []string{"voidReturnReturnValue"},
		},
		{
			name:     "upstream invalid 30 [tsx]",
			fileName: noMisusedPromisesTsxFile,
			sourceText: `
type O = {
  func: () => void;
};
const Component = (obj: O) => null;
<Component func={async () => 0} />;
      `,
			options: nil,
			wantIds: []string{"voidReturnAttribute"},
		},
		{
			name:     "upstream invalid 32 [tsx]",
			fileName: noMisusedPromisesTsxFile,
			sourceText: `
type O = {
  func: () => void;
};
const g = async () => 'foo';
const Component = (obj: O) => null;
<Component func={g} />;
      `,
			options: nil,
			wantIds: []string{"voidReturnAttribute"},
		},
		{
			name:     "upstream invalid 33",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface ItLike {
  (name: string, callback: () => number): void;
  (name: string, callback: () => void): void;
}

declare const it: ItLike;

it('', async () => {});
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 34",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface ItLike {
  (name: string, callback: () => number): void;
}
interface ItLike {
  (name: string, callback: () => void): void;
}

declare const it: ItLike;

it('', async () => {});
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 35",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface ItLike {
  (name: string, callback: () => void): void;
}
interface ItLike {
  (name: string, callback: () => number): void;
}

declare const it: ItLike;

it('', async () => {});
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 36",
			fileName: noMisusedPromisesFile,
			sourceText: `
console.log({ ...Promise.resolve({ key: 42 }) });
      `,
			options: nil,
			wantIds: []string{"spread"},
		},
		{
			name:     "upstream invalid 37",
			fileName: noMisusedPromisesFile,
			sourceText: `
const getData = () => Promise.resolve({ key: 42 });

console.log({
  someData: 42,
  ...getData(),
});
      `,
			options: nil,
			wantIds: []string{"spread"},
		},
		{
			name:     "upstream invalid 38",
			fileName: noMisusedPromisesFile,
			sourceText: `
declare const condition: boolean;

console.log({ ...(condition && Promise.resolve({ key: 42 })) });
console.log({ ...(condition || Promise.resolve({ key: 42 })) });
console.log({ ...(condition ? {} : Promise.resolve({ key: 42 })) });
console.log({ ...(condition ? Promise.resolve({ key: 42 }) : {}) });
      `,
			options: nil,
			wantIds: []string{"spread", "spread", "spread", "spread"},
		},
		{
			name:     "upstream invalid 39",
			fileName: noMisusedPromisesFile,
			sourceText: `
function restPromises(first: Boolean, ...callbacks: Array<() => void>): void {}

restPromises(
  true,
  () => Promise.resolve(true),
  () => Promise.resolve(null),
  () => true,
  () => Promise.resolve('Hello'),
);
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument", "voidReturnArgument", "voidReturnArgument"},
		},
		{
			name:     "upstream invalid 40",
			fileName: noMisusedPromisesFile,
			sourceText: `
type MyUnion = (() => void) | boolean;

function restUnion(first: string, ...callbacks: Array<MyUnion>): void {}
restUnion('Testing', false, () => Promise.resolve(true));
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 41",
			fileName: noMisusedPromisesFile,
			sourceText: `
function restTupleOne(first: string, ...callbacks: [() => void]): void {}
restTupleOne('My string', () => Promise.resolve(1));
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 42",
			fileName: noMisusedPromisesFile,
			sourceText: `
function restTupleTwo(
  first: boolean,
  ...callbacks: [undefined, () => void, undefined]
): void {}

restTupleTwo(true, undefined, () => Promise.resolve(true), undefined);
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 43",
			fileName: noMisusedPromisesFile,
			sourceText: `
function restTupleFour(
  first: number,
  ...callbacks: [() => void, boolean, () => void, () => void]
): void;

restTupleFour(
  1,
  () => Promise.resolve(true),
  false,
  () => {},
  () => Promise.resolve(1),
);
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument", "voidReturnArgument"},
		},
		{
			name:     "upstream invalid 44",
			fileName: noMisusedPromisesFile,
			sourceText: `
class TakesVoidCb {
  constructor(first: string, ...args: Array<() => void>);
}

new TakesVoidCb;
new TakesVoidCb();
new TakesVoidCb(
  'Testing',
  () => {},
  () => Promise.resolve(true),
);
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 45",
			fileName: noMisusedPromisesFile,
			sourceText: `
function restTuple(...args: []): void;
function restTuple(...args: [boolean, () => void]): void;
function restTuple(..._args: any[]): void {}

restTuple();
restTuple(true, () => Promise.resolve(1));
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 46",
			fileName: noMisusedPromisesFile,
			sourceText: `
type ReturnsRecord = () => Record<string, () => void>;

const test: ReturnsRecord = () => {
  return { asynchronous: async () => {} };
};
      `,
			options: nil,
			wantIds: []string{"voidReturnProperty"},
		},
		{
			name:     "upstream invalid 47",
			fileName: noMisusedPromisesFile,
			sourceText: `
let value: Record<string, () => void>;
value.asynchronous = async () => {};
      `,
			options: nil,
			wantIds: []string{"voidReturnVariable"},
		},
		{
			name:     "upstream invalid 48",
			fileName: noMisusedPromisesFile,
			sourceText: `
type ReturnsRecord = () => Record<string, () => void>;

async function asynchronous() {}

const test: ReturnsRecord = () => {
  return { asynchronous };
};
      `,
			options: nil,
			wantIds: []string{"voidReturnProperty"},
		},
		{
			name:     "upstream invalid 49",
			fileName: noMisusedPromisesFile,
			sourceText: `
declare function foo(cb: undefined | (() => void));
declare const bar: undefined | (() => Promise<void>);
foo(bar);
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 50",
			fileName: noMisusedPromisesFile,
			sourceText: `
declare function foo(cb: string & (() => void));
declare const bar: string & (() => Promise<void>);
foo(bar);
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 51",
			fileName: noMisusedPromisesFile,
			sourceText: `
function consume(..._callbacks: Array<() => void>): void {}
let cbs: Array<() => Promise<boolean>> = [
  () => Promise.resolve(true),
  () => Promise.resolve(true),
];
consume(...cbs);
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 52",
			fileName: noMisusedPromisesFile,
			sourceText: `
function consume(..._callbacks: Array<() => void>): void {}
let cbs = [() => Promise.resolve(true), () => Promise.resolve(true)] as const;
consume(...cbs);
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 53",
			fileName: noMisusedPromisesFile,
			sourceText: `
function consume(..._callbacks: Array<() => void>): void {}
let cbs = [() => Promise.resolve(true), () => Promise.resolve(true)];
consume(...cbs);
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 54",
			fileName: noMisusedPromisesFile,
			sourceText: `
class MyClass {
  setThing(): void {
    return;
  }
}

class MySubclassExtendsMyClass extends MyClass {
  async setThing(): Promise<void> {
    await Promise.resolve();
  }
}
      `,
			options: nil,
			wantIds: []string{"voidReturnInheritedMethod"},
		},
		{
			name:     "upstream invalid 55",
			fileName: noMisusedPromisesFile,
			sourceText: `
class MyClass {
  setThing(): void {
    return;
  }
}

abstract class MyAbstractClassExtendsMyClass extends MyClass {
  abstract setThing(): Promise<void>;
}
      `,
			options: nil,
			wantIds: []string{"voidReturnInheritedMethod"},
		},
		{
			name:     "upstream invalid 56",
			fileName: noMisusedPromisesFile,
			sourceText: `
class MyClass {
  setThing(): void {
    return;
  }
}

interface MyInterfaceExtendsMyClass extends MyClass {
  setThing(): Promise<void>;
}
      `,
			options: nil,
			wantIds: []string{"voidReturnInheritedMethod"},
		},
		{
			name:     "upstream invalid 57",
			fileName: noMisusedPromisesFile,
			sourceText: `
abstract class MyAbstractClass {
  abstract setThing(): void;
}

class MySubclassExtendsMyAbstractClass extends MyAbstractClass {
  async setThing(): Promise<void> {
    await Promise.resolve();
  }
}
      `,
			options: nil,
			wantIds: []string{"voidReturnInheritedMethod"},
		},
		{
			name:     "upstream invalid 58",
			fileName: noMisusedPromisesFile,
			sourceText: `
abstract class MyAbstractClass {
  abstract setThing(): void;
}

abstract class MyAbstractSubclassExtendsMyAbstractClass extends MyAbstractClass {
  abstract setThing(): Promise<void>;
}
      `,
			options: nil,
			wantIds: []string{"voidReturnInheritedMethod"},
		},
		{
			name:     "upstream invalid 59",
			fileName: noMisusedPromisesFile,
			sourceText: `
abstract class MyAbstractClass {
  abstract setThing(): void;
}

interface MyInterfaceExtendsMyAbstractClass extends MyAbstractClass {
  setThing(): Promise<void>;
}
      `,
			options: nil,
			wantIds: []string{"voidReturnInheritedMethod"},
		},
		{
			name:     "upstream invalid 60",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface MyInterface {
  setThing(): void;
}

class MyInterfaceSubclass implements MyInterface {
  async setThing(): Promise<void> {
    await Promise.resolve();
  }
}
      `,
			options: nil,
			wantIds: []string{"voidReturnInheritedMethod"},
		},
		{
			name:     "upstream invalid 61",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface MyInterface {
  setThing(): void;
}

abstract class MyAbstractClassImplementsMyInterface implements MyInterface {
  abstract setThing(): Promise<void>;
}
      `,
			options: nil,
			wantIds: []string{"voidReturnInheritedMethod"},
		},
		{
			name:     "upstream invalid 62",
			fileName: noMisusedPromisesFile,
			sourceText: `
class MyClass {
  accessor setThing = (): void => {
    return;
  };
}

class MySubclassExtendsMyClass extends MyClass {
  accessor setThing = async (): Promise<void> => {
    await Promise.resolve();
  };
}
      `,
			options: nil,
			wantIds: []string{"voidReturnInheritedMethod"},
		},
		{
			name:     "upstream invalid 63",
			fileName: noMisusedPromisesFile,
			sourceText: `
abstract class MyClass {
  abstract accessor setThing: () => void;
}

abstract class MySubclassExtendsMyClass extends MyClass {
  abstract accessor setThing: () => Promise<void>;
}
      `,
			options: nil,
			wantIds: []string{"voidReturnInheritedMethod"},
		},
		{
			name:     "upstream invalid 64",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface MyInterface {
  setThing(): void;
}

interface MySubInterface extends MyInterface {
  setThing(): Promise<void>;
}
      `,
			options: nil,
			wantIds: []string{"voidReturnInheritedMethod"},
		},
		{
			name:     "upstream invalid 65",
			fileName: noMisusedPromisesFile,
			sourceText: `
type MyTypeIntersection = { setThing(): void } & { thing: number };

class MyClassImplementsMyTypeIntersection implements MyTypeIntersection {
  thing = 1;
  async setThing(): Promise<void> {
    await Promise.resolve();
  }
}
      `,
			options: nil,
			wantIds: []string{"voidReturnInheritedMethod"},
		},
		{
			name:     "upstream invalid 66",
			fileName: noMisusedPromisesFile,
			sourceText: `
type MyGenericType<IsAsync extends boolean = true> = IsAsync extends true
  ? { setThing(): Promise<void> }
  : { setThing(): void };

interface MyAsyncInterface extends MyGenericType<false> {
  setThing(): Promise<void>;
}
      `,
			options: nil,
			wantIds: []string{"voidReturnInheritedMethod"},
		},
		{
			name:     "upstream invalid 67",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface MyInterface {
  setThing(): void;
}

interface MyOtherInterface {
  setThing(): void;
}

interface MyThirdInterface extends MyInterface, MyOtherInterface {
  setThing(): Promise<void>;
}
      `,
			options: nil,
			wantIds: []string{"voidReturnInheritedMethod", "voidReturnInheritedMethod"},
		},
		{
			name:     "upstream invalid 68",
			fileName: noMisusedPromisesFile,
			sourceText: `
class MyClass {
  setThing(): void {
    return;
  }
}

class MyOtherClass {
  setThing(): void {
    return;
  }
}

interface MyInterface extends MyClass, MyOtherClass {
  setThing(): Promise<void>;
}
      `,
			options: nil,
			wantIds: []string{"voidReturnInheritedMethod", "voidReturnInheritedMethod"},
		},
		{
			name:     "upstream invalid 69",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface MyAsyncInterface {
  setThing(): Promise<void>;
}

interface MySyncInterface {
  setThing(): void;
}

class MyClass {
  setThing(): void {
    return;
  }
}

class MySubclass extends MyClass implements MyAsyncInterface, MySyncInterface {
  async setThing(): Promise<void> {
    await Promise.resolve();
  }
}
      `,
			options: nil,
			wantIds: []string{"voidReturnInheritedMethod", "voidReturnInheritedMethod"},
		},
		{
			name:     "upstream invalid 70",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface MyInterface {
  setThing(): void;
}

const MyClassExpressionExtendsMyClass = class implements MyInterface {
  setThing(): Promise<void> {
    await Promise.resolve();
  }
};
      `,
			options: nil,
			wantIds: []string{"voidReturnInheritedMethod"},
		},
		{
			name:     "upstream invalid 71",
			fileName: noMisusedPromisesFile,
			sourceText: `
const MyClassExpression = class {
  setThing(): void {
    return;
  }
};

class MyClassExtendsMyClassExpression extends MyClassExpression {
  async setThing(): Promise<void> {
    await Promise.resolve();
  }
}
      `,
			options: nil,
			wantIds: []string{"voidReturnInheritedMethod"},
		},
		{
			name:     "upstream invalid 72",
			fileName: noMisusedPromisesFile,
			sourceText: `
const MyClassExpression = class {
  setThing(): void {
    return;
  }
};
type MyClassExpressionType = typeof MyClassExpression;

interface MyInterfaceExtendsMyClassExpression extends MyClassExpressionType {
  setThing(): Promise<void>;
}
      `,
			options: nil,
			wantIds: []string{"voidReturnInheritedMethod"},
		},
		{
			name:     "upstream invalid 73",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface MySyncInterface {
  (): void;
  (arg: string): void;
  new (): void;
  [key: string]: () => void;
  [key: number]: () => void;
  myMethod(): void;
}
interface MyAsyncInterface extends MySyncInterface {
  (): Promise<void>;
  (arg: string): Promise<void>;
  new (): Promise<void>;
  [key: string]: () => Promise<void>;
  [key: number]: () => Promise<void>;
  myMethod(): Promise<void>;
}
      `,
			options: nil,
			wantIds: []string{"voidReturnInheritedMethod"},
		},
		{
			name:     "upstream invalid 74",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface MyCall {
  (): void;
  (arg: string): void;
}

interface MyIndex {
  [key: string]: () => void;
  [key: number]: () => void;
}

interface MyConstruct {
  new (): void;
  new (arg: string): void;
}

interface MyMethods {
  doSyncThing(): void;
  doOtherSyncThing(): void;
  syncMethodProperty: () => void;
}
interface MyInterface extends MyCall, MyIndex, MyConstruct, MyMethods {
  (): void;
  (arg: string): Promise<void>;
  new (): void;
  new (arg: string): void;
  [key: string]: () => Promise<void>;
  [key: number]: () => void;
  doSyncThing(): Promise<void>;
  doAsyncThing(): Promise<void>;
  syncMethodProperty: () => Promise<void>;
}
      `,
			options: nil,
			wantIds: []string{"voidReturnInheritedMethod", "voidReturnInheritedMethod"},
		},
		{
			name:     "upstream invalid 75",
			fileName: noMisusedPromisesFile,
			sourceText: `
declare function isTruthy(value: unknown): Promise<boolean>;
[0, 1, 2].filter(isTruthy);
      `,
			options: nil,
			wantIds: []string{"predicate"},
		},
		{
			name:     "upstream invalid 76",
			fileName: noMisusedPromisesFile,
			sourceText: `
const array: number[] = [];
array.every(() => Promise.resolve(true));
      `,
			options: nil,
			wantIds: []string{"predicate"},
		},
		{
			name:     "upstream invalid 77",
			fileName: noMisusedPromisesFile,
			sourceText: `
const array: (string[] & { foo: 'bar' }) | (number[] & { bar: 'foo' }) = [];
array.every(() => Promise.resolve(true));
      `,
			options: nil,
			wantIds: []string{"predicate"},
		},
		{
			name:     "upstream invalid 78 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
const tuple: [number, number, number] = [1, 2, 3];
tuple.find(() => Promise.resolve(false));
      `,
			options: NoMisusedPromisesOptions{ChecksConditionals: type_checking.Ref(true)},
			wantIds: []string{"predicate"},
		},
		{
			name:     "upstream invalid 79",
			fileName: noMisusedPromisesFile,
			sourceText: `
type ReturnsVoid = () => void;
declare const useCallback: <T extends (...args: unknown[]) => unknown>(
  fn: T,
) => T;
declare const useCallbackReturningVoid: typeof useCallback<ReturnsVoid>;
useCallbackReturningVoid(async () => {});
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 80",
			fileName: noMisusedPromisesFile,
			sourceText: `
type ReturnsVoid = () => void;
declare const useCallback: <T extends (...args: unknown[]) => unknown>(
  fn: T,
) => T;
useCallback<ReturnsVoid>(async () => {});
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 81",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface Foo<T> {
  (callback: () => T): void;
  (callback: () => number): void;
}
declare const foo: Foo<void>;

foo(async () => {});
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument"},
		},
		{
			name:     "upstream invalid 82",
			fileName: noMisusedPromisesFile,
			sourceText: `
declare function tupleFn<T extends (...args: unknown[]) => unknown>(
  ...fns: [T, string, T]
): void;
tupleFn<() => void>(
  async () => {},
  'foo',
  async () => {},
);
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument", "voidReturnArgument"},
		},
		{
			name:     "upstream invalid 83",
			fileName: noMisusedPromisesFile,
			sourceText: `
declare function arrayFn<T extends (...args: unknown[]) => unknown>(
  ...fns: (T | string)[]
): void;
arrayFn<() => void>(
  async () => {},
  'foo',
  async () => {},
);
      `,
			options: nil,
			wantIds: []string{"voidReturnArgument", "voidReturnArgument"},
		},
		{
			name:     "upstream invalid 84",
			fileName: noMisusedPromisesFile,
			sourceText: `
type HasVoidMethod = {
  f(): void;
};

const o: HasVoidMethod = {
  async f() {
    return 3;
  },
};
      `,
			options: nil,
			wantIds: []string{"voidReturnProperty"},
		},
		{
			name:     "upstream invalid 85",
			fileName: noMisusedPromisesFile,
			sourceText: `
type HasVoidMethod = {
  f(): void;
};

const o: HasVoidMethod = {
  async f(): Promise<number> {
    return 3;
  },
};
      `,
			options: nil,
			wantIds: []string{"voidReturnProperty"},
		},
		{
			name:     "upstream invalid 86",
			fileName: noMisusedPromisesFile,
			sourceText: `
type HasVoidMethod = {
  f(): void;
};
const obj: HasVoidMethod = {
  f() {
    return Promise.resolve('foo');
  },
};
      `,
			options: nil,
			wantIds: []string{"voidReturnProperty"},
		},
		{
			name:     "upstream invalid 87",
			fileName: noMisusedPromisesFile,
			sourceText: `
type HasVoidMethod = {
  f(): void;
};
const obj: HasVoidMethod = {
  f(): Promise<void> {
    throw new Error();
  },
};
      `,
			options: nil,
			wantIds: []string{"voidReturnProperty"},
		},
		{
			name:     "upstream invalid 88",
			fileName: noMisusedPromisesFile,
			sourceText: `
type O = { f: () => void };
const asyncFunction = async () => 'foo';
const obj: O = {
  f: asyncFunction,
};
      `,
			options: nil,
			wantIds: []string{"voidReturnProperty"},
		},
		{
			name:     "upstream invalid 89",
			fileName: noMisusedPromisesFile,
			sourceText: `
type O = { f: () => void };
const obj: O = {
  f: async (): Promise<string> => 'foo',
};
      `,
			options: nil,
			wantIds: []string{"voidReturnProperty"},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedWithOptions(test, NoMisusedPromises, testCase.fileName, testCase.sourceText, testCase.options)
			rule_testing.ExpectFindings(test, result, testCase.wantIds...)
		})
	}
}
