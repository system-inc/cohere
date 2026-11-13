package check

import (
	"maps"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// Without a store, as under `--no-cache`, a run with an extra build hashes each file once, the standard library's
// among them (#b9f3bg4): the host keys its load to keep its packages for the build, and the standard library's keys
// read those digests, and the build keys its own load from the host's digests, which its standard library's keys read
// in turn. Each set of keys is the one computed with no digest given. Hashing the standard library again for its keys,
// the host and the build would each hash its files twice.
//
// Not parallel: it reads cache.FilesHashed, which every test in the process that keys a package moves.
func TestARunWithAnExtraBuildHashesEachStandardLibraryFileOnceWithoutAStore(test *testing.T) {
	root := test.TempDir()
	writeFiles(test, root, throughFiles)
	rules := throughRules()
	goRoot := goRootOf(test)
	storeless := func(build ...string) *Caching {
		caching := cachingAt(test.TempDir(), rules)
		caching.Store = nil
		caching.Context.Build = append(caching.Context.Build, build...)
		standardLibrary := standardLibraryContext("check tests", goRoot)
		standardLibrary.Build = append(standardLibrary.Build, build...)
		caching.StandardLibrary = &StandardLibraryCaching{Context: standardLibrary}
		return caching
	}
	before := cache.FilesHashed()
	hostSession := NewCacheSession(storeless(), rules)
	hostSession.KeepForBuilds()
	hostLoaded, hostLoadError := load.PackagesCached(root, []string{"./..."}, []string{"GOWORK=off"}, nil,
		load.Caching{Restorer: hostSession, RecordReads: true})
	if hostLoadError != nil || len(hostLoaded.Diagnostics) > 0 {
		test.Fatalf("loading the host's build: %v %+v", hostLoadError, hostLoaded.Diagnostics)
	}
	result, runError := Run(hostLoaded.Packages, rules, Options{Cache: hostSession, Withheld: hostLoaded.Withheld,
		Bodiless: hostLoaded.Bodiless, Reads: hostLoaded.Reads, WalkSlots: hostLoaded.WalkSlots, KeepForBuilds: true})
	if runError != nil || len(result.Crashes) > 0 {
		test.Fatalf("running the host's build: %v %+v", runError, result.Crashes)
	}
	result.ReadWalks.Wait()
	if result.Host.EntriesKept() == 0 {
		test.Fatalf("the host's run kept no entry, so the build restores nothing from it")
	}
	buildSession := NewCacheSession(storeless("GOOS=windows"), rules)
	buildSession.RestoreFromHost(result.Host)
	buildLoaded, buildLoadError := load.PackagesCached(root, []string{"./..."}, []string{"GOWORK=off", "GOOS=windows"},
		nil, load.Caching{Restorer: buildSession})
	if buildLoadError != nil || len(buildLoaded.Diagnostics) > 0 {
		test.Fatalf("loading windows's build: %v %+v", buildLoadError, buildLoaded.Diagnostics)
	}
	hashed := cache.FilesHashed() - before
	hostFiles := filesKeyedSet(hostLoaded.Packages)
	runFiles := maps.Clone(hostFiles)
	maps.Copy(runFiles, filesKeyedSet(buildLoaded.Packages))
	if len(runFiles) == len(hostFiles) {
		test.Fatalf("windows's build holds no file the host's does not, so nothing tells a file of its own apart")
	}
	if hashed != int64(len(runFiles)) {
		test.Errorf("the host's build and windows's hashed %d files, want each of the %d they hold hashed once",
			hashed, len(runFiles))
	}
	for name, session := range map[string]*CacheSession{"the host's": hostSession, "windows's": buildSession} {
		if len(session.standardLibraryKeys) == 0 {
			test.Fatalf("%s session keyed no standard-library package", name)
		}
		shared := session.caching.StandardLibrary.Context
		shared.Facts, shared.Loader = factAnalyzerSet(session.known), load.LoaderSource
		var reachable []*packages.Package
		for keyed := range session.standardLibraryKeys {
			reachable = append(reachable, keyed)
		}
		fresh, keysError := cache.StandardLibraryKeys(reachable, shared, cache.FileDigests{})
		if keysError != nil {
			test.Fatalf("keying %s standard library with no digest given: %v", name, keysError)
		}
		if !maps.Equal(fresh, session.standardLibraryKeys) {
			test.Errorf("%s standard-library keys differ from those computed with no digest given", name)
		}
	}
}

// filesKeyedSet is every distinct file of the packages loaded reaches that a key hashes, as filesKeyed counts them.
func filesKeyedSet(loaded []*packages.Package) map[string]bool {
	files := map[string]bool{}
	packages.Visit(loaded, nil, func(visited *packages.Package) {
		if visited.PkgPath == "unsafe" {
			return
		}
		for _, list := range [][]string{visited.GoFiles, visited.CompiledGoFiles, visited.OtherFiles,
			visited.EmbedFiles, visited.IgnoredFiles} {
			for _, file := range list {
				files[file] = true
			}
		}
	})
	return files
}
