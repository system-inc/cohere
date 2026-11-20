package check

import (
	"go/types"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// digestsAlike reports whether name's closure digests in host and build, each a package checked here, are equal, asked
// in order of each name in before first, of one memo per build told sizes.
func digestsAlike(
	test *testing.T,
	host string,
	build string,
	sizes types.Sizes,
	before []string,
	name string,
) bool {
	test.Helper()
	digest := func(source string) cache.Key {
		checked := checkedInMemory(test, renderingPath, "package rendered\n\n"+source, nil)
		memo := NewReadsBuild()
		memo.measureBy(sizes)
		for _, earlier := range before {
			memo.closureDigest(checked.Scope().Lookup(earlier))
		}
		object := checked.Scope().Lookup(name)
		if object == nil {
			test.Fatalf("the package declares no %s", name)
		}
		return memo.closureDigest(object)
	}
	return digest(host) == digest(build)
}

// A cycle's members are one component, digested once from all their lines, whichever member is digested first: A and
// B point at each other and only A's Change differs, so B's digest differs too, asked after A's has been taken. Let a
// back edge to a member still on the stack lower nothing, or never mark a member on it, and B is a component alone,
// digested before A has a digest, so it reads alike in both builds.
func TestACycleIsDigestedWholeWhicheverMemberIsDigestedFirst(test *testing.T) {
	test.Parallel()

	host := "type A struct{ B *B; Change int }\n\ntype B struct{ A *A }\n"
	build := "type A struct{ B *B; Change string }\n\ntype B struct{ A *A }\n"
	for _, sizes := range []types.Sizes{nil, types.SizesFor("gc", "amd64")} {
		if digestsAlike(test, host, build, sizes, []string{"A"}, "B") {
			test.Errorf("sized %t: B's digest, asked after A's, is alike though A's Change differs", sizes != nil)
		}
	}
}

// An object's own type is read with its lines (closureDigest): in a graph told a build's sizes, V's unexported field f
// of an unnamed struct is written by its properties, an observed line with no edge to T, so V's digest differs when T
// grows from one byte to two. Leave the observed lines out of the object's own and V's digests are alike.
func TestAnObjectsOwnObservedFieldsAreInItsClosureDigest(test *testing.T) {
	test.Parallel()

	host := "type T [1]byte\n\nvar V struct{ f T }\n"
	build := "type T [2]byte\n\nvar V struct{ f T }\n"
	if digestsAlike(test, host, build, types.SizesFor("gc", "amd64"), nil, "V") {
		test.Error("V's digest is alike though its field f grows from one byte to two")
	}
}
