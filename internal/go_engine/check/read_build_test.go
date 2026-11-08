package check

import (
	"go/types"
	"maps"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/platform"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// dependencyPath is the fixture's dependency package, whose objects read by the platform a build targets.
const dependencyPath = "example.com/dependency/limits"

// readFixture is the fixture testdata/platform-digests/workspace as one build loads and reads it, with the
// registry's rules.
type readFixture struct {
	loaded   *load.Loaded
	objects  platform.ObjectDigests
	packages platform.PackageDigests
}

// readFixtures is each build's read of the fixture, made once per test binary and only read after.
var readFixtures struct {
	once    sync.Once
	linux   readFixture
	windows readFixture
	failure error
}

// readBuilds loads and reads the fixture under linux and under windows on first use.
func readBuilds(test *testing.T) (linux readFixture, windows readFixture) {
	test.Helper()
	readFixtures.once.Do(func() {
		for _, build := range []struct {
			operatingSystem string
			read            *readFixture
		}{{"linux", &readFixtures.linux}, {"windows", &readFixtures.windows}} {
			build.read.loaded, readFixtures.failure = loadFixtureFor(build.operatingSystem)
			if readFixtures.failure != nil {
				return
			}
			build.read.objects, build.read.packages, readFixtures.failure = ReadBuild(build.read.loaded, rules.All())
			if readFixtures.failure != nil {
				return
			}
		}
	})
	if readFixtures.failure != nil {
		test.Fatalf("reading the fixture's builds: %v", readFixtures.failure)
	}
	return readFixtures.linux, readFixtures.windows
}

// loadFixtureFor is the fixture's workspace loaded for operatingSystem on arm64, cgo off.
func loadFixtureFor(operatingSystem string) (*load.Loaded, error) {
	directory, absoluteError := filepath.Abs(filepath.Join("testdata", "platform-digests", "workspace"))
	if absoluteError != nil {
		return nil, absoluteError
	}
	return load.Packages(directory, []string{"./..."},
		[]string{"GOWORK=off", "GOOS=" + operatingSystem, "GOARCH=arm64", "CGO_ENABLED=0"}, nil)
}

// keyIn is the key of the dependency's object name in read's load, or of member, a method or field of it, when set.
func keyIn(test *testing.T, read readFixture, name string, member string) platform.ObjectKey {
	test.Helper()
	for _, analyzed := range read.loaded.Packages {
		for _, imported := range analyzed.Imports {
			if imported.PkgPath != dependencyPath {
				continue
			}
			object := imported.Types.Scope().Lookup(name)
			if object == nil {
				test.Fatalf("%s declares no %s", dependencyPath, name)
			}
			if member != "" {
				object, _, _ = types.LookupFieldOrMethod(object.Type(), true, object.Pkg(), member)
			}
			key, keyed := platform.KeyOf(object)
			if !keyed {
				test.Fatalf("%s.%s %s has no key", dependencyPath, name, member)
			}
			return key
		}
	}
	test.Fatalf("no workspace package imports %s", dependencyPath)
	return platform.ObjectKey{}
}

// An object a dependency declares differently in its linux and windows files reads differently under the two
// builds, a constant's value, a function's signature, and a type's field with it; one declared once reads alike, and
// so does a method declared alike on a type that differs, which reads as its own line; and one declared under linux
// alone is held by linux's read and not windows'.
func TestTwoBuildsReadADependencysObjectsApartWhereTheirDeclarationsDiffer(test *testing.T) {
	test.Parallel()
	linux, windows := readBuilds(test)
	for _, differing := range [][2]string{{"Size", ""}, {"Open", ""}, {"Handle", ""}, {"Handle", "descriptor"}} {
		key := keyIn(test, linux, differing[0], differing[1])
		if key != keyIn(test, windows, differing[0], differing[1]) {
			test.Fatalf("%v keys differently under the two builds", key)
		}
		if linux.objects[key] == windows.objects[key] {
			test.Errorf("want %v to read differently under linux and windows, both digest it alike", key)
		}
	}
	for _, alike := range [][2]string{{"Same", ""}, {"Shared", ""}, {"Handle", "Close"}, {"Origin", "Left"}} {
		key := keyIn(test, linux, alike[0], alike[1])
		linuxDigest, linuxHolds := linux.objects[key]
		windowsDigest, windowsHolds := windows.objects[key]
		if !linuxHolds || !windowsHolds || linuxDigest != windowsDigest {
			test.Errorf("want %v read alike under both builds, linux holds it %t and windows %t", key, linuxHolds,
				windowsHolds)
		}
	}
	onlyLinux := keyIn(test, linux, "OnlyLinux", "")
	if _, held := windows.objects[onlyLinux]; held {
		test.Errorf("windows' read holds %v, which only linux declares", onlyLinux)
	}
	if _, held := linux.objects[onlyLinux]; !held {
		test.Errorf("linux's read does not hold %v, which it declares", onlyLinux)
	}
}

// Halt renders alike under both builds, but never returns under linux, which ctrlflow's noReturn fact says, so its
// facts alone read it apart.
func TestAnObjectThatRendersAlikeReadsApartByItsFacts(test *testing.T) {
	test.Parallel()
	linux, windows := readBuilds(test)
	key := keyIn(test, linux, "Halt", "")
	renderings := [][]string{}
	for _, read := range []readFixture{linux, windows} {
		for _, analyzed := range read.loaded.Packages {
			if imported, imports := analyzed.Imports[dependencyPath]; imports {
				renderings = append(renderings, renderedObject(imported.Types.Scope().Lookup("Halt")))
				break
			}
		}
	}
	if len(renderings) != 2 || !slices.Equal(renderings[0], renderings[1]) {
		test.Fatalf("want Halt rendered alike under both builds, got %q", renderings)
	}
	if linux.objects[key] == windows.objects[key] {
		test.Errorf("want Halt read apart by its noReturn fact, both builds digest it alike")
	}
}

// A workspace package whose exported constant differs by platform gives its importers something else under each
// build, while one whose files are the same under both gives them the same.
func TestAWorkspacePackageReadsApartWhereItsExportDiffers(test *testing.T) {
	test.Parallel()
	linux, windows := readBuilds(test)
	for _, identifier := range []string{"example.com/workspace/ours", "example.com/workspace/user"} {
		if _, held := linux.packages[identifier]; !held {
			test.Fatalf("linux's read holds no digest of %s, only of %v", identifier,
				slices.Sorted(maps.Keys(linux.packages)))
		}
	}
	if linux.packages["example.com/workspace/ours"] == windows.packages["example.com/workspace/ours"] {
		test.Errorf("want ours read apart by its Width, both builds digest it alike")
	}
	if linux.packages["example.com/workspace/user"] != windows.packages["example.com/workspace/user"] {
		test.Errorf("want user read alike under both builds, they digest it apart")
	}
	if _, held := linux.objects[platform.ObjectKey{PackagePath: "example.com/workspace/ours", Path: "Width"}]; held {
		test.Errorf("the object digests hold a workspace package's object")
	}
}

// Reading the same load again gives the same maps, three reads so a choice made in a map's order, as of which
// declaration a field two of them share is read from, shows.
func TestReadingOneLoadAgainGivesTheSameDigests(test *testing.T) {
	test.Parallel()
	linux, _ := readBuilds(test)
	for range 3 {
		objects, ours, readError := ReadBuild(linux.loaded, rules.All())
		if readError != nil {
			test.Fatalf("reading the build again: %v", readError)
		}
		if !maps.Equal(objects, linux.objects) || !maps.Equal(ours, linux.packages) {
			test.Fatalf("a second read differs from the first: %d objects against %d, %d packages against %d",
				len(objects), len(linux.objects), len(ours), len(linux.packages))
		}
	}
}

// A workspace package's digest is the output digest a cache entry of it holds, the rules' slots that export no fact
// included, so the read and the cache agree on what an importer inherits. Both load for linux, under which every
// package of the fixture type-checks and so has an entry.
func TestAWorkspacePackagesDigestIsItsCacheEntrysOutput(test *testing.T) {
	test.Parallel()
	registered := rules.All()
	session := NewCacheSession(testCaching(test, registered), registered)
	directory, absoluteError := filepath.Abs(filepath.Join("testdata", "platform-digests", "workspace"))
	if absoluteError != nil {
		test.Fatalf("finding the fixture: %v", absoluteError)
	}
	cachedLoad, cachedLoadError := load.PackagesCached(directory, []string{"./..."},
		[]string{"GOWORK=off", "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0"}, nil,
		load.Caching{Restorer: session, Listings: session})
	if cachedLoadError != nil {
		test.Fatalf("loading the fixture with the cache: %v", cachedLoadError)
	}
	_, runError := Run(cachedLoad.Packages, registered, Options{Cache: session, Withheld: cachedLoad.Withheld})
	if runError != nil {
		test.Fatalf("running the registry's rules: %v", runError)
	}
	loaded, loadError := loadFixtureFor("linux")
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	_, ours, readError := ReadBuild(loaded, registered)
	if readError != nil {
		test.Fatalf("reading the build: %v", readError)
	}
	compared := 0
	for saved, output := range session.outputs {
		if digest, held := ours[saved.ID]; held {
			compared++
			if digest != output {
				test.Errorf("want %s's digest %x, its entry's output, got %x", saved.ID, output, digest)
			}
		}
	}
	if compared != len(ours) {
		test.Errorf("compared %d of the read's %d workspace packages with an entry", compared, len(ours))
	}
}
