package adamic

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// animals is the probes' shared vocabulary: a Dog is an Animal that barks, and a cat written through an
// Animal-typed name is what makes each hole a TypeError.
const animals = `
interface Animal { readonly name: string }
interface Dog extends Animal { readonly bark: () => string }
declare const rex: Dog;
`

// TestInvariantMutableFiresOnEveryProvenHole: each case is a probe tsc 6.0.3 accepts and Node fails on,
// and the finding sits on the value being widened.
func TestInvariantMutableFiresOnEveryProvenHole(t *testing.T) {
	t.Parallel()
	for name, fixture := range map[string]struct {
		source string
		span   string
	}{
		// h03
		"a mutable property": {animals + `
const kennel: { pet: Dog } = { pet: rex };
const pen: { pet: Animal } = kennel;
pen.pet = { name: 'Tom' };`, "kennel"},
		// h04
		"a map's value": {animals + `
const dogs = new Map<string, Dog>();
const others: Map<string, Animal> = dogs;`, "dogs"},
		// h13
		"an argument": {animals + `
function adopt(all: Animal[]): void { all.push({ name: 'Tom' }); }
const dogs: Dog[] = [rex];
adopt(dogs);`, "dogs"},
		// h19
		"a returned type parameter array": {animals + `
function widen<Item extends Animal>(items: Item[]): Animal[] { return items; }`, "items"},
		// h20
		"an upcast": {animals + `
const dogs: Dog[] = [rex];
(dogs as Animal[]).push({ name: 'Tom' });`, "dogs"},
		"an assignment": {animals + `
const dogs: Dog[] = [rex];
let all: Animal[] = [];
all = dogs;`, "dogs"},
		"a mutable array under a readonly property": {animals + `
const kennel: { readonly dogs: Dog[] } = { dogs: [rex] };
const pen: { readonly dogs: Animal[] } = kennel;`, "kennel"},
		"an array inside an object literal": {animals + `
const dogs: Dog[] = [rex];
const pen: { readonly all: Animal[] } = { all: dogs };`, "dogs"},
		"a literal type in a mutable slot": {`
const circle: { kind: 'Circle' } = { kind: 'Circle' };
const shape: { kind: string } = circle;`, "circle"},
		"a union slot that may be missing": {animals + `
const dogs: Dog[] = [rex];
const maybe: Animal[] | undefined = dogs;`, "dogs"},
		"a mutable callback slot": {animals + `
const handler: { onAnimal: (animal: Dog) => void } = { onAnimal: (dog) => { dog.bark(); } };
const wide: { onAnimal: (animal: never) => void } = handler;`, "handler"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result := runAdamic(t, InvariantMutable, fixture.source)
			rule_testing.ExpectFindings(t, result, "mutableWidening")
			expectSpans(t, fixture.source, result, fixture.span)
		})
	}
}

// TestInvariantMutableStaysCleanWhereNothingCanBeWrittenThrough: read-only targets, fresh literals, the
// same type, and a class instance (nominal-class's) are not this rule's holes.
func TestInvariantMutableStaysCleanWhereNothingCanBeWrittenThrough(t *testing.T) {
	t.Parallel()
	for name, source := range map[string]string{
		// h14
		"a readonly array": animals + `const dogs: Dog[] = [rex]; const all: readonly Animal[] = dogs;`,
		// h11
		"a fresh literal":       animals + `const pen: { pet: Animal } = { pet: rex };`,
		"a fresh array literal": animals + `const all: Animal[] = [rex, rex];`,
		"a spread copy":         animals + `const dogs: Dog[] = [rex]; const all: Animal[] = [...dogs];`,
		"a readonly property":   animals + `const kennel: { pet: Dog } = { pet: rex }; const pen: { readonly pet: Animal } = kennel;`,
		"a read-only map":       animals + `const dogs = new Map<string, Dog>(); const all: ReadonlyMap<string, Animal> = dogs;`,
		"the same type":         animals + `const dogs: Dog[] = [rex]; const same: Dog[] = dogs;`,
		"a class instance":      animals + `class Box<Item> { item: Item; constructor(item: Item) { this.item = item; } } const box: Box<Animal> = new Box<Dog>(rex);`,
		"a width-only widening": `const point = { x: 1, y: 2 }; const flat: { x: number } = point;`,
		"a callback argument":   animals + `const dogs: Dog[] = [rex]; dogs.forEach((dog) => dog.bark());`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rule_testing.ExpectClean(t, runAdamic(t, InvariantMutable, source))
		})
	}
}

// TestInvariantMutableStaysCleanOnAdamicsPrograms: Adamic 0.1 compiles all ten.
func TestInvariantMutableStaysCleanOnAdamicsPrograms(t *testing.T) {
	t.Parallel()
	expectProgramsClean(t, InvariantMutable)
}

// TestInvariantMutableNamesTheSlot: the message says which part can be written through and both types
// there, since `'Dog[]' is seen as 'Animal[]'` alone does not say where to put the `readonly`.
func TestInvariantMutableNamesTheSlot(t *testing.T) {
	t.Parallel()
	source := animals + `
const kennel: { readonly dogs: Dog[] } = { dogs: [rex] };
const pen: { readonly dogs: Animal[] } = kennel;`
	result := runAdamic(t, InvariantMutable, source)
	rule_testing.ExpectFindings(t, result, "mutableWidening")
	description := result.Diagnostics[0].Message.Description
	for _, want := range []string{"`kennel.dogs[]`", "'Dog'", "'Animal'"} {
		if !strings.Contains(description, want) {
			t.Errorf("the message does not name %s: %s", want, description)
		}
	}
}
