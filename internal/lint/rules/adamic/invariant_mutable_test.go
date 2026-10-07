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
		"shared elements in a new array": {animals + `
const kennels: { pet: Dog }[] = [{ pet: rex }];
const pens: { pet: Animal }[] = kennels.map((kennel) => kennel);`, "kennels.map((kennel) => kennel)"},
		// t4 on #drbrp8c: a function can hand back what it holds, so its result is walked like any value.
		"a function's shared result": {animals + `
const kennel: Dog[] = [rex];
const dogsOf = (): Dog[] => kennel;
const animalsOf: () => Animal[] = dogsOf;`, "dogsOf"},
		"a mutable callback slot": {animals + `
const handler: { onAnimal: (animal: Dog) => void } = { onAnimal: (dog) => { dog.bark(); } };
const wide: { onAnimal: (animal: never) => void } = handler;`, "handler"},
		// #53w68gt: a type parameter target is written through its constraint, and pack.push stores a
		// Pack[number] where narrow holds only Narrow[number].
		"a type parameter target": {animals + `
function admit<Pack extends Animal[], Narrow extends Pack>(narrow: Narrow, extra: Pack[number]): void {
	const pack: Pack = narrow;
	pack.push(extra);
}`, "narrow"},
		"a type parameter target behind a readonly property": {animals + `
function admit<Pack extends { readonly all: Animal[] }, Narrow extends Pack>(narrow: Narrow): Pack {
	return narrow;
}`, "narrow"},
		// #53w68gt: an intersection's array member is a slot like any array.
		"an intersection target": {animals + `
declare const tagged: Dog[] & { tag: string };
const wide: Animal[] & { tag: string } = tagged;`, "tagged"},
		"an intersection source": {animals + `
declare const tagged: Dog[] & { tag: string };
const all: Animal[] = tagged;`, "tagged"},
		// #b9a0wgy: an intersection of objects, as either side; pen.pet = cat reaches kennel.
		"an object intersection target": {animals + `
interface Named { readonly label: string }
const kennel: { pet: Dog } & Named = { pet: rex, label: 'kennel' };
const pen: { pet: Animal } & Named = kennel;`, "kennel"},
		"an object intersection source": {animals + `
interface Named { readonly label: string }
const kennel: { pet: Dog } & Named = { pet: rex, label: 'kennel' };
const pen: { pet: Animal } = kennel;`, "kennel"},
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
		// t1 on #drbrp8c: a union slot is judged whole. Judged member by member, its `null` read as a slot a
		// handler could be written into; every element seen as an Element reported onfullscreenchange that way.
		"a union slot": `interface Handler { readonly run: (label: string) => string } interface SameHandler { readonly run: (label: string) => string } declare const source: { onEvent: Handler | null }; const wide: { onEvent: SameHandler | null } = source;`,
		// t1b on #drbrp8c: what the wider slot can hold is assignable back, so writing it is safe. The two slot
		// types differ (one is readonly), so identity would report it. The readonly is the target's: the source's
		// `readonly name` made writable is shape 1 of #gvzdft9, a hole of its own.
		"a slot assignable back": `declare const source: { pet: { name: string } }; const wide: { pet: { readonly name: string } } = source;`,
		"an any element":         `declare const loose: any[]; const strict: string[] = loose;`,
		"a new array from map":   animals + `const dogs: Dog[] = [rex]; const all: Animal[] = dogs.map((dog) => dog);`,
		"a new map":              animals + `const all: Map<string, Animal> = new Map<string, Dog>([['Rex', rex]]);`,
		"Object.values":          animals + `declare const byName: { readonly [name: string]: Dog }; const all: Animal[] = Object.values(byName);`,
		"a conditional literal":  `declare const flag: boolean; const maybe: { kind: string } | undefined = flag ? { kind: 'Circle' } : undefined;`,
		// #53w68gt's near-misses: a constraint with no writable slot, the same parameter, an unconstrained one,
		// and an intersection whose array member is the same and whose other member is read-only.
		"a type parameter with a readonly constraint":      animals + `function admit<Pack extends readonly Animal[], Narrow extends Pack>(narrow: Narrow): Pack { const pack: Pack = narrow; return pack; }`,
		"a type parameter constrained to a readonly shape": animals + `function admit<Pack extends Animal, Narrow extends Pack>(narrow: Narrow): Pack { return narrow; }`,
		"an unconstrained type parameter":                  `function hold<Item, Other extends Item>(other: Other): Item { return other; }`,
		// #53w68gt's measured false findings, each a pair that is no flow into the parameter.
		"a generic method seen under two receivers": `class Entity { name = ''; clone<T extends this>(): T { return this as T; } } class Session extends Entity { token = ''; } declare const session: Readonly<Session & object>; const wide: Readonly<Session> = session;`,
		"a union member the parameter is not":       `class Tracked { name = ''; } function insert<Entity extends Tracked>(entity: Readonly<Entity> | readonly Entity[]): readonly Entity[] { const entities: readonly Entity[] = Array.isArray(entity) ? entity : [entity]; return entities; }`,
		"a parameter narrowed to non-null":          `function keep<Value extends { items: string[] } | null>(value: Value, set: (next: Value) => void): void { if(value !== null) { set(value); } }`,
		// #b9a0wgy's near-misses: a readonly member, and a brand, whose object member is a phantom.
		"an object intersection with a readonly member": animals + `interface Named { readonly label: string } const kennel: { pet: Dog } & Named = { pet: rex, label: 'kennel' }; const pen: { readonly pet: Animal } & Named = kennel;`,
		"a class constructor's prototype":               `class Form { readonly kind = 'Form'; } type ClassType = (new () => object) & { prototype: object }; const resolve: () => typeof Form = () => Form; const wide: () => ClassType = resolve;`,
		"a branded primitive":                           `declare const brand: unique symbol; type Slot = string & { readonly [brand]?: () => 'value' }; const text: Slot = 'Hello'; const wide: string & { readonly [brand]?: () => string } = text;`,
		"an intersection with the same array":           animals + `declare const tagged: Animal[] & { readonly tag: 'kennel' }; const wide: Animal[] & { readonly tag: string } = tagged;`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rule_testing.ExpectClean(t, runAdamic(t, InvariantMutable, source))
		})
	}
}

