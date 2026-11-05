package typescript

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// TestNoMisusedPromisesStaysSilent carries all one hundred and twenty-three of tsgolint's valid
// cases.
//
// Extracted from tsgolint's own test file by parsing it with go/ast rather than by transcribing it.
// That is not a stylistic preference: a fixture written by hand encodes the same belief as the port
// it is meant to check, so it passes for exactly the reason the code is wrong. Every source string
// and every option expression below came out of that parse, and the extractor's counts were verified
// against the upstream file independently (`grep -c 'Code:'` gives 213 against 123 valid plus 90
// invalid, and `grep -c 'MessageId:'` gives 106 against 106 extracted diagnostics) so that an
// extraction bug (which looks exactly like a port bug) could not hide.
//
// The options are half of what this table proves. Fifty-six of the two hundred and thirteen cases set
// at least one option, and the same source text appears here as silent and in the Fires table as
// reporting, differing only in which flag was set. A port that ignored options would fail both tables
// rather than neither, but a port that read the wrong flag for a given position would pass one and
// fail the other, which is the failure this split is shaped to catch.
func TestNoMisusedPromisesStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		fileName   string
		sourceText string
		options    rule.Given[NoMisusedPromisesOptions]
	}{
		{
			name:     "upstream valid 0",
			fileName: noMisusedPromisesFile,
			sourceText: `
if (true) {
}
    `,
		},
		{
			name:     "upstream valid 1 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
if (Promise.resolve()) {
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksConditionals: type_checking.Ref(false)}),
		},
		{
			name:     "upstream valid 2",
			fileName: noMisusedPromisesFile,
			sourceText: `
if (true) {
} else if (false) {
} else {
}
    `,
		},
		{
			name:     "upstream valid 3 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
if (Promise.resolve()) {
} else if (Promise.resolve()) {
} else {
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksConditionals: type_checking.Ref(false)}),
		},
		{
			name:       "upstream valid 4",
			fileName:   noMisusedPromisesFile,
			sourceText: `for (;;) {}`,
		},
		{
			name:       "upstream valid 5",
			fileName:   noMisusedPromisesFile,
			sourceText: `for (let i; i < 10; i++) {}`,
		},
		{
			name:       "upstream valid 6 [options]",
			fileName:   noMisusedPromisesFile,
			sourceText: `for (let i; Promise.resolve(); i++) {}`,
			options:    rule.Configured(NoMisusedPromisesOptions{ChecksConditionals: type_checking.Ref(false)}),
		},
		{
			name:       "upstream valid 7",
			fileName:   noMisusedPromisesFile,
			sourceText: `do {} while (true);`,
		},
		{
			name:       "upstream valid 8 [options]",
			fileName:   noMisusedPromisesFile,
			sourceText: `do {} while (Promise.resolve());`,
			options:    rule.Configured(NoMisusedPromisesOptions{ChecksConditionals: type_checking.Ref(false)}),
		},
		{
			name:       "upstream valid 9",
			fileName:   noMisusedPromisesFile,
			sourceText: `while (true) {}`,
		},
		{
			name:       "upstream valid 10 [options]",
			fileName:   noMisusedPromisesFile,
			sourceText: `while (Promise.resolve()) {}`,
			options:    rule.Configured(NoMisusedPromisesOptions{ChecksConditionals: type_checking.Ref(false)}),
		},
		{
			name:       "upstream valid 11",
			fileName:   noMisusedPromisesFile,
			sourceText: `true ? 123 : 456;`,
		},
		{
			name:       "upstream valid 12 [options]",
			fileName:   noMisusedPromisesFile,
			sourceText: `Promise.resolve() ? 123 : 456;`,
			options:    rule.Configured(NoMisusedPromisesOptions{ChecksConditionals: type_checking.Ref(false)}),
		},
		{
			name:     "upstream valid 13",
			fileName: noMisusedPromisesFile,
			sourceText: `
if (!true) {
}
    `,
		},
		{
			name:     "upstream valid 14 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
if (!Promise.resolve()) {
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksConditionals: type_checking.Ref(false)}),
		},
		{
			name:       "upstream valid 15",
			fileName:   noMisusedPromisesFile,
			sourceText: `(await Promise.resolve()) || false;`,
		},
		{
			name:       "upstream valid 16 [options]",
			fileName:   noMisusedPromisesFile,
			sourceText: `Promise.resolve() || false;`,
			options:    rule.Configured(NoMisusedPromisesOptions{ChecksConditionals: type_checking.Ref(false)}),
		},
		{
			name:       "upstream valid 17",
			fileName:   noMisusedPromisesFile,
			sourceText: `(true && (await Promise.resolve())) || false;`,
		},
		{
			name:       "upstream valid 18 [options]",
			fileName:   noMisusedPromisesFile,
			sourceText: `(true && Promise.resolve()) || false;`,
			options:    rule.Configured(NoMisusedPromisesOptions{ChecksConditionals: type_checking.Ref(false)}),
		},
		{
			name:       "upstream valid 19",
			fileName:   noMisusedPromisesFile,
			sourceText: `false || (true && Promise.resolve());`,
		},
		{
			name:       "upstream valid 20",
			fileName:   noMisusedPromisesFile,
			sourceText: `(true && Promise.resolve()) || false;`,
		},
		{
			name:     "upstream valid 21",
			fileName: noMisusedPromisesFile,
			sourceText: `
async function test() {
  if (await Promise.resolve()) {
  }
}
    `,
		},
		{
			name:     "upstream valid 22",
			fileName: noMisusedPromisesFile,
			sourceText: `
async function test() {
  const mixed: Promise | undefined = Promise.resolve();
  if (mixed) {
    await mixed;
  }
}
    `,
		},
		{
			name:     "upstream valid 23",
			fileName: noMisusedPromisesFile,
			sourceText: `
if (~Promise.resolve()) {
}
    `,
		},
		{
			name:     "upstream valid 24",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface NotQuiteThenable {
  then(param: string): void;
  then(): void;
}
const value: NotQuiteThenable = { then() {} };
if (value) {
}
    `,
		},
		{
			name:       "upstream valid 25",
			fileName:   noMisusedPromisesFile,
			sourceText: `[1, 2, 3].forEach(val => {});`,
		},
		{
			name:       "upstream valid 26 [options]",
			fileName:   noMisusedPromisesFile,
			sourceText: `[1, 2, 3].forEach(async val => {});`,
			options:    rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturn: type_checking.Ref(false)}),
		},
		{
			name:       "upstream valid 27",
			fileName:   noMisusedPromisesFile,
			sourceText: `new Promise((resolve, reject) => resolve());`,
		},
		{
			name:       "upstream valid 28 [options]",
			fileName:   noMisusedPromisesFile,
			sourceText: `new Promise(async (resolve, reject) => resolve());`,
			options:    rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturn: type_checking.Ref(false)}),
		},
		{
			name:     "upstream valid 29",
			fileName: noMisusedPromisesFile,
			sourceText: `
Promise.all(
  ['abc', 'def'].map(async val => {
    await val;
  }),
);
    `,
		},
		{
			name:     "upstream valid 30",
			fileName: noMisusedPromisesFile,
			sourceText: `
const fn: (arg: () => Promise<void> | void) => void = () => {};
fn(() => Promise.resolve());
    `,
		},
		{
			name:     "upstream valid 31",
			fileName: noMisusedPromisesFile,
			sourceText: `
declare const returnsPromise: (() => Promise<void>) | null;
if (returnsPromise?.()) {
}
    `,
		},
		{
			name:     "upstream valid 32",
			fileName: noMisusedPromisesFile,
			sourceText: `
declare const returnsPromise: { call: () => Promise<void> } | null;
if (returnsPromise?.call()) {
}
    `,
		},
		{
			name:       "upstream valid 33",
			fileName:   noMisusedPromisesFile,
			sourceText: `Promise.resolve() ?? false;`,
		},
		{
			name:     "upstream valid 34",
			fileName: noMisusedPromisesFile,
			sourceText: `
function test(a: Promise<void> | undefinded) {
  const foo = a ?? Promise.reject();
}
    `,
		},
		{
			name:     "upstream valid 35",
			fileName: noMisusedPromisesFile,
			sourceText: `
function test(p: Promise<boolean> | undefined, bool: boolean) {
  if (p ?? bool) {
  }
}
    `,
		},
		{
			name:     "upstream valid 36",
			fileName: noMisusedPromisesFile,
			sourceText: `
async function test(p: Promise<boolean | undefined>, bool: boolean) {
  if ((await p) ?? bool) {
  }
}
    `,
		},
		{
			name:     "upstream valid 37",
			fileName: noMisusedPromisesFile,
			sourceText: `
async function test(p: Promise<boolean> | undefined) {
  if (await (p ?? Promise.reject())) {
  }
}
    `,
		},
		{
			name:     "upstream valid 38",
			fileName: noMisusedPromisesFile,
			sourceText: `
declare let a: Promise<string> | undefined

declare let b: Promise<string>

a = a ?? b
		`,
		},
		{
			name:     "upstream valid 39",
			fileName: noMisusedPromisesFile,
			sourceText: `
let f;
f = async () => 10;
    `,
		},
		{
			name:     "upstream valid 40",
			fileName: noMisusedPromisesFile,
			sourceText: `
let f: () => Promise<void>;
f = async () => 10;
const g = async () => 0;
const h: () => Promise<void> = async () => 10;
    `,
		},
		{
			name:     "upstream valid 41",
			fileName: noMisusedPromisesFile,
			sourceText: `
const obj = {
  f: async () => 10,
};
    `,
		},
		{
			name:     "upstream valid 42",
			fileName: noMisusedPromisesFile,
			sourceText: `
const f = async () => 123;
const obj = {
  f,
};
    `,
		},
		{
			name:     "upstream valid 43",
			fileName: noMisusedPromisesFile,
			sourceText: `
const obj = {
  async f() {
    return 0;
  },
};
    `,
		},
		{
			name:     "upstream valid 44",
			fileName: noMisusedPromisesFile,
			sourceText: `
type O = { f: () => Promise<void>; g: () => Promise<void> };
const g = async () => 0;
const obj: O = {
  f: async () => 10,
  g,
};
    `,
		},
		{
			name:     "upstream valid 45",
			fileName: noMisusedPromisesFile,
			sourceText: `
type O = { f: () => Promise<void> };
const name = 'f';
const obj: O = {
  async [name]() {
    return 10;
  },
};
    `,
		},
		{
			name:     "upstream valid 46",
			fileName: noMisusedPromisesFile,
			sourceText: `
const obj: number = {
  g() {
    return 10;
  },
};
    `,
		},
		{
			name:     "upstream valid 47",
			fileName: noMisusedPromisesFile,
			sourceText: `
const obj = {
  f: async () => 'foo',
  async g() {
    return 0;
  },
};
    `,
		},
		{
			name:     "upstream valid 48",
			fileName: noMisusedPromisesFile,
			sourceText: `
function f() {
  return async () => 0;
}
function g() {
  return;
}
    `,
		},
		{
			name:     "upstream valid 49 [tsx]",
			fileName: noMisusedPromisesTsxFile,
			sourceText: `
type O = {
  bool: boolean;
  func: () => Promise<void>;
};
const Component = (obj: O) => null;
<Component bool func={async () => 10} />;
      `,
		},
		{
			name:     "upstream valid 50 [tsx]",
			fileName: noMisusedPromisesTsxFile,
			sourceText: `
const Component: any = () => null;
<Component func={async () => 10} />;
      `,
		},
		{
			name:     "upstream valid 51",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface ItLike {
  (name: string, callback: () => Promise<void>): void;
  (name: string, callback: () => void): void;
}

declare const it: ItLike;

it('', async () => {});
      `,
		},
		{
			name:     "upstream valid 52",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface ItLike {
  (name: string, callback: () => void): void;
  (name: string, callback: () => Promise<void>): void;
}

declare const it: ItLike;

it('', async () => {});
      `,
		},
		{
			name:     "upstream valid 53",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface ItLike {
  (name: string, callback: () => void): void;
}
interface ItLike {
  (name: string, callback: () => Promise<void>): void;
}

declare const it: ItLike;

it('', async () => {});
      `,
		},
		{
			name:     "upstream valid 54",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface ItLike {
  (name: string, callback: () => Promise<void>): void;
}
interface ItLike {
  (name: string, callback: () => void): void;
}

declare const it: ItLike;

it('', async () => {});
      `,
		},
		{
			name:     "upstream valid 55 [tsx]",
			fileName: noMisusedPromisesTsxFile,
			sourceText: `
interface Props {
  onEvent: (() => void) | (() => Promise<void>);
}

declare function Component(props: Props): any;

const _ = <Component onEvent={async () => {}} />;
      `,
		},
		{
			name:     "upstream valid 56",
			fileName: noMisusedPromisesFile,
			sourceText: `
console.log({ ...(await Promise.resolve({ key: 42 })) });
    `,
		},
		{
			name:     "upstream valid 57",
			fileName: noMisusedPromisesFile,
			sourceText: `
const getData = () => Promise.resolve({ key: 42 });

console.log({
  someData: 42,
  ...(await getData()),
});
    `,
		},
		{
			name:     "upstream valid 58",
			fileName: noMisusedPromisesFile,
			sourceText: `
declare const condition: boolean;

console.log({ ...(condition && (await Promise.resolve({ key: 42 }))) });
console.log({ ...(condition || (await Promise.resolve({ key: 42 }))) });
console.log({ ...(condition ? {} : await Promise.resolve({ key: 42 })) });
console.log({ ...(condition ? await Promise.resolve({ key: 42 }) : {}) });
    `,
		},
		{
			name:     "upstream valid 59",
			fileName: noMisusedPromisesFile,
			sourceText: `
console.log([...(await Promise.resolve(42))]);
    `,
		},
		{
			name:     "upstream valid 60 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
console.log({ ...Promise.resolve({ key: 42 }) });
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksSpreads: type_checking.Ref(false)}),
		},
		{
			name:     "upstream valid 61 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
const getData = () => Promise.resolve({ key: 42 });

console.log({
  someData: 42,
  ...getData(),
});
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksSpreads: type_checking.Ref(false)}),
		},
		{
			name:     "upstream valid 62 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
declare const condition: boolean;

console.log({ ...(condition && Promise.resolve({ key: 42 })) });
console.log({ ...(condition || Promise.resolve({ key: 42 })) });
console.log({ ...(condition ? {} : Promise.resolve({ key: 42 })) });
console.log({ ...(condition ? Promise.resolve({ key: 42 }) : {}) });
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksSpreads: type_checking.Ref(false)}),
		},
		{
			name:     "upstream valid 63 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
// This is invalid Typescript, but it shouldn't trigger this linter specifically
console.log([...Promise.resolve(42)]);
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksSpreads: type_checking.Ref(false)}),
		},
		{
			name:     "upstream valid 64",
			fileName: noMisusedPromisesFile,
			sourceText: `
function spreadAny(..._args: any): void {}

spreadAny(
  true,
  () => Promise.resolve(1),
  () => Promise.resolve(false),
);
    `,
		},
		{
			name:     "upstream valid 65",
			fileName: noMisusedPromisesFile,
			sourceText: `
function spreadArrayAny(..._args: Array<any>): void {}

spreadArrayAny(
  true,
  () => Promise.resolve(1),
  () => Promise.resolve(false),
);
    `,
		},
		{
			name:     "upstream valid 66",
			fileName: noMisusedPromisesFile,
			sourceText: `
function spreadArrayUnknown(..._args: Array<unknown>): void {}

spreadArrayUnknown(() => Promise.resolve(true), 1, 2);

function spreadArrayFuncPromise(
  ..._args: Array<() => Promise<undefined>>
): void {}

spreadArrayFuncPromise(
  () => Promise.resolve(undefined),
  () => Promise.resolve(undefined),
);
    `,
		},
		{
			name:     "upstream valid 67",
			fileName: noMisusedPromisesFile,
			sourceText: `
class TakeCallbacks {
  constructor(...callbacks: Array<() => void>) {}
}

new TakeCallbacks;
new TakeCallbacks();
new TakeCallbacks(
  () => 1,
  () => true,
);
    `,
		},
		{
			name:     "upstream valid 68",
			fileName: noMisusedPromisesFile,
			sourceText: `
class Foo {
  public static doThing(): void {}
}

class Bar extends Foo {
  public async doThing(): Promise<void> {}
}
    `,
		},
		{
			name:     "upstream valid 69",
			fileName: noMisusedPromisesFile,
			sourceText: `
class Foo {
  public doThing(): void {}
}

class Bar extends Foo {
  public static async doThing(): Promise<void> {}
}
    `,
		},
		{
			name:     "upstream valid 70",
			fileName: noMisusedPromisesFile,
			sourceText: `
class Foo {
  public doThing = (): void => {};
}

class Bar extends Foo {
  public static doThing = async (): Promise<void> => {};
}
    `,
		},
		{
			name:     "upstream valid 71",
			fileName: noMisusedPromisesFile,
			sourceText: `
class Foo {
  public doThing = (): void => {};
}

class Bar extends Foo {
  public static accessor doThing = async (): Promise<void> => {};
}
    `,
		},
		{
			name:     "upstream valid 72",
			fileName: noMisusedPromisesFile,
			sourceText: `
class Foo {
  public accessor doThing = (): void => {};
}

class Bar extends Foo {
  public static accessor doThing = (): void => {};
}
    `,
		},
		{
			name:     "upstream valid 73 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
class Foo {
  [key: string]: void;
}

class Bar extends Foo {
  [key: string]: Promise<void>;
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 74",
			fileName: noMisusedPromisesFile,
			sourceText: `
function restTuple(...args: []): void;
function restTuple(...args: [string]): void;
function restTuple(..._args: string[]): void {}

restTuple();
restTuple('Hello');
    `,
		},
		{
			name:     "upstream valid 75",
			fileName: noMisusedPromisesFile,
			sourceText: `
      let value: Record<string, () => void>;
      value.sync = () => {};
    `,
		},
		{
			name:     "upstream valid 76",
			fileName: noMisusedPromisesFile,
			sourceText: `
      type ReturnsRecord = () => Record<string, () => void>;

      const test: ReturnsRecord = () => {
        return { sync: () => {} };
      };
    `,
		},
		{
			name:     "upstream valid 77",
			fileName: noMisusedPromisesFile,
			sourceText: `
      type ReturnsRecord = () => Record<string, () => void>;

      function sync() {}

      const test: ReturnsRecord = () => {
        return { sync };
      };
    `,
		},
		{
			name:     "upstream valid 78",
			fileName: noMisusedPromisesFile,
			sourceText: `
      function withTextRecurser<Text extends string>(
        recurser: (text: Text) => void,
      ): (text: Text) => void {
        return (text: Text): void => {
          if (text.length) {
            return;
          }

          return recurser(node);
        };
      }
    `,
		},
		{
			name:     "upstream valid 79",
			fileName: noMisusedPromisesFile,
			sourceText: `
declare function foo(cb: undefined | (() => void));
declare const bar: undefined | (() => void);
foo(bar);
    `,
		},
		{
			name:     "upstream valid 80 [options] [tsx]",
			fileName: noMisusedPromisesTsxFile,
			sourceText: `
        type OnSelectNodeFn = (node: string | null) => void;

        interface ASTViewerBaseProps {
          readonly onSelectNode?: OnSelectNodeFn;
        }

        declare function ASTViewer(props: ASTViewerBaseProps): null;
        declare const onSelectFn: OnSelectNodeFn;

        <ASTViewer onSelectNode={onSelectFn} />;
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{Attributes: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 81 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
class MyClass {
  setThing(): void {
    return;
  }
}

class MySubclassExtendsMyClass extends MyClass {
  setThing(): void {
    return;
  }
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 82 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
class MyClass {
  async setThing(): Promise<void> {
    await Promise.resolve();
  }
}

class MySubclassExtendsMyClass extends MyClass {
  async setThing(): Promise<void> {
    await Promise.resolve();
  }
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 83 [options]",
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
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(false)},
			)}),
		},
		{
			name:     "upstream valid 84 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
class MyClass {
  setThing(): void {
    return;
  }
}

abstract class MyAbstractClassExtendsMyClass extends MyClass {
  abstract setThing(): void;
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 85 [options]",
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
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(false)},
			)}),
		},
		{
			name:     "upstream valid 86 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
class MyClass {
  setThing(): void {
    return;
  }
}

interface MyInterfaceExtendsMyClass extends MyClass {
  setThing(): void;
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 87 [options]",
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
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(false)},
			)}),
		},
		{
			name:     "upstream valid 88 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
abstract class MyAbstractClass {
  abstract setThing(): void;
}

class MySubclassExtendsMyAbstractClass extends MyAbstractClass {
  setThing(): void {
    return;
  }
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 89 [options]",
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
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(false)},
			)}),
		},
		{
			name:     "upstream valid 90 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
abstract class MyAbstractClass {
  abstract setThing(): void;
}

abstract class MyAbstractSubclassExtendsMyAbstractClass extends MyAbstractClass {
  abstract setThing(): void;
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 91 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
abstract class MyAbstractClass {
  abstract setThing(): void;
}

abstract class MyAbstractSubclassExtendsMyAbstractClass extends MyAbstractClass {
  abstract setThing(): Promise<void>;
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(false)},
			)}),
		},
		{
			name:     "upstream valid 92 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
abstract class MyAbstractClass {
  abstract setThing(): void;
}

interface MyInterfaceExtendsMyAbstractClass extends MyAbstractClass {
  setThing(): void;
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 93 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
abstract class MyAbstractClass {
  abstract setThing(): void;
}

interface MyInterfaceExtendsMyAbstractClass extends MyAbstractClass {
  setThing(): Promise<void>;
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(false)},
			)}),
		},
		{
			name:     "upstream valid 94 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface MyInterface {
  setThing(): void;
}

