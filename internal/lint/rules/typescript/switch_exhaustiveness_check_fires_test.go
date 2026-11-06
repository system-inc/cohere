package typescript

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"

	"github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// TestSwitchExhaustivenessCheckFires carries forty-seven of tsgolint's fifty-two invalid cases,
// asserting fifty-one findings in upstream's own order. The fifty-second case needs a second file
// and has its own test, TestSwitchExhaustivenessCheckResolvesAnEnumAcrossAModuleBoundary.
//
// Four of these inputs report twice on one switch statement, once for a missing literal branch and
// once for the missing default, because `checkSwitchExhaustive` and `checkSwitchNoUnionDefaultCase`
// are independent checks over one shared metadata value and both can fire on the same node. A
// fixture asserting a single id per input would have passed while silently dropping half the
// findings, which is why this asserts an ordered list rather than a presence.
//
// `addMissingCases` never appears here, and cannot: it is tsgolint's suggestion message and
// tsgolint ships no suggestions. Only `switchIsNotExhaustive` and `dangerousDefaultCase` are
// reachable, which TestSwitchExhaustivenessCheckShipsNoRepairs pins from the other direction.
//
// Two of these sources are written as interpreted Go strings rather than raw ones, because the
// TypeScript they contain has backticks in an enum member name and a Go raw literal cannot hold
// one. Upstream writes them as concatenations for the same reason. They are here rather than
// dropped because an enum key that is a template literal, or one containing an escaped quote, is
// exactly the shape a name-based implementation would mishandle and a type-identity-based one
// handles for free.
func TestSwitchExhaustivenessCheckFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		options    rule.Given[SwitchExhaustivenessCheckOptions]
		wantIds    []string
	}{
		{"upstream invalid 0 [allowDefault=false, requireDefault=true]",
			`
declare const value: 'literal';
switch (value) {
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(false),
				RequireDefaultForNonUnion:           type_checking.Ref(true),
			}),
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 1 [allowDefault=false, requireDefault=true]",
			`
declare const value: 'literal' & { _brand: true };
switch (value) {
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(false),
				RequireDefaultForNonUnion:           type_checking.Ref(true),
			}),
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 2 [allowDefault=false, requireDefault=true]",
			`
declare const value: ('literal' & { _brand: true }) | 1;
switch (value) {
  case 'literal':
    break;
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(false),
				RequireDefaultForNonUnion:           type_checking.Ref(true),
			}),
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 3 [allowDefault=true, requireDefault=false]",
			`
declare const value: '1' | '2' | number;
switch (value) {
  case '1':
    break;
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(true),
				RequireDefaultForNonUnion:           type_checking.Ref(false),
			}),
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 4 [allowDefault=true, requireDefault=true]",
			`
declare const value: '1' | '2' | number;
switch (value) {
  case '1':
    break;
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(true),
				RequireDefaultForNonUnion:           type_checking.Ref(true),
			}),
			[]string{"switchIsNotExhaustive", "switchIsNotExhaustive"}},
		{"upstream invalid 5 [allowDefault=true, requireDefault=true]",
			`
declare const value: (string & { foo: 'bar' }) | '1';
switch (value) {
  case '1':
    break;
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(true),
				RequireDefaultForNonUnion:           type_checking.Ref(true),
			}),
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 6 [allowDefault=false, requireDefault=true]",
			`
declare const value: (string & { foo: 'bar' }) | '1' | 1 | null | undefined;
switch (value) {
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(false),
				RequireDefaultForNonUnion:           type_checking.Ref(true),
			}),
			[]string{"switchIsNotExhaustive", "switchIsNotExhaustive"}},
		{"upstream invalid 7 [allowDefault=false, requireDefault=true]",
			`
declare const value: string | number;
switch (value) {
  case 1:
    break;
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(false),
				RequireDefaultForNonUnion:           type_checking.Ref(true),
			}),
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 8 [allowDefault=false, requireDefault=true]",
			`
declare const value: number;
declare const a: number;
switch (value) {
  case a:
    break;
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(false),
				RequireDefaultForNonUnion:           type_checking.Ref(true),
			}),
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 9 [allowDefault=false, requireDefault=true]",
			`
