package load

import (
	"errors"
	"path/filepath"
	"testing"
)

// The gate a load reaches before its type check (Caching.AtGate) is told every package the load holds, the same count
// Loaded.PackagesLoaded reads off the roots once the load has ended, so a share at the gate and the record's final
// share are over one total (#ghb3rcn). Over reachedRootsFiles, with every root and with the analyzed roots only, which
// lists tool and never reaches it, the gate is reached once, and with no Restorer every package is checked from source.
func TestTheGateCountsEveryPackageTheLoadLoads(test *testing.T) {
	test.Parallel()
	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	writeListingFiles(test, root, reachedRootsFiles)
	for _, analyzedRootsOnly := range []bool{false, true} {
		calls, fromSourceAtGate, packagesAtGate := 0, 0, 0
		loaded, loadError := PackagesCached(root, []string{"./..."}, []string{"GOWORK=off"}, nil, Caching{
			AnalyzedRootsOnly: analyzedRootsOnly,
			AtGate: func(fromSource int, packages int) error {
				calls++
				fromSourceAtGate, packagesAtGate = fromSource, packages
				return nil
			},
		})
		if loadError != nil {
			test.Fatalf("loading %s: %v", root, loadError)
		}
		if calls != 1 || packagesAtGate != loaded.PackagesLoaded || fromSourceAtGate != packagesAtGate {
			test.Errorf("with the analyzed roots only %t the gate was reached %d times, told %d of %d packages "+
				"from source, want once, every one of the %d the load loaded", analyzedRootsOnly, calls,
				fromSourceAtGate, packagesAtGate, loaded.PackagesLoaded)
		}
	}
}

// A gate that refuses the load ends it with its error, and the load returns no packages.
func TestARefusalAtTheGateEndsTheLoad(test *testing.T) {
	test.Parallel()
	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	writeListingFiles(test, root, reachedRootsFiles)
	refused := errors.New("refused at the gate")
	loaded, loadError := PackagesCached(root, []string{"./..."}, []string{"GOWORK=off"}, nil, Caching{
		AtGate: func(int, int) error { return refused },
	})
	if !errors.Is(loadError, refused) || loaded != nil {
		test.Errorf("a refused load returned %v and packages %t, want the refusal and none", loadError, loaded != nil)
	}
}
