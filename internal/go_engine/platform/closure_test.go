package platform

import (
	"go/types"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// workspacePath is the fixture workspace's module path, testdata/workspace, which requires testdata/dependency.
const workspacePath = "example.com/workspace"

// hostBuild and extraBuild are the fixture's two builds, on one architecture as every pair of builds is.
var (
	hostBuild  = project.Build{OperatingSystem: "linux", Architecture: "amd64"}
	extraBuild = project.Build{OperatingSystem: "windows", Architecture: "amd64"}
)

// fixture is the workspace loaded under both builds, once per test binary: each load type-checks the standard
// library from source, seconds of work every test only reads.
var fixture struct {
	once    sync.Once
	host    *load.Loaded
	extra   *load.Loaded
	failure error
}

// loadedFixture loads testdata/workspace under both builds on first use.
func loadedFixture(test *testing.T) (*load.Loaded, *load.Loaded) {
	test.Helper()
	fixture.once.Do(func() {
		directory, absoluteError := filepath.Abs(filepath.Join("testdata", "workspace"))
		if absoluteError != nil {
			fixture.failure = absoluteError
			return
		}
		loadUnder := func(target project.Build) (*load.Loaded, error) {
			return load.Packages(directory, []string{"./..."}, append([]string{"GOWORK=off"}, target.Environment()...),
				nil)
		}
		if fixture.host, fixture.failure = loadUnder(hostBuild); fixture.failure != nil {
			return
		}
		fixture.extra, fixture.failure = loadUnder(extraBuild)
	})
	if fixture.failure != nil {
		test.Fatalf("loading the fixture: %v", fixture.failure)
	}
	for _, loaded := range []*load.Loaded{fixture.host, fixture.extra} {
		for _, diagnostic := range loaded.Diagnostics {
			test.Errorf("the fixture does not compile, so it measures nothing: %s:%d:%d: %s", diagnostic.File,
				diagnostic.Line, diagnostic.Column, diagnostic.Message)
		}
	}
	return fixture.host, fixture.extra
}

// everyPackage is every package the load reaches from its workspace packages, dependencies included.
func everyPackage(loaded *load.Loaded) []*packages.Package {
	seen := map[string]bool{}
	var reached []*packages.Package
	var visit func(each *packages.Package)
	visit = func(each *packages.Package) {
		if seen[each.ID] {
			return
		}
		seen[each.ID] = true
		reached = append(reached, each)
		for _, imported := range each.Imports {
			visit(imported)
		}
	}
	for _, each := range loaded.Packages {
		visit(each)
	}
	return reached
}

// inWorkspace reports whether a package path is the fixture workspace's.
func inWorkspace(path string) bool {
	return path == workspacePath || strings.HasPrefix(path, workspacePath+"/")
}

// digestOf is a hand-built digest of a name, and another when the build reads it differently.
func digestOf(name string, differs bool) cache.Key {
	if differs {
		return cache.DigestOf([]byte(name), []byte("differs"))
	}
	return cache.DigestOf([]byte(name))
}

// objectDigestsOf stands in for unit 2's digests: every package outside the workspace the load reaches, its
// package-level objects, and the methods and fields of its named types, each with a digest of its key, and another
// for each key in differing.
func objectDigestsOf(loaded *load.Loaded, differing []ObjectKey) map[ObjectKey]cache.Key {
	digests := map[ObjectKey]cache.Key{}
	add := func(object types.Object) {
		if key, keyed := KeyOf(object); keyed {
			digests[key] = digestOf(key.PackagePath+" "+string(key.Path), slices.Contains(differing, key))
		}
	}
	for _, each := range everyPackage(loaded) {
		if inWorkspace(each.PkgPath) || each.Types == nil {
			continue
		}
		packageKey := ObjectKey{PackagePath: each.PkgPath}
		digests[packageKey] = digestOf(each.PkgPath, slices.Contains(differing, packageKey))
		scope := each.Types.Scope()
		for _, name := range scope.Names() {
			object := scope.Lookup(name)
			add(object)
			named, isNamed := object.Type().(*types.Named)
			if _, isTypeName := object.(*types.TypeName); !isTypeName || !isNamed {
				continue
			}
			for method := range named.Methods() {
				add(method)
			}
			switch underlying := named.Underlying().(type) {
			case *types.Struct:
				for field := range underlying.Fields() {
					add(field)
				}
			case *types.Interface:
				for method := range underlying.ExplicitMethods() {
					add(method)
				}
			}
		}
	}
	return digests
}

// packageDigestsOf stands in for unit 2's: every workspace package the load reaches, by load ID, with a digest of
// its ID, and another for each ID in differing.
func packageDigestsOf(loaded *load.Loaded, differing []string) map[string]cache.Key {
	digests := map[string]cache.Key{}
	for _, each := range everyPackage(loaded) {
		if inWorkspace(each.PkgPath) {
			digests[each.ID] = digestOf(each.ID, slices.Contains(differing, each.ID))
		}
	}
	return digests
}

// dependencyKeys are the keys of the dependency's objects the extra build reads differently: Named, which named
// references by name; Outer's embedded field Inner, which promoted passes through to read Field; Shape, the type
// of implicit's type switch binding; and Handle, the type of expression's value no identifier names.
func dependencyKeys(test *testing.T, loaded *load.Loaded) (ObjectKey, ObjectKey, ObjectKey, ObjectKey) {
	test.Helper()
	index := slices.IndexFunc(everyPackage(loaded), func(each *packages.Package) bool {
		return each.PkgPath == "example.com/dependency"
	})
	if index < 0 {
		test.Fatal("the load reaches no example.com/dependency")
	}
	scope := everyPackage(loaded)[index].Types.Scope()
	keyOf := func(object types.Object) ObjectKey {
		key, keyed := KeyOf(object)
		if !keyed {
			test.Fatalf("%v has no key", object)
		}
		return key
	}
	outer, isStruct := scope.Lookup("Outer").Type().Underlying().(*types.Struct)
	if !isStruct || outer.NumFields() != 1 || !outer.Field(0).Embedded() {
		test.Fatal("dependency.Outer is not a struct embedding Inner")
	}
	return keyOf(scope.Lookup("Named")), keyOf(outer.Field(0)), keyOf(scope.Lookup("Shape")),
		keyOf(scope.Lookup("Handle"))
}

// fixtureClosure is the closure of the extra build over the fixture, with every digest the same but those the
// cases name: the four dependency objects, and the outputs of seed, its test variant, and importer.
func fixtureClosure(test *testing.T) ([]Member, ObjectKey, ObjectKey, ObjectKey, ObjectKey) {
	test.Helper()
	host, extra := loadedFixture(test)
	named, embedded, shape, handle := dependencyKeys(test, extra)
	differingPackages := []string{workspacePath + "/seed", workspacePath + "/seed [" + workspacePath + "/seed.test]",
		workspacePath + "/importer"}
	members, closureError := Closure(
		BuildLoad{Build: hostBuild, Packages: host.Packages, ObjectDigests: objectDigestsOf(host, nil),
			PackageDigests: packageDigestsOf(host, nil)},
		BuildLoad{Build: extraBuild, Packages: extra.Packages,
			ObjectDigests:  objectDigestsOf(extra, []ObjectKey{named, embedded, shape, handle}),
			PackageDigests: packageDigestsOf(extra, differingPackages)},
	)
	if closureError != nil {
		test.Fatalf("the closure is refused: %v", closureError)
	}
	return members, named, embedded, shape, handle
}

// TestClosureHoldsEachRule holds Closure to each of its rules over the fixture, one case each, by the reasons it
// gives a package, or none for a package left out:
//   - a seed by its file set (red with the file comparison dropped), and by being in the extra build alone (red
//     with only packages both builds have compared);
//   - a reference to a differing object by name (red with Uses unread), through an embedded field to a promoted
//     field (red with Selection.Index unwalked), through a type switch's binding (red with Implicits and Types
//     unread, since the guard's expression has the binding's type too), and through the type of a value no
//     identifier names (red with only Uses, Implicits and Selections read);
//   - a package referencing only objects read alike, out (red with every outside reference counted);
//   - an importer of a seed whose output differs, in (red with importers unfollowed), an importer of that importer,
//     whose own output differs, in (red with only seeds' importers followed), and an importer of a seed whose output
//     is the same, out (red with importers followed whatever the output).
func TestClosureHoldsEachRule(test *testing.T) {
	test.Parallel()
	members, named, embedded, shape, handle := fixtureClosure(test)
	reasonsOf := map[string][]Reason{}
	for _, member := range members {
		reasonsOf[member.Id] = member.Reasons
	}
	seed := Reason{Kind: ReasonKindSeed}
	cases := map[string]struct {
		id   string
		want []Reason
	}{
		"a seed by its file set":                {"sameseed", []Reason{seed}},
		"a seed in the extra build alone":       {"windowsonly", []Reason{seed}},
		"a test variant seed":                   {"seed [" + workspacePath + "/seed.test]", []Reason{seed}},
		"a reference by name":                   {"named", []Reason{{Kind: ReasonKindReference, Object: named}}},
		"a reference through an embedded field": {"promoted", []Reason{{Kind: ReasonKindReference, Object: embedded}}},
		"a reference by an implicit":            {"implicit", []Reason{{Kind: ReasonKindReference, Object: shape}}},
		"a reference by an expression's type":   {"expression", []Reason{{Kind: ReasonKindReference, Object: handle}}},
		"only identical references":             {"identical", nil},
		"an importer of a differing seed": {"importer", []Reason{{Kind: ReasonKindImporter,
			Imported: workspacePath + "/seed"}}},
		"an importer of a differing importer": {"chained", []Reason{{Kind: ReasonKindImporter,
			Imported: workspacePath + "/importer"}}},
		"an importer of a seed alike":          {"sameimporter", nil},
		"a package the host's build alone has": {"linuxonly", nil},
	}
	for name, each := range cases {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			id := workspacePath + "/" + each.id
			if got := reasonsOf[id]; !reflect.DeepEqual(got, each.want) {
				test.Errorf("%s has reasons %+v, where %+v were expected", id, got, each.want)
			}
		})
	}
}

