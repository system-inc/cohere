package adamic

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// shelters is probe h01's two classes: the same shape, and a method that takes only a Dog where the other
// takes any Animal.
const shelters = animals + `
class AnimalShelter { admit(animal: Animal): string { return animal.name; } }
class DogShelter { admit(dog: Dog): string { return dog.bark(); } }
class Box<Item> {
	private item: Item;
	constructor(item: Item) { this.item = item; }
	put(item: Item): void { this.item = item; }
	get(): Item { return this.item; }
}
`

// heritage is shelters with Box's subclasses (#ncheh9w): one fixing its argument, one passing its own through,
// one fixing that one's, and one passing an array of its own. heritage-probes.tgz on the task runs the first two
// in Node: seen as a Box<Animal>, a Cat put in, and the dog's `bark` is not a function.
const heritage = shelters + `
class DogBox extends Box<Dog> {}
class Crate<Contents> extends Box<Contents> {}
class DogCrate extends Crate<Dog> {}
class Pen<Member> extends Box<Member[]> {}
`

// TestNominalClassFiresOnEveryProvenHole: each case is accepted by tsc 6.0.3 and fails on Node.
func TestNominalClassFiresOnEveryProvenHole(t *testing.T) {
	t.Parallel()
	for name, fixture := range map[string]struct {
		source string
		id     string
		span   string
	}{
		// h01
		"another class": {shelters + `const shelter: AnimalShelter = new DogShelter();`, "notAnInstance", "new DogShelter()"},
		// h18
		"a plain object": {shelters + `const shelter: AnimalShelter = { admit: (dog: Dog): string => dog.bark() };`, "notAnInstance", "{ admit: (dog: Dog): string => dog.bark() }"},
		// adamic's generic.ts
		"different type arguments":      {shelters + `const dogBox = new Box<Dog>(rex); const animalBox: Box<Animal> = dogBox;`, "typeArgumentsDiffer", "dogBox"},
		"an argument":                   {shelters + `function house(shelter: AnimalShelter): void { shelter.admit({ name: 'Tom' }); } house(new DogShelter());`, "notAnInstance", "new DogShelter()"},
		"a class in a property":         {shelters + `const dogs = { shelter: new DogShelter() }; const all: { readonly shelter: AnimalShelter } = dogs;`, "notAnInstance", "dogs"},
		"an empty class takes anything": {`class Token {} const token: Token = 42;`, "notAnInstance", "42"},
		// Strict on `any` too: a Box<any> seen as a Box<Dog> is the same hole the any opened, reported where it is used.
		"an any type argument": {shelters + `declare const loose: Box<any>; const strict: Box<Dog> = loose;`, "typeArgumentsDiffer", "loose"},
		// heritage-probes.tgz: a subclass gives Box its arguments, and those are what must be identical (#ncheh9w).
		"a subclass fixing another argument":          {heritage + `const animalBox: Box<Animal> = new DogBox(rex);`, "typeArgumentsDiffer", "new DogBox(rex)"},
		"a generic subclass passing another argument": {heritage + `const animalBox: Box<Animal> = new Crate<Dog>(rex);`, "typeArgumentsDiffer", "new Crate<Dog>(rex)"},
		"two levels of subclass":                      {heritage + `const animalBox: Box<Animal> = new DogCrate(rex);`, "typeArgumentsDiffer", "new DogCrate(rex)"},
		"an argument mapped into another type":        {heritage + `const animalBox: Box<Animal[]> = new Pen<Dog>([rex]);`, "typeArgumentsDiffer", "new Pen<Dog>([rex])"},
		// #sse0s6s shapes 2 and 3, Adamic's type_rules_weak_* and type_rules_union_* rows (codex/shared-ssa-unit-a
		// 66ad9bb): a structural value seen as a class through Adamic's Weak, an intersection with its brand, or
		// through a union of classes. Probes p2_* and p3_* on the task: tsc 6.0.3 accepts, and Node, past an
		// instanceof the value fails, throws.
		"a literal as Weak (weak_min)": {`import type { Weak } from 'adamic';
class A { value(): number { return 1; } }
const weak: Weak<A> = { value: () => 2 };
console.log('accepted');`, "notAnInstance", "{ value: () => 2 }"},
		"an object as Weak (weak_field)": {`import type { Weak } from 'adamic';
class A { value: number = 1; }
const object: { value: number } = { value: 2 };
const weak: Weak<A> = object;
object.value = 3;
if (weak !== undefined) { console.log(` + "`${weak.value}`" + `); }`, "notAnInstance", "object"},
		"an object with a callback as Weak (weak_method)": {`import type { Weak } from 'adamic';
class A { value: number = 1; read(): number { return this.value; } }
const object: { value: number; read: () => number } = { value: 2, read: () => 9 };
const weak: Weak<A> = object;
object.value = 3;
function read(value: Weak<A>): number { return value === undefined ? -1 : value.read(); }
console.log(` + "`${read(weak)}`" + `);`, "notAnInstance", "object"},
		"an object as a Weak property (weak_callback)": {`import type { Weak } from 'adamic';
class A { value: number = 1; read(): number { return this.value; } }
const object: { value: number; read: () => number } = { value: 2, read: () => 9 };
const holder: { value: Weak<A> } = { value: object };
object.read = () => 8;
function read(value: Weak<A>): number { return value === undefined ? -1 : value.read(); }
console.log(` + "`${read(holder.value)}`" + `);`, "notAnInstance", "object"},
		"a literal as A or B (union_min)": {`class A { value(): number { return 1; } }
class B { value(): number { return 2; } }
const value: A | B = { value: () => 3 };
console.log('accepted');`, "notAnInstance", "{ value: () => 3 }"},
		"a literal with a field as A or B (union_field)": {`class A { value: number = 1; }
class B { value: number = 2; }
const view: A | B = { value: 3 };
const object: { value: number } = view;
object.value = 4;
console.log(` + "`${view.value}`" + `);`, "notAnInstance", "{ value: 3 }"},
		"a literal with a callback as A or B (union_method)": {`class A { value: number = 1; read(): number { return this.value; } }
class B { value: number = 2; read(): number { return this.value; } }
const view: A | B = { value: 3, read: () => 9 };
const object: { value: number; read: () => number } = view;
object.value = 4;
function read(value: A | B): number {
 if (value instanceof A) { return value.read(); }
 return value.read();
}
console.log(` + "`${read(view)}`" + `);`, "notAnInstance", "{ value: 3, read: () => 9 }"},
		"a literal replaced through an alias as A or B (union_callback)": {`class A { read(): number { return 1; } }
class B { read(): number { return 2; } }
const view: A | B = { read: () => 3 };
const object: { read: () => number } = view;
object.read = () => 4;
function read(value: A | B): number {
 if (value instanceof A) { return value.read(); }
 return value.read();
}
console.log(` + "`${read(view)}`" + `);`, "notAnInstance", "{ read: () => 3 }"},
		// A literal-typed field: the union is the literal's contextual type, so `kind` stays 'a', widened or not.
		"a literal with a literal-typed field as A or B": {`class A { readonly kind: 'a' = 'a'; }
class B { readonly kind: 'b' = 'b'; }
const value: A | B = { kind: 'a' };`, "notAnInstance", "{ kind: 'a' }"},
		"a literal as a class or undefined": {`class A { value(): number { return 1; } }
const maybe: A | undefined = { value: () => 2 };`, "notAnInstance", "{ value: () => 2 }"},
		// api's drizzle adapters: an intersection whose member is the subclass says the arguments differ.
		"a subclass in an intersection": {heritage + `declare const tagged: DogBox & { readonly tag: string }; const animalBox: Box<Animal> = tagged;`, "typeArgumentsDiffer", "tagged"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result := runAdamic(t, NominalClass, fixture.source)
			rule_testing.ExpectFindings(t, result, fixture.id)
			expectSpans(t, fixture.source, result, fixture.span)
		})
	}
}