interface MySubInterfaceExtendsMyInterface extends MyInterface {
  setThing(): void;
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 95 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface MyInterface {
  setThing(): void;
}

interface MySubInterfaceExtendsMyInterface extends MyInterface {
  setThing(): Promise<void>;
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(false)},
			)}),
		},
		{
			name:     "upstream valid 96 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface MyInterface {
  setThing(): void;
}

class MyClassImplementsMyInterface implements MyInterface {
  setThing(): void {
    return;
  }
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 97 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface MyInterface {
  setThing(): void;
}

class MyClassImplementsMyInterface implements MyInterface {
  async setThing(): Promise<void> {
    await Promise.resolve();
  }
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(false)},
			)}),
		},
		{
			name:     "upstream valid 98 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface MyInterface {
  setThing(): void;
}

abstract class MyAbstractClassImplementsMyInterface implements MyInterface {
  abstract setThing(): void;
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 99 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface MyInterface {
  setThing(): void;
}

abstract class MyAbstractClassImplementsMyInterface implements MyInterface {
  abstract setThing(): Promise<void>;
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(false)},
			)}),
		},
		{
			name:     "upstream valid 100 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
type MyTypeLiteralsIntersection = { setThing(): void } & { thing: number };

class MyClass implements MyTypeLiteralsIntersection {
  thing = 1;
  setThing(): void {
    return;
  }
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 101 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
type MyTypeLiteralsIntersection = { setThing(): void } & { thing: number };

class MyClass implements MyTypeLiteralsIntersection {
  thing = 1;
  async setThing(): Promise<void> {
    await Promise.resolve();
  }
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(false)},
			)}),
		},
		{
			name:     "upstream valid 102 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
type MyGenericType<IsAsync extends boolean = true> = IsAsync extends true
  ? { setThing(): Promise<void> }
  : { setThing(): void };

interface MyAsyncInterface extends MyGenericType {
  setThing(): Promise<void>;
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 103 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
type MyGenericType<IsAsync extends boolean = true> = IsAsync extends true
  ? { setThing(): Promise<void> }
  : { setThing(): void };

interface MyAsyncInterface extends MyGenericType<false> {
  setThing(): Promise<void>;
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(false)},
			)}),
		},
		{
			name:     "upstream valid 104 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface MyInterface {
  setThing(): void;
}

