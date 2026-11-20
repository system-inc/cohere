package build_dependence

import (
	"go/types"
	"testing"

	"golang.org/x/tools/go/packages"
)

// widthSource is host.go of a package parsed here, a file only the host compiles that declares Width, so every unit of
// it is a seed under widthDifference.
const widthSource = "const Width = 4\n"

// widthDifference is a build that leaves out host.go of each of loaded, every unit of it a seed, and shares
// shared.go.
func widthDifference(loaded ...*packages.Package) Difference {
	difference := Difference{Packages: map[string]PackageDifference{}}
	for _, each := range loaded {
		difference.Packages[each.ID] = PackageDifference{SharedFiles: []string{"shared.go"},
			HostOnlyFiles: []string{"host.go"}}
	}
	return difference
}

// sharedBesideWidth is the package identifier of shared, a file both builds compile, beside host.go, which declares
// Width (widthSource), importing imported.
func sharedBesideWidth(
	test *testing.T,
	identifier string,
	shared string,
	imported ...*packages.Package,
) *packages.Package {
	test.Helper()
	return parsedPackage(test, identifier, [][2]string{
		{"shared.go", "package " + identifier + "\n\n" + shared},
		{"host.go", "package " + identifier + "\n\n" + widthSource},
	}, imported...)
}

// graphOfParsed is the graph of loaded, each added as a workspace package.
func graphOfParsed(loaded ...*packages.Package) *Graph {
	graph := NewGraph()
	for _, each := range loaded {
		graph.AddPackage(each, true, nil)
	}
	return graph
}

// lookup is the object name names in loaded's package scope, failing the test when there is none.
func lookup(test *testing.T, loaded *packages.Package, name string) types.Object {
	test.Helper()
	object := loaded.Types.Scope().Lookup(name)
	if object == nil {
		test.Fatalf("%s declares no %s", loaded.PkgPath, name)
	}
	return object
}

// expectAnswer fails when answered is not want, named by what was asked.
func expectAnswer(test *testing.T, asked string, name bool, underlying bool, want Bits) {
	test.Helper()
	if got := (Bits{Name: name, Underlying: underlying}); got != want {
		test.Errorf("%s: want %+v, got %+v", asked, want, got)
	}
}

// An object or a name of a package the other graph does not hold has no counterpart to answer for it, so it answers
// both bits set, failing wide, and is counted without its package, though the other graph is judged and holds a
// package of its own: Steady, a plain int of outside, would read nothing set through its type. Send such an object to
// the by-name reading because a judgment is present, or answer such a name without its name bit, and it reads as
// independent.
func TestCounterpartsFailWideForAPackageTheOtherGraphDoesNotHold(test *testing.T) {
	test.Parallel()

	dependency := sharedBesideWidth(test, "dependency", "type Layout struct{ items [Width]byte }\n")
	graph := graphOfParsed(dependency)
	outside := parsedPackage(test, "outside", [][2]string{{"outside.go", "package outside\n\nvar Steady int\n"}})
	counterparts := NewCounterparts(graph, graph.Against(widthDifference(dependency)))
	name, underlying := counterparts.DependentObject(lookup(test, outside, "Steady"))
	expectAnswer(test, "outside's Steady", name, underlying, Bits{Name: true, Underlying: true})
	name, underlying, isDeclared := counterparts.DependentName(outside.PkgPath, "Steady")
	expectAnswer(test, "outside's Steady by name", name, underlying, Bits{Name: true, Underlying: true})
	if isDeclared {
		test.Error("outside's Steady by name: want it undeclared, got declared")
	}
	if counts := counterparts.Counts(); counts != (CounterpartCounts{WithoutPackage: 2}) {
		test.Errorf("want both asked counted without their package, got %+v", counts)
	}
}

// An object whose path names nothing alike in the other graph's package of its path has no counterpart, so it answers
// both bits set, failing wide, and is counted without a counterpart: Wanted, which the other package does not declare;
// Record's First, whose path names the other Record's Second; and Shape, a type where the other package declares a
// var. Each found object's own bits are unset, so reading them in place of failing wide reads as independent, and a
// path that finds nothing hands ObjectBits no object at all. Record, alike in both, is answered by its counterpart.
func TestCounterpartsFailWideForAnObjectWithNoCounterpartAlike(test *testing.T) {
	test.Parallel()

	other := sharedBesideWidth(test, "dependency", "type Record struct{ Second int }\n\nvar Shape int\n")
	graph := graphOfParsed(other)
	asked := sharedBesideWidth(test, "dependency",
		"type Record struct{ First int }\n\ntype Shape struct{}\n\nvar Wanted int\n")
	counterparts := NewCounterparts(graph, graph.Against(widthDifference(other)))
	record, isStruct := lookup(test, asked, "Record").Type().Underlying().(*types.Struct)
	if !isStruct {
		test.Fatal("the asked Record is no struct")
	}
	for _, each := range []struct {
		asked  string
		object types.Object
	}{
		{"Wanted", lookup(test, asked, "Wanted")},
		{"Record's First", record.Field(0)},
		{"Shape", lookup(test, asked, "Shape")},
	} {
		if counterpart, isFound := counterparts.Counterpart(each.object); isFound {
			test.Errorf("%s: want no counterpart, got %v", each.asked, counterpart)
		}
		name, underlying := counterparts.DependentObject(each.object)
		expectAnswer(test, each.asked, name, underlying, Bits{Name: true, Underlying: true})
	}
	name, underlying := counterparts.DependentObject(lookup(test, asked, "Record"))
	expectAnswer(test, "Record", name, underlying, Bits{})
	if counts := counterparts.Counts(); counts != (CounterpartCounts{Answered: 1, WithoutCounterpart: 3}) {
		test.Errorf("want Record answered and the other three counted without a counterpart, got %+v", counts)
	}
}