// TestClosureCountsAKeyNeitherBuildHoldsAsDiffering holds Closure to taking a reference whose key neither build's
// ObjectDigests holds as differing, since a missing key is unknown and unknown widens the closure: named, whose
// dependency.Named is in neither map and alike in nothing else, is in, and identical stays out (red with a key
// both builds lack read as alike).
func TestClosureCountsAKeyNeitherBuildHoldsAsDiffering(test *testing.T) {
	test.Parallel()
	host, extra := loadedFixture(test)
	named, _, _, _ := dependencyKeys(test, extra)
	hostObjects, extraObjects := objectDigestsOf(host, nil), objectDigestsOf(extra, nil)
	delete(hostObjects, named)
	delete(extraObjects, named)
	members, closureError := Closure(
		BuildLoad{Build: hostBuild, Packages: host.Packages, ObjectDigests: hostObjects,
			PackageDigests: packageDigestsOf(host, nil)},
		BuildLoad{Build: extraBuild, Packages: extra.Packages, ObjectDigests: extraObjects,
			PackageDigests: packageDigestsOf(extra, nil)},
	)
	if closureError != nil {
		test.Fatalf("the closure is refused: %v", closureError)
	}
	reasonsOf := map[string][]Reason{}
	for _, member := range members {
		reasonsOf[member.Id] = member.Reasons
	}
	want := []Reason{{Kind: ReasonKindReference, Object: named}}
	if got := reasonsOf[workspacePath+"/named"]; !reflect.DeepEqual(got, want) {
		test.Errorf("named, whose reference neither build digests, has reasons %+v, where %+v were expected", got, want)
	}
	if got, member := reasonsOf[workspacePath+"/identical"]; member {
		test.Errorf("identical, whose references both builds digest alike, is in with %+v", got)
	}
}