declare const value: bigint;
switch (value) {
  case 10n:
    break;
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(false),
				RequireDefaultForNonUnion:           type_checking.Ref(true),
			}),
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 10 [allowDefault=false, requireDefault=true]",
			`
declare const value: symbol;
const a = Symbol('a');
switch (value) {
  case a:
    break;
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(false),
				RequireDefaultForNonUnion:           type_checking.Ref(true),
			}),
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 11 [allowDefault=false, requireDefault=true]",
			`
const a = Symbol('aa');
const b = Symbol('bb');
declare const value: typeof a | typeof b | 1;
switch (value) {
  case 1:
    break;
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(false),
				RequireDefaultForNonUnion:           type_checking.Ref(true),
			}),
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 12 [allowDefault=false, requireDefault=true]",
			`
const a = Symbol('a');
declare const value: typeof a | string;
switch (value) {
  case a:
    break;
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(false),
				RequireDefaultForNonUnion:           type_checking.Ref(true),
			}),
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 13 [allowDefault=false, requireDefault=false]",
			`
declare const value: boolean;
switch (value) {
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(false),
				RequireDefaultForNonUnion:           type_checking.Ref(false),
			}),
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 14 [allowDefault=false, requireDefault=true]",
			`
declare const value: boolean | 1;
switch (value) {
  case false:
    break;
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(false),
				RequireDefaultForNonUnion:           type_checking.Ref(true),
			}),
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 15 [allowDefault=false, requireDefault=true]",
			`