// TestNominalClassStaysCleanOnInstancesAndInterfaces: the class's own instances, its subclasses, and an
// instance passed as an interface it satisfies.
func TestNominalClassStaysCleanOnInstancesAndInterfaces(t *testing.T) {
	t.Parallel()
	for name, source := range map[string]string{
		"its own instance":       shelters + `const shelter: AnimalShelter = new AnimalShelter();`,
		"the same type argument": shelters + `const dogBox = new Box<Dog>(rex); const same: Box<Dog> = dogBox;`,
		// h02's shape, with the interface tsc checks strictly
		"an interface it satisfies": shelters + `interface Shelter { readonly admit: (animal: Animal) => string } const shelter: Shelter = new AnimalShelter();`,
		"a subclass":                `class Base { readonly id = 1; } class Derived extends Base { readonly extra = 2; } const base: Base = new Derived();`,
		"this":                      `class Chain { next(): Chain { return this; } }`,
		"a union of instances":      shelters + `declare const either: AnimalShelter | undefined; const maybe: AnimalShelter | undefined = either;`,
		// The heritage near-misses (#ncheh9w): the argument the subclass gives, and a read-only view, not a class.
		"a subclass fixing the same argument":          heritage + `const dogs: Box<Dog> = new DogBox(rex);`,
		"a generic subclass passing the same argument": heritage + `const dogs: Box<Dog> = new Crate<Dog>(rex);`,
		"two levels with the same argument":            heritage + `const dogs: Box<Dog> = new DogCrate(rex); const crate: Crate<Dog> = new DogCrate(rex);`,
		"an argument mapped the same way":              heritage + `const dogs: Box<Dog[]> = new Pen<Dog>([rex]);`,
		"a read-only view of a subclass":               heritage + `const view: { readonly get: () => Animal } = new DogBox(rex);`,
		// #sse0s6s's near-misses: an instance and undefined as Weak, an instance into a union of classes, a literal
		// a union's interface member takes, and a primitive member.
		"an instance as Weak":                 `import type { Weak } from 'adamic'; class A { value(): number { return 1; } } const weak: Weak<A> = new A();`,
		"undefined as Weak":                   `import type { Weak } from 'adamic'; class A { value(): number { return 1; } } const weak: Weak<A> = undefined;`,
		"an instance into a union of classes": `class A { value(): number { return 1; } } class B { value(): number { return 2; } } const either: A | B = new B();`,
		"a literal an interface member takes": `class A { value(): number { return 1; } } interface Shape { value(): number } const either: A | Shape = { value: () => 3 };`,
		"a primitive member":                  `class A { value(): number { return 1; } } const either: A | string = 'text';`,
		// @system_cohere_lint's question on #sse0s6s: a literal both a class member and an interface member take, read
		// through a property check. Probe p4 on the task: tsc 6.0.3 accepts it and Node reads the literal's own members,
		// so nothing fails on Node, and a union passes when one member does, as relate's rule has it. Adamic's stage 0
		// refuses the literal by its own nominal view; whether a property check narrowing a structural value to a
		// class is a hole is native dispatch's question, filed against relate's union rule, not widened here.
		"a literal a class and an interface both take": `class A { readonly tag: string = 'a'; bark(): string { return 'woof'; } } interface Named { bark(): string } const either: A | Named = { tag: 'literal', bark: () => 'literal' };`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rule_testing.ExpectClean(t, runAdamic(t, NominalClass, source))
		})
	}
}

// TestNominalClassStaysCleanOnAdamicsPrograms: Adamic 0.1 compiles all ten, the generic Stack among them.
func TestNominalClassStaysCleanOnAdamicsPrograms(t *testing.T) {
	t.Parallel()
	expectProgramsClean(t, NominalClass)
}
