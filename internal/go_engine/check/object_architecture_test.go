package check

import (
	"go/types"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
)

// architectureEnvironment is objectEnvironment's for goos with no tags, on architecture rather than the host's
// `GOARCH`, which every build objectEnvironment makes shares, so two of its builds never differ by architecture.
func architectureEnvironment(goos string, architecture string) []string {
	environment := objectEnvironment(goos, "")
	for index, pair := range environment {
		if strings.HasPrefix(pair, "GOARCH=") {
			environment[index] = "GOARCH=" + architecture
		}
	}
	return environment
}

// The same rule between two builds whose architectures differ, which no other run of the object differential on one
// machine meets, each of its builds being on the host's `GOARCH`: the architecture_sizes fixture loaded as linux/amd64,
// the host's side, and as linux/arm64, the build's, whatever the machine is. Its Pad is 64 bytes on amd64 and 128 on
// arm64, as internal/cpu's CacheLinePad is, its Guard holds an int32 on one and a func() on the other, and each other
// declaration of it holds one of the two another way.
//
// Twenty-seven of them have a line that differs and a bit only the read of what the line observes sets (#mtfdb1m), each
// a member whose own line writes every unexported field of an unnamed struct its type reaches by the field's
// properties under the build's sizes (closureWalkStep), where the names its spelled type holds read alike:
//   - by a size, an unnamed struct holding a Pad by value: the variables Features, Arrayed, Nested, Deeper, Foreign
//     and Tabled, which are one, Behind, Sliced, Mapped, Channeled and Called, which reach one through a pointer, a
//     slice, a map, a channel and a signature, the function Give, whose result is one, the interface method
//     Taker.Take, whose parameter is one, the fields Outer.Inner and Wrapped's, which are and point at one, and the
//     method Sized.Size, whose receiver's type parameter is constrained by a slice of one;
//   - by how it prints, with no size moving, an unnamed struct holding Guards behind a slice or a map, which printf's
//     match enters: the variables Listed and Keyed, Shelved and Stacked, whose slice of Guards is one named type
//     down, Boxed, whose slice of Guards is a generic's, told Guard by its type argument, Rooted, whose Tree holds a
//     slice of Trees, so the read of it ends only by knowing a type it has met, Ringed, whose Ring holds a slice of
//     itself with its type arguments swapped, so the read ends only where go/types hands back the instance it met,
//     Swapped, whose Ring of an int holds a Guard only in the Ring of a Guard inside it, which the read meets within
//     its own origin and reads by its type arguments, Aliased, whose struct is an alias's, the function
//     Constrained, whose type parameter's constraint is a struct holding a Shelf, and KeyedBy, whose map is keyed by
//     a Marked, an int32 on one and a string on the other.
//     Sized.Size, Ringed, Swapped, Aliased, Constrained and KeyedBy are each pinned set besides.
//
// The differential fails only where a line moves and no bit is set, so the members whose lines read alike in both
// are pinned too, each to neither bit set, and a read that widens past what a line observes fails here rather than
// leaving fewer members independent with no test to say so: Pointed and Guarded, behind a pointer, Signalled, behind
// a channel, and Handled, behind a signature, each as long and printing alike whatever is behind it; Spoken, behind
// an interface's method; Linked and Hooked, an unnamed struct holding a Pad behind a pointer and a signature inside an
// observed field, each spelled through an alias so no field of the member's own declaration holds it; Referred, whose
// Ref holds nothing of the Guard it is told; Named, Exported and Embedded, which write Pad by its name; and Hidden,
// which embeds an unexported type.
//
// Read a member's own type as any name (build_dependence's borrowedMemberReferences) and the twenty-seven fail; read an
// observed field's type in full and no further and Listed, Keyed, Shelved and Stacked fail; enter no named type's
// declaration from one and Shelved, Stacked and Boxed fail; leave a map's key out and KeyedBy fails, an alias's
// right-hand side read in full and no further and Aliased does; observe an instance's type arguments as what it holds
// and Referred's bit is set; enter a pointer from one and Pointed's, Guarded's and Referred's are, a channel and
// Signalled's is, a signature and Handled's is, an interface's methods and Spoken's is; read an observed field's type
// with the member's walk on and Linked's and Hooked's are, and observe an alias's underlying bit, which reads what a
// selection through Pointing dereferences, and Linked's is; observe an embedded field and Hidden's is; observe no
// struct a constraint spells and Constrained fails, a method's receiver's constraints read as any name and Sized.Size
// does; read an instance met within its own origin's by its type arguments' names and Swapped fails; forget the types
// met and the read of Rooted never ends.
func TestEveryObjectWhoseLinesDifferByArchitectureHasItsBitSet(test *testing.T) {
	test.Parallel()

	directory := filepath.Join("testdata", "architecture_sizes")
	host := loadObjects(test, directory, architectureEnvironment("linux", "amd64"), "./...", true)
	built := loadObjects(test, directory, architectureEnvironment("linux", "arm64"), "./...", true)
	if len(host.broken) > 0 || len(built.broken) > 0 {
		test.Fatalf("the fixture does not type-check: %q on amd64, %q on arm64", host.broken, built.broken)
	}
	hosted := host.packages["example.com/architecturesizes"]
	if hosted == nil {
		test.Fatal("the fixture's host holds no example.com/architecturesizes")
	}
	started := time.Now()
	dependence := objectDependence(host, built, objectFixtureWorkspace)

	// Pad's underlying bit is what says the two builds differ at all: unset, nothing below would be judged.
	if bits := dependence.ObjectBits(hosted.Types.Scope().Lookup("Pad")); !bits.Underlying {
		test.Errorf("want Pad's underlying bit set on arm64, got %+v", bits)
	}
	tally := objectDifferences(host, built, dependence.ObjectBits)
	reportObjectDifferences(test, "linux/arm64 against linux/amd64", tally, started)
	if tally.differedCovered == 0 {
		test.Error("no differing line is covered by a bit, so the two builds read alike and nothing is judged")
	}
	scope := hosted.Types.Scope()
	sized, isNamed := scope.Lookup("Sized").Type().(*types.Named)
	if !isNamed || sized.NumMethods() == 0 {
		test.Fatal("the fixture's Sized is no named type with a method")
	}
	for _, each := range []struct {
		name   string
		object types.Object
	}{
		{"KeyedBy", scope.Lookup("KeyedBy")},
		{"Aliased", scope.Lookup("Aliased")},
		{"Constrained", scope.Lookup("Constrained")},
		{"Sized.Size", sized.Method(0)},
		{"Ringed", scope.Lookup("Ringed")},
		{"Swapped", scope.Lookup("Swapped")},
	} {
		if bits := dependence.ObjectBits(each.object); !bits.Name {
			test.Errorf("want the bit of %s set on arm64, as its own line differs, got %+v", each.name, bits)
		}
	}
	alike := []string{"Pointed", "Guarded", "Signalled", "Handled", "Referred", "Named", "Exported", "Embedded",
		"Spoken", "Linked", "Hooked", "Hidden"}
	for _, name := range alike {
		if bits := dependence.ObjectBits(scope.Lookup(name)); bits != (build_dependence.Bits{}) {
			test.Errorf("want neither bit of %s set on arm64, as its line reads alike in both builds, got %+v", name,
				bits)
		}
	}
}

