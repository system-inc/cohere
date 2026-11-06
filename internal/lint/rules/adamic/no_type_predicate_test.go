package adamic

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// TestNoTypePredicateFiresOnEveryWrittenPredicate: adamic's refusal 4, and each place a predicate can be
// written.
func TestNoTypePredicateFiresOnEveryWrittenPredicate(test *testing.T) {
	test.Parallel()
	const pets = "type Pet = { readonly kind: 'Cat'; readonly meow: () => string } | { readonly kind: 'Dog'; readonly bark: () => string };\n"
	for name, fixture := range map[string]struct {
		source string
		span   string
	}{
		// adamic's refusal 4, tsc 6.0.3 accepts it and Node fails
		"a lying guard": {
			pets + `function isCat(pet: Pet): pet is Extract<Pet, { kind: 'Cat' }> { return pet.kind === 'Dog'; }`,
			"pet is Extract<Pet, { kind: 'Cat' }>",
		},
		"an assertion": {
			pets +
				`function assertCat(pet: Pet): asserts pet is Extract<Pet, { kind: 'Cat' }> { if (pet.kind !== 'Cat') { throw new Error('dog'); } }`,
			"asserts pet is Extract<Pet, { kind: 'Cat' }>",
		},
		"a bare assertion": {
			`function assertPresent(value: unknown): asserts value { if (value === undefined) { throw new Error('missing'); } }`,
			"asserts value",
		},
		"a this predicate": {
			`class Tree { readonly leaf = true; isLeaf(): this is { readonly leaf: true } { return this.leaf; } }`,
			"this is { readonly leaf: true }",
		},
		"a declared function": {
			pets + `declare function isDog(pet: Pet): pet is Extract<Pet, { kind: 'Dog' }>;`,
			"pet is Extract<Pet, { kind: 'Dog' }>",
		},
		"a function type": {
			pets + `type Guard = (pet: Pet) => pet is Extract<Pet, { kind: 'Dog' }>;`,
			"pet is Extract<Pet, { kind: 'Dog' }>",
		},
		"an arrow": {
			pets + `const isDog = (pet: Pet): pet is Extract<Pet, { kind: 'Dog' }> => pet.kind === 'Dog';`,
			"pet is Extract<Pet, { kind: 'Dog' }>",
		},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			result := runAdamic(test, NoTypePredicate, fixture.source)
			rule_testing.ExpectFindings(test, result, "typePredicate")
			expectSpans(test, fixture.source, result, fixture.span)
		})
	}
}

// TestNoTypePredicateStaysCleanOnInferredPredicates: a predicate the body proves is never written.
func TestNoTypePredicateStaysCleanOnInferredPredicates(test *testing.T) {
	test.Parallel()
	for name, source := range map[string]string{
		"an inferred predicate": `declare const values: readonly (number | undefined)[]; const present = values.filter((value) => value !== undefined);`,
		"a boolean function":    `function isEven(value: number): boolean { return value % 2 === 0; }`,
		"narrowing in place":    `type Pet = { readonly kind: 'Cat' } | { readonly kind: 'Dog' }; declare const pet: Pet; if (pet.kind === 'Cat') { pet.kind; }`,
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test, runAdamic(test, NoTypePredicate, source))
		})
	}
}

// TestNoTypePredicateStaysCleanOnAdamicsPrograms: Adamic 0.1 compiles all ten.
func TestNoTypePredicateStaysCleanOnAdamicsPrograms(test *testing.T) {
	test.Parallel()
	expectProgramsClean(test, NoTypePredicate)
}
