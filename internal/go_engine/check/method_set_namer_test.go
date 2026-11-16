package check

import (
	"go/types"
	"slices"
	"testing"
)

// methodSetNamesFixture is one package of a method set rule's own, checked in memory, and the sets it asks of it.
type methodSetNamesFixture struct {
	name         string
	source       string
	dependencies map[string]string
	sets         []methodSetNamesQuery
}

// methodSetNamesQuery is one set a fixture asks: the type of the package-scope object named object, a type name's
// or a variable's, or a pointer to it, and the names its method set holds, sorted.
type methodSetNamesQuery struct {
	object  string
	pointer bool
	names   []string
}

// methodSetNamesFixtures are a fixture for each rule of NewMethodSet's walk the namer mirrors (methodSetNamer). Each
// set's names are written out, so a rule both walks drop alike still fails, and the test holds them to NewMethodSet's
// too. An embedded type parameter has no fixture: Go allows no type parameter as an embedded field, so a walk from a
// type name never reaches one, and a type parameter's constraint is reached only as an interface, which the embedded
// interface fixture covers.
var methodSetNamesFixtures = []methodSetNamesFixture{
	{
		// Near's Shadowed, at depth 1, settles the name before Left's and Right's cancel each other at depth 2.
		name: "a method shadows deeper ones",
		source: `package fixture

type Left struct{}

func (Left) Shadowed() {}

type Right struct{}

func (Right) Shadowed() {}

type Pair struct {
	Left
	Right
}

type Near struct{}

func (Near) Shadowed() {}

type Outer struct {
	Near
	Pair
}
`,
		sets: []methodSetNamesQuery{
			{object: "Outer", names: []string{"Shadowed"}},
			{object: "Outer", pointer: true, names: []string{"Shadowed"}},
		},
	},
	{
		// Two embedded types promote Shadowed at one depth, and one type embedded twice at one depth promotes its
		// methods twice: each name cancels, and a name only one of them has is kept.
		name: "two of a name at one depth cancel",
		source: `package fixture

type Left struct{}

func (Left) Shadowed() {}
func (Left) LeftOnly() {}

type Right struct{}

func (Right) Shadowed() {}

type Pair struct {
	Left
	Right
}

type Shared struct{}

func (Shared) Twice() {}

type First struct{ Shared }

type Second struct{ Shared }

type Diamond struct {
	First
	Second
}

type Deeper struct {
	Pair
	Diamond
}

func (Deeper) Shadowed() {}
`,
		sets: []methodSetNamesQuery{
			{object: "Pair", names: []string{"LeftOnly"}},
			{object: "Pair", pointer: true, names: []string{"LeftOnly"}},
			{object: "Diamond", names: nil},
			{object: "Deeper", names: []string{"LeftOnly", "Shadowed"}},
		},
	},
	{
		// A field hides a deeper method of its name, and collides with a method of its name at its own depth.
		name: "a field collides with a method",
		source: `package fixture

type Inner struct{}

func (Inner) Name() {}
func (Inner) Kept() {}

type Outer struct {
	Inner
	Name string
}

type Holder struct{ Name int }

type Beside struct {
	Holder
	Inner
}
`,
		sets: []methodSetNamesQuery{
			{object: "Outer", names: []string{"Kept"}},
			{object: "Outer", pointer: true, names: []string{"Kept"}},
			{object: "Beside", names: []string{"Kept"}},
			{object: "Beside", pointer: true, names: []string{"Kept"}},
		},
	},
	{
		// An interface's set holds its embedded interfaces' methods, and a struct embedding an interface is lent
		// them, by value too.
		name: "an embedded interface brings its methods",
		source: `package fixture

type Reader interface{ Read() }

type Closer interface{ Close() }

type ReadCloser interface {
	Reader
	Closer
	Flush()
}

type Holder struct{ ReadCloser }
`,
		sets: []methodSetNamesQuery{
			{object: "ReadCloser", names: []string{"Close", "Flush", "Read"}},
			{object: "ReadCloser", pointer: true, names: nil},
			{object: "Holder", names: []string{"Close", "Flush", "Read"}},
			{object: "Holder", pointer: true, names: []string{"Close", "Flush", "Read"}},
		},
	},
	{
		// An embedded pointer lends a value its base's pointer methods, and a cycle through embedded pointers ends at
		// the named type seen at a shallower depth.
		name: "an embedded pointer, and a cycle through one",
		source: `package fixture

type Base struct{}

func (*Base) Grow() {}
func (Base) Look() {}

type Wrapper struct{ *Base }

type Node struct {
	*Node
	Value int
}

func (*Node) Next() {}

type Ring struct{ *Link }

type Link struct{ *Ring }

func (*Ring) Turn() {}

func (Link) Hold() {}
`,
		sets: []methodSetNamesQuery{
			{object: "Wrapper", names: []string{"Grow", "Look"}},
			{object: "Wrapper", pointer: true, names: []string{"Grow", "Look"}},
			{object: "Node", names: nil},
			{object: "Node", pointer: true, names: []string{"Next"}},
			{object: "Ring", names: []string{"Hold"}},
			{object: "Ring", pointer: true, names: []string{"Hold", "Turn"}},
		},
	},
	{
		// An instantiated generic type has its origin's methods, by their receivers, alone or embedded, and so does a
		// generic type embedding one instantiated by its own type parameter, itself instantiated.
		name: "a generic receiver",
		source: `package fixture

type Box[T any] struct{ value T }

func (box Box[T]) Get() T { return box.value }
func (box *Box[T]) Set(value T) { box.value = value }

type Holder struct{ Box[int] }

type Getter interface{ Get() string }

type Wrap[T Getter] struct {
	Box[T]
	inner T
}

var instance Box[string]

var wrapped Wrap[Box[string]]
`,
		sets: []methodSetNamesQuery{
			{object: "Box", names: []string{"Get"}},
			{object: "Box", pointer: true, names: []string{"Get", "Set"}},
			{object: "instance", names: []string{"Get"}},
			{object: "instance", pointer: true, names: []string{"Get", "Set"}},
			{object: "Holder", names: []string{"Get"}},
			{object: "Holder", pointer: true, names: []string{"Get", "Set"}},
			{object: "wrapped", names: []string{"Get"}},
			{object: "wrapped", pointer: true, names: []string{"Get", "Set"}},
		},
	},
	{
		// A value's set lacks a pointer receiver's method and the pointer's has it; a named pointer type has none.
		name: "value against pointer receivers",
		source: `package fixture

type Counter struct{}

func (Counter) Read() {}
func (*Counter) Increment() {}

type Pointing *Counter
`,
		sets: []methodSetNamesQuery{
			{object: "Counter", names: []string{"Read"}},
			{object: "Counter", pointer: true, names: []string{"Increment", "Read"}},
			{object: "Pointing", names: nil},
		},
	},
	{
		// Two packages' unexported methods of one spelling are two methods, so the name is in the set twice.
		name: "unexported names of two packages",
		source: `package fixture

import "example.com/other"

type Local struct{}

func (Local) name() {}

type Both struct {
	other.Hidden
	Local
}
`,
		dependencies: map[string]string{"example.com/other": `package other

type Hidden struct{}

func (Hidden) name() {}
`},
		sets: []methodSetNamesQuery{
			{object: "Both", names: []string{"name", "name"}},
			{object: "Both", pointer: true, names: []string{"name", "name"}},
		},
	},
}