declare const value: boolean | number;
switch (value) {
  case 1:
    break;
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(false),
				RequireDefaultForNonUnion:           type_checking.Ref(true),
			}),
			[]string{"switchIsNotExhaustive", "switchIsNotExhaustive"}},
		{"upstream invalid 16 [allowDefault=false, requireDefault=true]",
			`
declare const value: object;
switch (value) {
  case 1:
    break;
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(false),
				RequireDefaultForNonUnion:           type_checking.Ref(true),
			}),
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 17 [allowDefault=true, requireDefault=true]",
			`
enum Aaa {
  Foo,
  Bar,
}
declare const value: Aaa | 1 | string;
switch (value) {
  case 1:
    break;
  case Aaa.Foo:
    break;
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(true),
				RequireDefaultForNonUnion:           type_checking.Ref(true),
			}),
			[]string{"switchIsNotExhaustive", "switchIsNotExhaustive"}},
		{"upstream invalid 18 [defaults]",
			`
type Day =
  | 'Monday'
  | 'Tuesday'
  | 'Wednesday'
  | 'Thursday'
  | 'Friday'
  | 'Saturday'
  | 'Sunday';

const day = 'Monday' as Day;
let result = 0;

switch (day) {
  case 'Monday': {
    result = 1;
    break;
  }
}
      `,
			rule.Given[SwitchExhaustivenessCheckOptions]{},
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 19 [defaults]",
			`
enum Enum {
  A,
  B,
}

function test(value: Enum): number {
  switch (value) {
    case Enum.A:
      return 1;
  }
}
      `,
			rule.Given[SwitchExhaustivenessCheckOptions]{},
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 20 [defaults]",
			`
type A = 'a';
type B = 'b';
type C = 'c';
type Union = A | B | C;

function test(value: Union): number {
  switch (value) {
    case 'a':
      return 1;
  }
}
      `,
			rule.Given[SwitchExhaustivenessCheckOptions]{},
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 21 [defaults]",
			`
const A = 'a';
const B = 1;
const C = true;

type Union = typeof A | typeof B | typeof C;

function test(value: Union): number {
  switch (value) {
    case 'a':
      return 1;
  }
}
      `,
			rule.Given[SwitchExhaustivenessCheckOptions]{},
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 22 [defaults]",
			`
type DiscriminatedUnion = { type: 'A'; a: 1 } | { type: 'B'; b: 2 };

function test(value: DiscriminatedUnion): number {
  switch (value.type) {
    case 'A':
      return 1;
  }
}
      `,
			rule.Given[SwitchExhaustivenessCheckOptions]{},
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 23 [defaults]",
			`
type Day =
  | 'Monday'
  | 'Tuesday'
  | 'Wednesday'
  | 'Thursday'
  | 'Friday'
  | 'Saturday'
  | 'Sunday';

const day = 'Monday' as Day;

switch (day) {
}
      `,
			rule.Given[SwitchExhaustivenessCheckOptions]{},
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 24 [defaults]",
			`
const a = Symbol('a');
const b = Symbol('b');
const c = Symbol('c');

type T = typeof a | typeof b | typeof c;

function test(value: T): number {
  switch (value) {
    case a:
      return 1;
  }
}
      `,
			rule.Given[SwitchExhaustivenessCheckOptions]{},
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 25 [defaults]",
			`
type T = 1 | 2;

function test(value: T): number {
  switch (value) {
    case 1:
      return 1;
  }
}
      `,
			rule.Given[SwitchExhaustivenessCheckOptions]{},
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 26 [defaults]",
			`
type T = 1 | 2;

function test(value: T): number {
  switch (value) {
  }
}
      `,
			rule.Given[SwitchExhaustivenessCheckOptions]{},
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 27 [defaults]",
			`
export enum Enum {
  'test-test' = 'test-test',
  'test' = 'test',
}

function test(arg: Enum): string {
  switch (arg) {
  }
}
      `,
			rule.Given[SwitchExhaustivenessCheckOptions]{},
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 28 [defaults]",
			`
export enum Enum {
  '' = 'test-test',
  'test' = 'test',
}

function test(arg: Enum): string {
  switch (arg) {
  }
}
      `,
			rule.Given[SwitchExhaustivenessCheckOptions]{},
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 29 [defaults]",
			`
export enum Enum {
  '9test' = 'test-test',
  'test' = 'test',
}

function test(arg: Enum): string {
  switch (arg) {
  }
}
      `,
			rule.Given[SwitchExhaustivenessCheckOptions]{},
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 30 [allowDefault=true, requireDefault=true]",
			`
const value: number = Math.floor(Math.random() * 3);
switch (value) {
  case 0:
    return 0;
  case 1:
    return 1;
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(true),
				RequireDefaultForNonUnion:           type_checking.Ref(true),
			}),
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 31 [defaults]",
			"\n        enum Enum {\n          'a' = 1,\n          [`key-with\n\n          new-line`] = 2,\n        }\n\n        declare const a: Enum;\n\n        switch (a) {\n        }\n      ",
			rule.Given[SwitchExhaustivenessCheckOptions]{},
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 32 [defaults]",
			"\n        enum Enum {\n          'a' = 1,\n          \"'a' `b` \\\"c\\\"\" = 2,\n        }\n\n        declare const a: Enum;\n\n        switch (a) {}\n      ",
			rule.Given[SwitchExhaustivenessCheckOptions]{},
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 33 [allowDefault=false, requireDefault=false]",
			`
type MyUnion = 'foo' | 'bar' | 'baz';

declare const myUnion: MyUnion;

switch (myUnion) {
  case 'foo':
  case 'bar':
  case 'baz': {
    break;
  }
  default: {
    break;
  }
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(false),
				RequireDefaultForNonUnion:           type_checking.Ref(false),
			}),
			[]string{"dangerousDefaultCase"}},
		{"upstream invalid 34 [allowDefault=false, requireDefault=false]",
			`
enum MyEnum {
  Foo = 'Foo',
  Bar = 'Bar',
  Baz = 'Baz',
}

declare const myEnum: MyEnum;

switch (myEnum) {
  case MyEnum.Foo:
  case MyEnum.Bar:
  case MyEnum.Baz: {
    break;
  }
  default: {
    break;
  }
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(false),
				RequireDefaultForNonUnion:           type_checking.Ref(false),
			}),
			[]string{"dangerousDefaultCase"}},
		{"upstream invalid 35 [allowDefault=false, requireDefault=false]",
			`
enum MyEnum {
  Foo,
  Bar,
  Baz,
}

declare const myEnum: MyEnum;

switch (myEnum) {
  case MyEnum.Foo:
  case MyEnum.Bar:
  case MyEnum.Baz: {
    break;
  }
  default: {
    break;
  }
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(false),
				RequireDefaultForNonUnion:           type_checking.Ref(false),
			}),
			[]string{"dangerousDefaultCase"}},
		{"upstream invalid 36 [allowDefault=false, requireDefault=false]",
			`
declare const myBoolean: boolean;

switch (myBoolean) {
  case true:
  case false: {
    break;
  }
  default: {
    break;
  }
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(false),
				RequireDefaultForNonUnion:           type_checking.Ref(false),
			}),
			[]string{"dangerousDefaultCase"}},
		{"upstream invalid 37 [allowDefault=false, requireDefault=false]",
			`
declare const myValue: undefined;

switch (myValue) {
  case undefined: {
    break;
  }

  default: {
    break;
  }
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(false),
				RequireDefaultForNonUnion:           type_checking.Ref(false),
			}),
			[]string{"dangerousDefaultCase"}},
		{"upstream invalid 38 [allowDefault=false, requireDefault=false]",
			`
declare const myValue: null;

switch (myValue) {
  case null: {
    break;
  }

  default: {
    break;
  }
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(false),
				RequireDefaultForNonUnion:           type_checking.Ref(false),
			}),
			[]string{"dangerousDefaultCase"}},
		{"upstream invalid 39 [allowDefault=false, requireDefault=false]",
			`
declare const myValue: 'foo' | boolean | undefined | null;

switch (myValue) {
  case 'foo':
  case true:
  case false:
  case undefined:
  case null: {
    break;
  }

  default: {
    break;
  }
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				AllowDefaultCaseForExhaustiveSwitch: type_checking.Ref(false),
				RequireDefaultForNonUnion:           type_checking.Ref(false),
			}),
			[]string{"dangerousDefaultCase"}},
		{"upstream invalid 40 [considerDefault=false]",
			`
declare const literal: 'a' | 'b';

switch (literal) {
  case 'a':
    break;
  default:
    break;
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				ConsiderDefaultExhaustiveForUnions: type_checking.Ref(false),
			}),
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 41 [defaults]",
			`
declare const literal: 'a' | 'b';

switch (literal) {
  case 'a':
    break;
}
`,
			rule.Given[SwitchExhaustivenessCheckOptions]{},
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 42 [considerDefault=false]",
			`
declare const literal: 'a' | 'b';

switch (literal) {
  default:
  case 'a':
    break;
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				ConsiderDefaultExhaustiveForUnions: type_checking.Ref(false),
			}),
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 43 [considerDefault=false]",
			`
declare const literal: 'a' | 'b' | 'c';

switch (literal) {
  case 'a':
    break;
  default:
    break;
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				ConsiderDefaultExhaustiveForUnions: type_checking.Ref(false),
			}),
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 44 [considerDefault=false]",
			`