// Block 1's walk ends over any load, broken code included (#mtfdb1m): the expanding_instance fixture's Grown holds a
// slice of itself at a slice of what it holds, which go/types refuses as an instantiation cycle and still types, so
// every instance a walk enters names one it has not met. The graph of the load and Growing's bits, which observe
// Grown[int], are asked within a deadline, and a walk that does not end fails it rather than hanging the run, as it
// hung census's object test to its timeout when observe entered every instance it had not met.
func TestBlockOnesWalkEndsOverAnInstanceThatExpandsWithoutEnd(test *testing.T) {
	test.Parallel()

	host := loadObjects(test, filepath.Join("testdata", "expanding_instance"), objectEnvironment("linux", ""), "./...",
		false)
	hosted := host.packages["example.com/expandinginstance"]
	if hosted == nil || hosted.Types == nil || len(host.broken) == 0 {
		test.Fatalf("want the fixture typed and refused, as go/types refuses an instantiation cycle, got %q broken",
			host.broken)
	}
	judged := make(chan build_dependence.Bits, 1)
	go func() {
		dependence := objectDependence(host, host, objectFixtureWorkspace)
		judged <- dependence.ObjectBits(hosted.Types.Scope().Lookup("Growing"))
	}()
	select {
	case bits := <-judged:
		if bits != (build_dependence.Bits{}) {
			test.Errorf("want neither bit of Growing set by a build that changes nothing, got %+v", bits)
		}
	case <-time.After(30 * time.Second):
		test.Fatal("block 1's walk over the fixture did not end within 30 s")
	}
}
