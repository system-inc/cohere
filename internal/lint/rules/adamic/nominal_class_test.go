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