interface MyOtherInterface {
  setThing(): void;
}

interface MyThirdInterface extends MyInterface, MyOtherInterface {
  setThing(): void;
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 105 [options]",
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
  setThing(): void;
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 106 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface MyInterface {
  setThing(): void;
}

interface MyOtherInterface {
  setThing(): void;
}

class MyClass {
  setThing(): void {
    return;
  }
}

class MySubclass extends MyClass implements MyInterface, MyOtherInterface {
  setThing(): void {
    return;
  }
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 107 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
class MyClass {
  setThing(): void {
    return;
  }
}

const MyClassExpressionExtendsMyClass = class extends MyClass {
  setThing(): void {
    return;
  }
};
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 108 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
const MyClassExpression = class {
  setThing(): void {
    return;
  }
};

class MyClassExtendsMyClassExpression extends MyClassExpression {
  setThing(): void {
    return;
  }
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 109 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
const MyClassExpression = class {
  setThing(): void {
    return;
  }
};
type MyClassExpressionType = typeof MyClassExpression;

interface MyInterfaceExtendsMyClassExpression extends MyClassExpressionType {
  setThing(): void;
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 110 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface MySyncCallSignatures {
  (): void;
  (arg: string): void;
}
interface MyAsyncInterface extends MySyncCallSignatures {
  (): Promise<void>;
  (arg: string): Promise<void>;
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 111 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface MySyncConstructSignatures {
  new (): void;
  new (arg: string): void;
}
interface ThisIsADifferentIssue extends MySyncConstructSignatures {
  new (): Promise<void>;
  new (arg: string): Promise<void>;
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 112 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface MySyncIndexSignatures {
  [key: string]: void;
  [key: number]: void;
}
interface ThisIsADifferentIssue extends MySyncIndexSignatures {
  [key: string]: Promise<void>;
  [key: number]: Promise<void>;
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 113 [options]",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface MySyncInterfaceSignatures {
  (): void;
  (arg: string): void;
  new (): void;
  [key: string]: () => void;
  [key: number]: () => void;
}
interface MyAsyncInterface extends MySyncInterfaceSignatures {
  (): Promise<void>;
  (arg: string): Promise<void>;
  new (): Promise<void>;
  [key: string]: () => Promise<void>;
  [key: number]: () => Promise<void>;
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:     "upstream valid 114 [options]",
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
  (arg: string): void;
  new (): void;
  new (arg: string): void;
  [key: string]: () => void;
  [key: number]: () => void;
  doSyncThing(): void;
  doAsyncThing(): Promise<void>;
  syncMethodProperty: () => void;
}
      `,
			options: rule.Configured(NoMisusedPromisesOptions{ChecksVoidReturnOpts: type_checking.Ref(
				NoMisusedPromisesChecksVoidReturnOptions{InheritedMethods: type_checking.Ref(true)},
			)}),
		},
		{
			name:       "upstream valid 115",
			fileName:   noMisusedPromisesFile,
			sourceText: `const notAFn1: string = '';`,
		},
		{
			name:       "upstream valid 116",
			fileName:   noMisusedPromisesFile,
			sourceText: `const notAFn2: number = 1;`,
		},
		{
			name:       "upstream valid 117",
			fileName:   noMisusedPromisesFile,
			sourceText: `const notAFn3: boolean = true;`,
		},
		{
			name:       "upstream valid 118",
			fileName:   noMisusedPromisesFile,
			sourceText: `const notAFn4: { prop: 1 } = { prop: 1 };`,
		},
		{
			name:       "upstream valid 119",
			fileName:   noMisusedPromisesFile,
			sourceText: `const notAFn5: {} = {};`,
		},
		{
			name:     "upstream valid 120",
			fileName: noMisusedPromisesFile,
			sourceText: `
const array: number[] = [1, 2, 3];
array.filter(a => a > 1);
    `,
		},
		{
			name:     "upstream valid 121",
			fileName: noMisusedPromisesFile,
			sourceText: `
type ReturnsPromiseVoid = () => Promise<void>;
declare const useCallback: <T extends (...args: unknown[]) => unknown>(
  fn: T,
) => T;
useCallback<ReturnsPromiseVoid>(async () => {});
    `,
		},
		{
			name:     "upstream valid 122",
			fileName: noMisusedPromisesFile,
			sourceText: `
type ReturnsVoid = () => void;
type ReturnsPromiseVoid = () => Promise<void>;
declare const useCallback: <T extends (...args: unknown[]) => unknown>(
  fn: T,
) => T;
useCallback<ReturnsVoid | ReturnsPromiseVoid>(async () => {});
    `,
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedGiven(
				test,
				NoMisusedPromises,
				testCase.fileName,
				testCase.sourceText,
				testCase.options,
			)
			rule_testing.ExpectClean(test, result)
		})
	}
}