enum MyEnum {
  Foo = 'Foo',
  Bar = 'Bar',
  Baz = 'Baz',
}

declare const myEnum: MyEnum;

switch (myEnum) {
  case MyEnum.Foo:
    break;
  default: {
    break;
  }
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				ConsiderDefaultExhaustiveForUnions: type_checking.Ref(false),
			}),
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 45 [considerDefault=false]",
			`
declare const value: boolean;
switch (value) {
  default: {
    break;
  }
}
      `,
			rule.Configured(SwitchExhaustivenessCheckOptions{
				ConsiderDefaultExhaustiveForUnions: type_checking.Ref(false),
			}),
			[]string{"switchIsNotExhaustive"}},
		{"upstream invalid 50 [defaults]",
			`
        export namespace A {
          export enum B {
            C,
            D,
          }
        }
        declare const foo: A.B;
        switch (foo) {
          case A.B.C: {
            break;
          }
        }
      `,
			rule.Given[SwitchExhaustivenessCheckOptions]{},
			[]string{"switchIsNotExhaustive"}},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectFindings(test, rule_testing.RunTypedGiven(
				test, SwitchExhaustivenessCheck, switchExhaustivenessFile, testCase.sourceText, testCase.options,
			), testCase.wantIds...)
		})
	}
}
