package load

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// heldModule is a module of one package of two files, neither importing anything on disk: held.go, which the tests
// hold other bytes of, and other.go, which they hold nothing of.
var heldModule = map[string]string{
	"go.mod":   "module example.com/held\n\ngo 1.24\n",
	"held.go":  "package held\n\n// One is one.\nfunc One() int { return 1 }\n",
	"other.go": "package held\n\n// Other is two.\nfunc Other() int { return 2 }\n",
}

// heldSource is held.go as the run holds it, which differs from the disk's: it imports strings and declares Two.
const heldSource = "package held\n\nimport \"strings\"\n\n// One is one.\nfunc One() int { return 1 }\n\n" +
	"// Two is the length of two.\nfunc Two() int { return len(strings.Repeat(\"x\", 2)) }\n"

// heldFor is a Held that holds file alone, with contents.
func heldFor(file string, contents string) Held {
	return func(asked string) ([]byte, bool) {
		if asked == file {
			return []byte(contents), true
		}
		return nil, false
	}
}

// The parse reads a file the run holds from its held bytes, not the disk: the type-checked package has the
// declaration only the held bytes have, and no error, since the import those bytes add is listed too. A file it holds
// nothing of is read from disk.
func TestTheParseReadsTheHeldBytes(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	writeListingFiles(test, directory, heldModule)
	held := heldFor(filepath.Join(directory, "held.go"), heldSource)
	loaded, loadError := PackagesCached(directory, []string{"./..."}, []string{"GOWORK=off"}, nil, Caching{Held: held})
	if loadError != nil {
		test.Fatal(loadError)
	}
	if len(loaded.Packages) != 1 {
		test.Fatalf("loaded %d packages, want 1", len(loaded.Packages))
	}
	if loaded.Packages[0].Types.Scope().Lookup("Two") == nil {
		test.Error("the package has no Two, so the parse read held.go from disk")
	}
	if loaded.Packages[0].Types.Scope().Lookup("Other") == nil {
		test.Error("the package has no Other, so other.go, which nothing holds, was not read from disk")
	}
	if len(loaded.Diagnostics) > 0 {
		test.Errorf("the load found %+v", loaded.Diagnostics)
	}
}

// The in-process lister reads a file the run holds from its held bytes, so a package lists the imports those bytes
// write, not the disk's.
func TestTheListerReadsTheHeldBytes(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	writeListingFiles(test, directory, heldModule)
	goEnvironment := goEnvironmentOf(test, directory, []string{"GOWORK=off"})
	held := heldFor(filepath.Join(directory, "held.go"), heldSource)
	listed, doubt := listInProcess(goEnvironment, []string{"./..."}, held, nil, nil)
	if doubt != "" {
		test.Fatalf("the lister did not answer: %s", doubt)
	}
	listedPackage := listed.byPath["example.com/held"]
	if listedPackage == nil {
		test.Fatalf("the lister did not list example.com/held: %v", sortedKeys(listed.roots))
	}
	if !slices.Equal(listedPackage.Imports, []string{"strings"}) {
		test.Errorf("example.com/held imports %v, want the held bytes' [strings]", listedPackage.Imports)
	}
	if listed.byPath["strings"] == nil {
		test.Error("the lister did not list strings, which only the held bytes import")
	}
}

// The lister's prefetch of the standard library reads through held, and the caller writes what held reads once the
// listing returns, as the pipeline's reread does (#5qhgt92), so the listing returns only after every read the prefetch
// started has ended. heldModule imports nothing on disk, so every read of the standard library here is the prefetch's.
// The fixture's own read waits until the prefetch's first read has begun, so a read is under way before the listing
// can end, and that first read waits until the listing has stopped the prefetch, so it is still under way when the
// listing's own work is done: it has ended when the listing returns only if the listing waited for it.
func TestThePrefetchEndsWithTheListing(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	writeListingFiles(test, directory, heldModule)
	goEnvironment := goEnvironmentOf(test, directory, []string{"GOWORK=off"})
	// abandoned frees the first read when the test ends, so a listing that never stops the prefetch leaves nothing
	// waiting on it.
	abandoned := make(chan struct{})
	test.Cleanup(func() { close(abandoned) })
	prefetchReading := make(chan struct{})
	var firstRead sync.Once
	var firstReadEnded atomic.Bool
	// lister is set before the listing starts the prefetch, whose first read is the only one that reads it.
	var lister *inProcessLister
	held := Held(func(file string) ([]byte, bool) {
		if strings.HasPrefix(file, directory+string(filepath.Separator)) {
			<-prefetchReading
			return nil, false
		}
		isFirst := false
		firstRead.Do(func() {
			isFirst = true
			close(prefetchReading)
		})
		if isFirst {
			select {
			case <-lister.prefetchDone:
			case <-abandoned:
			}
			firstReadEnded.Store(true)
		}
		return nil, false
	})
	built, doubt := newInProcessLister(goEnvironment, held, nil, nil)
	if doubt != "" {
		test.Fatalf("the lister could not be built: %s", doubt)
	}
	lister = built
	if _, doubt = lister.list([]string{"./..."}); doubt != "" {
		test.Fatalf("the lister did not answer: %s", doubt)
	}
	if !firstReadEnded.Load() {
		test.Error("the listing returned while a read the prefetch started was still under way")
	}
}
