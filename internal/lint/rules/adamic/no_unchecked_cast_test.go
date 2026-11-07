package adamic

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

const shapes = `
type Circle = { readonly kind: 'Circle'; readonly radius: number };
type Square = { readonly kind: 'Square'; readonly side: number };
type Shape = Circle | Square;
interface User { readonly name: string }
declare const shape: Shape;
declare function load(): unknown;
`

// anyHolders is two generic shapes for a cast to a target that holds any below its top.
const anyHolders = `
interface Box<Value> { readonly value: Value }
interface Graph<Source, Context, Arguments> { readonly source: Source; readonly context: Context; readonly arguments: Arguments }
`

// requests is HttpJson.test.ts's shape in ahra: a request's options, spread into a fresh literal with a key
// they do not declare.
const requests = `
type Init = { readonly method?: string; readonly body?: string };
declare const init: Init;
`

// TestNoUncheckedCastFiresOnCastsNothingCouldCheck: adamic's refusal 2, and the hatches around it.
func TestNoUncheckedCastFiresOnCastsNothingCouldCheck(test *testing.T) {
	test.Parallel()
	for name, fixture := range map[string]struct {
		source string
		span   string
	}{
		// adamic's refusal 2
		"unknown to an interface": {shapes + `const user = load() as User;`, "load() as User"},
		"through unknown":         {shapes + `const user = (shape as unknown) as User;`, "(shape as unknown) as User"},
		"from any":                {shapes + `declare const raw: any; const user = raw as User;`, "raw as User"},
		"from an any array": {
			shapes + `declare const rows: any[]; const users = rows as User[];`,
			"rows as User[]",
		},
		"an angle bracket": {shapes + `const user = <User>load();`, "<User>load()"},
		// Adamic refuses explicit any at every position, type arguments included (adamic docs/rulings.md:39),
		// so an any one level down in the target lets nothing through.
		"from any to a target holding any": {
			anyHolders + `declare const rows: any[]; const boxes = rows as Box<any>[];`,
			"rows as Box<any>[]",
		},
		"from any to a target holding any thrice": {
			anyHolders + `declare const raw: any; const graph = raw as Graph<any, any, any>;`,
			"raw as Graph<any, any, any>",
		},
		"a fresh literal with a shared key of the wrong type": {
			requests + `declare const method: string | number;
const request = { ...init, method, duplex: 'half' } as Init;`,
			"{ ...init, method, duplex: 'half' } as Init",
		},
		"members with no tag": {
			`type A = { readonly a: number }; type B = { readonly b: number }; declare const either: A | B; const a = either as A;`,
			"either as A",
		},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			result := runAdamic(test, NoUncheckedCast, fixture.source)
			rule_testing.ExpectFindings(test, result, "uncheckedCast")
			expectSpans(test, fixture.source, result, fixture.span)
		})
	}
}

// TestNoUncheckedCastStaysCleanOnChecksAndUpcasts: what Adamic compiles, checked or not needing a check.
func TestNoUncheckedCastStaysCleanOnChecksAndUpcasts(test *testing.T) {
	test.Parallel()
	for name, source := range map[string]string{
		"a discriminated member":         shapes + `const circle = shape as Circle;`,
		"typeof apart":                   `declare const value: string | number; const text = value as string;`,
		"instanceof apart":               `class Cat { meow(): string { return ''; } } class Dog { bark(): string { return ''; } } declare const pet: Cat | Dog; const cat = pet as Cat;`,
		"undefined apart":                shapes + `declare const maybe: User | undefined; const user = maybe as User;`,
		"an upcast":                      shapes + `const any = shape as Shape | undefined;`,
		"as const":                       `const pair = [1, 2] as const;`,
		"to unknown":                     shapes + `const opaque = shape as unknown;`,
		"an any array to unknown[]":      `declare const rows: any[]; const opaque = rows as unknown[];`,
		"a fresh literal dropping a key": requests + `const request = { ...init, duplex: 'half' } as Init;`,
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test, runAdamic(test, NoUncheckedCast, source))
		})
	}
}