// TestClosureIsSortedAndStable holds Closure's members to being sorted by load ID with each member's reasons
// sorted, and to the same answer twice (red with the members left in the order a map gives them), and to naming
// no package the extra build does not analyze, as the plain seed its importer imports (red with every workspace
// package of the load a member).
func TestClosureIsSortedAndStable(test *testing.T) {
	test.Parallel()
	first, _, _, _, _ := fixtureClosure(test)
	second, _, _, _, _ := fixtureClosure(test)
	if !reflect.DeepEqual(first, second) {
		test.Errorf("two closures of the same loads differ:\n%+v\n%+v", first, second)
	}
	ids := []string{}
	for _, member := range first {
		ids = append(ids, strings.TrimPrefix(member.Id, workspacePath+"/"))
		if !slices.IsSortedFunc(member.Reasons, compareReasons) {
			test.Errorf("%s's reasons are not sorted: %+v", member.Id, member.Reasons)
		}
	}
	want := []string{"chained", "expression", "implicit", "importer", "named", "promoted", "sameseed",
		"seed [" + workspacePath + "/seed.test]", "windowsonly"}
	if !slices.Equal(ids, want) {
		test.Errorf("the closure is %q, where %q was expected", ids, want)
	}
}

// TestClosureRefusesAnotherArchitecture holds Closure to refusing an extra build on another `GOARCH` than the
// host's, naming the layout blind spot that makes one unsound (red with the architecture unchecked).
func TestClosureRefusesAnotherArchitecture(test *testing.T) {
	test.Parallel()
	other := project.Build{OperatingSystem: "windows", Architecture: "arm64"}
	_, closureError := Closure(BuildLoad{Build: hostBuild}, BuildLoad{Build: other})
	if closureError == nil || !strings.Contains(closureError.Error(), "layout read without a name") ||
		!strings.Contains(closureError.Error(), "SA1027") {
		test.Errorf("a build on another architecture is not refused for the layout blind spot: %v", closureError)
	}
}

// TestClosureRefusesOneBuildTwice holds Closure to refusing the host's build handed as the extra build, which has
// nothing to differ by (red with the builds uncompared).
func TestClosureRefusesOneBuildTwice(test *testing.T) {
	test.Parallel()
	_, closureError := Closure(BuildLoad{Build: hostBuild}, BuildLoad{Build: hostBuild})
	if closureError == nil || !strings.Contains(closureError.Error(), "is the host's build") {
		test.Errorf("one build handed twice is not refused: %v", closureError)
	}
}