// TestInvariantMutableFiresWhereAReadOnlySlotIsMadeWritable: shape 1 of #gvzdft9, six-probes.tgz's s1. A
// read-only property is covariant, so it may hold a Dog behind `Animal`, and a writable view of it lets a Cat in;
// tsc 6.0.3 accepts it, and Node throws `bark is not a function`.
func TestInvariantMutableFiresWhereAReadOnlySlotIsMadeWritable(t *testing.T) {
	t.Parallel()
	for name, fixture := range map[string]struct {
		source string
		span   string
	}{
		"the same type, read-only then writable": {animals + `
const kennel: { readonly pet: Dog } = { pet: rex };
const view: { readonly pet: Animal } = kennel;
const pen: { pet: Animal } = view;`, "view"},
		"one level down": {animals + `
declare const yard: { readonly kennel: { readonly pet: Animal } };
const open: { readonly kennel: { pet: Animal } } = yard;`, "yard"},
		// six-probes.tgz's s1b: a read-only string may hold a literal, so `sounds[dog.kind]` stops being a function.
		"a primitive, under a slot assignable back": {`declare const source: { pet: { readonly name: string } }; const wide: { pet: { name: string } } = source;`, "source"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result := runAdamic(t, InvariantMutable, fixture.source)
			rule_testing.ExpectFindings(t, result, "readonlyMadeWritable")
			expectSpans(t, fixture.source, result, fixture.span)
		})
	}
}

// TestInvariantMutableStaysCleanWhereAReadOnlySlotStaysReadOnly: the near-misses of shape 1.
func TestInvariantMutableStaysCleanWhereAReadOnlySlotStaysReadOnly(t *testing.T) {
	t.Parallel()
	for name, source := range map[string]string{
		"read-only both sides":        animals + `declare const kennel: { readonly pet: Animal }; const view: { readonly pet: Animal } = kennel;`,
		"a literal type":              `declare const circle: { readonly kind: 'Circle' }; const shape: { kind: 'Circle' } = circle;`,
		"null":                        `declare const empty: { readonly next: null }; const node: { next: null } = empty;`,
		"a writable source":           animals + `declare const kennel: { pet: Animal }; const pen: { pet: Animal } = kennel;`,
		"a fresh literal of the view": animals + `const pen: { pet: Animal } = { pet: rex };`,
		// s1c: a library getter throws on the write before anything is stored.
		"a library getter": `const pattern = /dog/; const view: { source: string } = pattern;`,
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