// TestMethodSetNamesFixtures holds the namer, on each rule's fixture, to the names written out and to NewMethodSet's.
func TestMethodSetNamesFixtures(test *testing.T) {
	test.Parallel()
	for _, fixture := range methodSetNamesFixtures {
		test.Run(fixture.name, func(test *testing.T) {
			test.Parallel()
			checked := checkedInMemory(test, "example.com/fixture", fixture.source, fixture.dependencies)
			namer := newMethodSetNamer()
			for _, query := range fixture.sets {
				object := checked.Scope().Lookup(query.object)
				if object == nil {
					test.Fatalf("the fixture declares no %s", query.object)
				}
				set := object.Type()
				if query.pointer {
					set = types.NewPointer(set)
				}
				got := slices.Sorted(slices.Values(namer.methodNames(set)))
				if !slices.Equal(got, query.names) {
					test.Errorf("%s: the namer gives %v, want %v", set, got, query.names)
				}
				if fromMethodSet := methodSetNamesOf(set); !slices.Equal(fromMethodSet, query.names) {
					test.Errorf("%s: NewMethodSet gives %v, the fixture wants %v", set, fromMethodSet, query.names)
				}
			}
		})
	}
}

// methodSetNamesOf are the names of NewMethodSet(set), sorted, what the namer must give.
func methodSetNamesOf(set types.Type) []string {
	methods := types.NewMethodSet(set)
	var names []string
	for index := range methods.Len() {
		names = append(names, methods.At(index).Obj().Name())
	}
	slices.Sort(names)
	return names
}
