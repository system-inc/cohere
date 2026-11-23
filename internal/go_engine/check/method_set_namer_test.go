package check

import (
	"slices"
	"testing"

	"github.com/system-inc/go-types/types"
)

// methodSetNamesFixture is one package of a method set rule's own, checked in memory, and the sets it asks of it.
// holds, when set, checks what the sets prove nothing without, a fact of the checked package the rule turns on.
type methodSetNamesFixture struct {
	name         string
	source       string
	dependencies map[string]string
	sets         []methodSetNamesQuery
	holds        func(test *testing.T, checked *types.Package)
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
		// A type embedded twice at one depth carries its collision down: Shared appears twice at depth 2, so Deep,
		// which it embeds once, brings Carried at depth 3 as a collision too, and neither set holds it (#jy0v2ef).
		name: "a collision carried down from a type embedded twice",
		source: `package fixture

type Deep struct{}

func (Deep) Carried() {}

type Shared struct{ Deep }

type First struct{ Shared }

type Second struct{ Shared }

type Diamond struct {
	First
	Second
}
`,
		sets: []methodSetNamesQuery{
			{object: "Diamond", names: nil},
			{object: "Diamond", pointer: true, names: nil},
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
		// An unexported field is its package's name: Outer's name hides Inner's method of its package and spelling,
		// and Mixed's name, the fixture's, is not other's method name, which the set keeps (#jy0v2ef).
		name: "an unexported field keyed by its package",
		source: `package fixture

import "example.com/other"

type Inner struct{}

func (Inner) name() {}
func (Inner) Kept() {}

type Outer struct {
	Inner
	name string
}

type Mixed struct {
	other.Hidden
	name int
}
`,
		dependencies: map[string]string{"example.com/other": `package other

type Hidden struct{}

func (Hidden) name() {}
`},
		sets: []methodSetNamesQuery{
			{object: "Outer", names: []string{"Kept"}},
			{object: "Outer", pointer: true, names: []string{"Kept"}},
			{object: "Mixed", names: []string{"name"}},
			{object: "Mixed", pointer: true, names: []string{"name"}},
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
			// No finished type check reaches the namer's named pointer return (method_set_namer.go, methodNames' first
			// check), so this holds with it deleted: Go declares no method on a named pointer type, and its underlying
			// pointer is neither a struct nor an interface, so the walk finds nothing either way. go/types attaches a
			// method to one only in a package with type errors, which checkedInMemory refuses (#jy0v2ef).
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
	{
		// One instance reached twice at one depth by two pointers, other.G[int] instantiated by other's checker in A
		// and by the fixture's in B, is one type embedded twice, folded by types.Identical, not by ==, so M collides
		// (#jy0v2ef). wasSeen's types.Identical half has no fixture and can have none: a type identical to one walked
		// at a shallower depth brings only names that depth settled, so walking it again changes no name, and a
		// cycle's types are finitely many pointers, so == alone still ends it.
		name: "two identical instances that are not one pointer",
		source: `package fixture

import "example.com/other"

type B struct{ other.G[int] }

type Both struct {
	other.A
	B
}
`,
		dependencies: map[string]string{"example.com/other": `package other

type G[T any] struct{}

func (G[T]) M() {}

type A struct{ G[int] }
`},
		sets: []methodSetNamesQuery{
			{object: "Both", names: nil},
			{object: "Both", pointer: true, names: nil},
		},
		holds: func(test *testing.T, checked *types.Package) {
			both := methodSetNamesDeclared(test, checked, "Both")
			throughOther := methodSetNamesField(test, methodSetNamesField(test, both, 0), 0)
			throughFixture := methodSetNamesField(test, methodSetNamesField(test, both, 1), 0)
			if throughOther == throughFixture || !types.Identical(throughOther, throughFixture) {
				test.Fatalf("A's %s and B's %s are not two pointers to identical instances, so the fixture proves "+
					"nothing", throughOther, throughFixture)
			}
		},
	},
	{
		// An alias is its type: as a type name, embedded, embedded through a pointer, and of a pointer, whose set is
		// its base's pointer set (#jy0v2ef).
		name: "an alias, as a type name and embedded",
		source: `package fixture

type Inner struct{}

func (Inner) Look()  {}
func (*Inner) Grow() {}

type Renamed = Inner

type ThroughAlias struct{ Renamed }

type ThroughPointer struct{ *Renamed }

type PointerTo = *Inner
`,
		sets: []methodSetNamesQuery{
			{object: "Renamed", names: []string{"Look"}},
			{object: "Renamed", pointer: true, names: []string{"Grow", "Look"}},
			{object: "ThroughAlias", names: []string{"Look"}},
			{object: "ThroughAlias", pointer: true, names: []string{"Grow", "Look"}},
			{object: "ThroughPointer", names: []string{"Grow", "Look"}},
			{object: "PointerTo", names: []string{"Grow", "Look"}},
		},
		holds: func(test *testing.T, checked *types.Package) {
			for _, alias := range []string{"Renamed", "PointerTo"} {
				if _, isAlias := methodSetNamesDeclared(test, checked, alias).(*types.Alias); !isAlias {
					test.Fatalf("%s is no *types.Alias, so the fixture proves nothing of aliases", alias)
				}
			}
		},
	},
	{
		// A type reached after a duplicate at one depth is walked too: depth 2 is Shared, Shared, Only, so folding
		// Shared's second entry into its first moves Only into the second slot, and Only's M is kept (#2br8dpa).
		name: "a type after a duplicate at one depth",
		source: `package fixture

type Shared struct{}

func (Shared) Twice() {}

type Only struct{}

func (Only) M() {}

type First struct{ Shared }

type Second struct{ Shared }

type Third struct{ Only }

type Holder struct {
	First
	Second
	Third
}
`,
		sets: []methodSetNamesQuery{
			{object: "Holder", names: []string{"M"}},
			{object: "Holder", pointer: true, names: []string{"M"}},
		},
	},
	{
		// The duplicate marked is the type it duplicates, not the first at the depth: depth 2 is Unique, Shared,
		// Shared, so Shared's M collides and Unique's A is kept (#2br8dpa).
		name: "a duplicate after a unique type at one depth",
		source: `package fixture

type Unique struct{}

func (Unique) A() {}

type Shared struct{}

func (Shared) M() {}

type Near struct{ Unique }

type First struct{ Shared }

type Second struct{ Shared }

type Holder struct {
	Near
	First
	Second
}
`,
		sets: []methodSetNamesQuery{
			{object: "Holder", names: []string{"A"}},
			{object: "Holder", pointer: true, names: []string{"A"}},
		},
	},
	{
		// A depth folded of a duplicate is walked without the entries past its unique ones: depth 2 is Shared, Shared
		// and Contract, an alias of an interface literal, which no named type's seen check skips, so walking it twice
		// would collide its M away (#2br8dpa).
		name: "an unnamed type after a duplicate at one depth",
		source: `package fixture

type Shared struct{}

func (Shared) Twice() {}

type Contract = interface{ M() }

type First struct{ Shared }

type Second struct{ Shared }

type Third struct{ Contract }

type Holder struct {
	First
	Second
	Third
}
`,
		sets: []methodSetNamesQuery{
			{object: "Holder", names: []string{"M"}},
			{object: "Holder", pointer: true, names: []string{"M"}},
		},
		holds: func(test *testing.T, checked *types.Package) {
			if _, isAlias := methodSetNamesDeclared(test, checked, "Contract").(*types.Alias); !isAlias {
				test.Fatal("Contract is no *types.Alias, so the fixture proves nothing of an unnamed type")
			}
		},
	},
	{
		// An interface embedded twice at one depth, by a struct diamond, collides its methods as a struct does
		// (#2br8dpa).
		name: "an interface embedded twice at one depth",
		source: `package fixture

type Shaper interface{ Shape() }

type First struct{ Shaper }

type Second struct{ Shaper }

type Diamond struct {
	First
	Second
}
`,
		sets: []methodSetNamesQuery{
			{object: "Diamond", names: nil},
			{object: "Diamond", pointer: true, names: nil},
		},
	},
	{
		// A method whose receiver is an alias of a pointer is a pointer receiver's, so Base's value set lacks it.
		// indexPackage asks only of a pointer or an interface, so no production call reads a receiver; this holds the
		// namer to NewMethodSet, which the namer promises to match (#2br8dpa).
		name: "a receiver that is an alias of a pointer",
		source: `package fixture

type Base struct{}

type Pointer = *Base

func (Pointer) M() {}
`,
		sets: []methodSetNamesQuery{
			{object: "Base", names: nil},
			{object: "Base", pointer: true, names: []string{"M"}},
		},
		holds: func(test *testing.T, checked *types.Package) {
			base, isNamed := methodSetNamesDeclared(test, checked, "Base").(*types.Named)
			if !isNamed || base.NumMethods() != 1 {
				test.Fatal("Base declares no one method, so the fixture proves nothing of its receiver")
			}
			if _, isAlias := base.Method(0).Signature().Recv().Type().(*types.Alias); !isAlias {
				test.Fatalf("M's receiver is %s, no *types.Alias, so the fixture proves nothing of an alias of a "+
					"pointer", base.Method(0).Signature().Recv().Type())
			}
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
			if fixture.holds != nil {
				fixture.holds(test, checked)
			}
			namer := newMethodSetNamer()
			for _, query := range fixture.sets {
				set := methodSetNamesDeclared(test, checked, query.object)
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

// methodSetNamesDeclared is the type of the package-scope object checked declares as name.
func methodSetNamesDeclared(test *testing.T, checked *types.Package, name string) types.Type {
	test.Helper()
	object := checked.Scope().Lookup(name)
	if object == nil {
		test.Fatalf("the fixture declares no %s", name)
	}
	return object.Type()
}

// methodSetNamesField is the type of the field at index of fielded's underlying struct.
func methodSetNamesField(test *testing.T, fielded types.Type, index int) types.Type {
	test.Helper()
	structure, isStruct := fielded.Underlying().(*types.Struct)
	if !isStruct || index >= structure.NumFields() {
		test.Fatalf("%s has no field %d", fielded, index)
	}
	return structure.Field(index).Type()
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