// TestNoUncheckedCastStaysCleanOnAdamicsPrograms: Adamic 0.1 compiles all ten.
func TestNoUncheckedCastStaysCleanOnAdamicsPrograms(test *testing.T) {
	test.Parallel()
	expectProgramsClean(test, NoUncheckedCast)
}

// noRuntimeChecks is the option a project on tsc sets: nothing backs a union downcast at runtime.
var noRuntimeChecks = NoUncheckedCastOptions{RuntimeChecksEmitted: false}

// unionDowncasts are the casts Adamic checks and tsc does not, each with the span a finding covers.
var unionDowncasts = map[string]struct {
	source string
	span   string
}{
	"a primitive union":   {`declare const value: string | true; const text = value as string;`, "value as string"},
	"a missing value":     {`function present<T>(data: T | undefined): T { return data as T; }`, "data as T"},
	"a discriminated one": {shapes + `const circle = shape as Circle;`, "shape as Circle"},
}

// TestNoUncheckedCastReportsUnionDowncastsWithoutRuntimeChecks: under tsc a checked downcast is checked by
// nothing, so runtimeChecksEmitted false reports it, naming the type guard to write instead (#mdccedj).
func TestNoUncheckedCastReportsUnionDowncastsWithoutRuntimeChecks(test *testing.T) {
	test.Parallel()
	for name, fixture := range unionDowncasts {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			result := runAdamicWithOptions(test, NoUncheckedCast, fixture.source, noRuntimeChecks)
			rule_testing.ExpectFindings(test, result, "uncheckedDowncast")
			expectSpans(test, fixture.source, result, fixture.span)
		})
	}
}

// TestNoUncheckedCastPassesUnionDowncastsByDefault: the same casts, unconfigured and with the default
// written out, are Adamic's checked downcasts and stay clean, as before the option.
func TestNoUncheckedCastPassesUnionDowncastsByDefault(test *testing.T) {
	test.Parallel()
	for name, fixture := range unionDowncasts {
		test.Run(name+", unconfigured", func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test, runAdamic(test, NoUncheckedCast, fixture.source))
		})
		test.Run(name+", configured", func(test *testing.T) {
			test.Parallel()
			result := runAdamicWithOptions(test, NoUncheckedCast, fixture.source, DefaultNoUncheckedCastOptions())
			rule_testing.ExpectClean(test, result)
		})
	}
}

// TestNoUncheckedCastPassesWideningsWithoutRuntimeChecks: what claims nothing tsc did not prove needs no
// runtime check, so the strict option leaves it clean.
func TestNoUncheckedCastPassesWideningsWithoutRuntimeChecks(test *testing.T) {
	test.Parallel()
	for name, source := range map[string]string{
		"an upcast":                      shapes + `const wide = shape as Shape | undefined;`,
		"a cast a guard already made":    `declare const value: string | true; if (typeof value === 'string') { const text = value as string; }`,
		"a fresh literal dropping a key": requests + `const request = { ...init, duplex: 'half' } as Init;`,
		"as const":                       `const pair = [1, 2] as const;`,
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test, runAdamicWithOptions(test, NoUncheckedCast, source, noRuntimeChecks))
		})
	}
}

// TestNoUncheckedCastOptionsDecode: a bare severity keeps Adamic's meaning, the option reads as written, and
// a misspelled key is refused rather than silently meaning the default.
func TestNoUncheckedCastOptionsDecode(test *testing.T) {
	test.Parallel()
	bare, bareError := DecodeNoUncheckedCastOptions(nil)
	if bareError != nil || !bare.RuntimeChecksEmitted {
		test.Errorf("a bare severity decoded to %+v, %v, want runtimeChecksEmitted true", bare, bareError)
	}
	strict, strictError := DecodeNoUncheckedCastOptions([]byte(`{"runtimeChecksEmitted": false}`))
	if strictError != nil || strict.RuntimeChecksEmitted {
		test.Errorf("runtimeChecksEmitted false decoded to %+v, %v", strict, strictError)
	}
	if _, misspelledError := DecodeNoUncheckedCastOptions([]byte(`{"runtimeChecks": false}`)); misspelledError == nil {
		test.Error("an unknown key decoded without an error")
	}
}