// A name of a package the other graph holds is answered by the bits the other graph's judgment gives it, each bit as
// its own, and whether a unit there declares it: Layout, whose layout alone moves, has its underlying bit alone set,
// and Missing, which no unit declares, nothing set and is undeclared, so the record's expansion of it is read in its
// place. Swap the bits, or say Missing declared, and the answer is another's.
func TestCounterpartsAnswerANameByItsOwnBits(test *testing.T) {
	test.Parallel()

	dependency := sharedBesideWidth(test, "dependency", "type Layout struct{ items [Width]byte }\n")
	graph := graphOfParsed(dependency)
	counterparts := NewCounterparts(graph, graph.Against(widthDifference(dependency)))
	for _, each := range []struct {
		local    string
		want     Bits
		declared bool
	}{
		{"Layout", Bits{Underlying: true}, true},
		{"Missing", Bits{}, false},
	} {
		name, underlying, isDeclared := counterparts.DependentName(dependency.PkgPath, each.local)
		expectAnswer(test, each.local, name, underlying, each.want)
		if isDeclared != each.declared {
			test.Errorf("%s: want declared %t, got %t", each.local, each.declared, isDeclared)
		}
	}
}

// An object of a package the other graph holds by a record read back, which no unit there declares, is answered by
// what reading it through its type reaches, each object reached by its own package's name and by the bit it reaches:
// Extra, a var of upper the other graph's upper lacks, reaches dependency's Changing by name, whose method moves its
// name bit alone, so Extra answers both bits set; Elsewhere reaches a type of outside, a package the other graph does
// not hold, and fails wide; Kept reaches dependency's Steady, which moves nothing, and answers nothing set. Look the
// reached name up in the root's package, read the reached object's underlying bit for a name it reaches, or call a
// package held where only the root's is, and Extra or Elsewhere reads as independent.
func TestCounterpartsFollowADecodedReferenceIntoItsPackage(test *testing.T) {
	test.Parallel()

	dependencySource := "type Changing struct{}\n\nfunc (Changing) Apply(items [Width]int) {}\n\n" +
		"type Steady struct{}\n"
	dependency := sharedBesideWidth(test, "dependency", dependencySource)
	upper := parsedPackage(test, "upper", [][2]string{{"shared.go",
		"package upper\n\nimport \"example.com/dependency\"\n\nvar Held dependency.Changing\n"}}, dependency)
	recorded := recordedGraphOf(test, graphOfParsed(dependency, upper))
	counterparts := NewCounterparts(recorded, recorded.Against(widthDifference(dependency)))

	askedDependency := sharedBesideWidth(test, "dependency", dependencySource)
	outside := parsedPackage(test, "outside", [][2]string{{"outside.go", "package outside\n\ntype Thing struct{}\n"}})
	askedUpper := parsedPackage(test, "upper", [][2]string{{"shared.go", "package upper\n\n" +
		"import (\n\t\"example.com/dependency\"\n\t\"example.com/outside\"\n)\n\n" +
		"var Held dependency.Changing\n\nvar Extra dependency.Changing\n\nvar Elsewhere outside.Thing\n\n" +
		"var Kept dependency.Steady\n"}}, askedDependency, outside)
	for _, each := range []struct {
		local string
		want  Bits
	}{
		{"Extra", Bits{Name: true, Underlying: true}},
		{"Elsewhere", Bits{Name: true, Underlying: true}},
		{"Kept", Bits{}},
	} {
		name, underlying := counterparts.DependentObject(lookup(test, askedUpper, each.local))
		expectAnswer(test, each.local, name, underlying, each.want)
	}
}

// A method of an instance of a generic type of a package the other graph holds by a record read back is answered as
// its origin, the method its record names: Apply, whose signature reads Width, has its name bit set, and so does
// Box[int]'s Apply. Look the instance's method up as itself and it reads as independent.
func TestCounterpartsAnswerAnInstancesMethodByItsOrigin(test *testing.T) {
	test.Parallel()

	source := "type Box[T any] struct{ value T }\n\nfunc (Box[T]) Apply(items [Width]int) {}\n\nvar Boxed Box[int]\n"
	dependency := sharedBesideWidth(test, "dependency", source)
	recorded := recordedGraphOf(test, graphOfParsed(dependency))
	counterparts := NewCounterparts(recorded, recorded.Against(widthDifference(dependency)))
	asked := sharedBesideWidth(test, "dependency", source)
	method, _, _ := types.LookupFieldOrMethod(lookup(test, asked, "Boxed").Type(), false, asked.Types, "Apply")
	instance, isFunction := method.(*types.Func)
	if !isFunction || instance.Origin() == instance {
		test.Fatalf("want Box[int]'s Apply an instance of Box's, got %v", method)
	}
	for _, each := range []struct {
		asked  string
		object types.Object
	}{
		{"Box's Apply", instance.Origin()},
		{"Box[int]'s Apply", instance},
	} {
		name, underlying := counterparts.DependentObject(each.object)
		expectAnswer(test, each.asked, name, underlying, Bits{Name: true})
	}
}
