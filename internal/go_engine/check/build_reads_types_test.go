package check

import (
	"fmt"
	"slices"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/printf"
)

// typeReadsFiles are a module whose package user reads another package, base, every way the type check records: a
// call of os.Getpid, a parameter of base's Outer by value, a field of base reached through a field base embeds and user
// never names, so only the selection's path holds it, and base's generic, instantiated. user's own struct and its
// field are its own, never a read.
var typeReadsFiles = map[string]string{
	"go.mod": "module example.com/reads\n\ngo 1.21\n",
	"base/base.go": "package base\n\ntype inner struct{ Value int }\n\n// Outer embeds inner.\n" +
		"type Outer struct{ inner }\n\n// Identity is its argument.\nfunc Identity[Type any](value Type) Type {\n" +
		"\treturn value\n}\n",
	"user/user.go": "package user\n\nimport (\n\t\"os\"\n\n\t\"example.com/reads/base\"\n)\n\n" +
		"type own struct{ Count int }\n\n// Run reads base and os.\nfunc Run(outer base.Outer) int {\n" +
		"\tvar mine own\n\treturn os.Getpid() + outer.Value + mine.Count + base.Identity(1)\n}\n",
}

// loadRecordingAt is a load of root's packages of the test's own that gives every package reads to record into, as a
// run with extra builds loads.
func loadRecordingAt(test *testing.T, root string) *load.Loaded {
	test.Helper()
	loaded, loadError := load.PackagesCached(root, []string{"./..."}, []string{"GOWORK=off"}, nil,
		load.Caching{RecordReads: true})
	if loadError != nil {
		test.Fatalf("loading %s: %v", root, loadError)
	}
	return loaded
}

// loadedTypeReads writes typeReadsFiles under a directory of the test's and loads it, failing the test on any
// diagnostic.
func loadedTypeReads(test *testing.T) (string, *load.Loaded) {
	test.Helper()

	directory := test.TempDir()
	writeFiles(test, directory, typeReadsFiles)
	loaded := loadRecordingAt(test, directory)
	if len(loaded.Diagnostics) > 0 {
		test.Fatalf("the fixture loaded with diagnostics: %+v", loaded.Diagnostics)
	}
	return directory, loaded
}

// readsOfPath is the reads result holds of the package at path, not a test variant, failing the test when it holds
// none.
func readsOfPath(test *testing.T, reads map[*packages.Package]*build_reads.Reads, path string) *build_reads.Reads {
	test.Helper()

	for recorded, packageReads := range reads {
		if recorded.PkgPath == path && recorded.ForTest == "" {
			return packageReads
		}
	}
	test.Fatalf("the run holds no reads of %s", path)
	return nil
}

// A package's type reads, walked by the run once its last action has finished, are every object of another package
// its check reached, each with the kind of use it made of it, the field it reached only through a selection's path and
// the generic it instantiated among them, and the selection itself, from the first named type of base on its way, with
// the embedded field it passes through selected by its own name from there too, and never an object of its own or of
// the universe. Outer is named and taken by value, a use of it whole.
func TestAPackagesTypeReadsAreEveryForeignObjectItReached(test *testing.T) {
	test.Parallel()

	_, loaded := loadedTypeReads(test)
	result, runError := Run(loaded.Packages, []rule.Rule{houseRule(quietAnalyzer)}, Options{Reads: loaded.Reads})
	if runError != nil {
		test.Fatal(runError)
	}
	// The type-read walks run past Run, so the reads are whole only once they end.
	result.ReadWalks.Wait()
	reads := readsOfPath(test, result.Reads, "example.com/reads/user")
	if restorable, why := reads.Restorable(); !restorable {
		test.Fatalf("user is not restorable: %s", why)
	}
	var names []string
	for _, use := range reads.Uses() {
		names = append(names, use.Object.Pkg().Path()+"."+use.Object.Name()+" "+string(use.Kind))
	}
	want := []string{
		"example.com/reads/base.Identity Member",
		"example.com/reads/base.Outer Name",
		"example.com/reads/base.Outer Whole",
		"example.com/reads/base.Value Member",
		"example.com/reads/base.inner Member",
		"os.Getpid Member",
	}
	if !slices.Equal(names, want) {
		test.Fatalf("want user's reads\n%q\ngot\n%q", want, names)
	}
	var selections []string
	for _, selection := range reads.Selections() {
		selections = append(selections, fmt.Sprintf("%s.%s.%s %v", selection.Receiver.Pkg().Path(),
			selection.Receiver.Name(), selection.Name, selection.Index))
	}
	want = []string{"example.com/reads/base.Outer.Value [0 0]", "example.com/reads/base.Outer.inner [0]"}
	if !slices.Equal(selections, want) {
		test.Fatalf("want user's selections\n%q\ngot\n%q", want, selections)
	}
}

// unsettledReads are the packages of loaded's graph whose reads in result are neither recorded nor marked never, each
// by ID, and the packages of the graph that have no reads at all.
func unsettledReads(loaded *load.Loaded, reads map[*packages.Package]*build_reads.Reads) (unknown, missing []string) {
	packages.Visit(loaded.Roots, nil, func(visited *packages.Package) {
		packageReads, held := reads[visited]
		switch {
		case !held:
			missing = append(missing, visited.ID)
		case !packageReads.Recorded() && packageReads.Never() == "":
			unknown = append(unknown, visited.ID)
		}
	})
	return unknown, missing
}

// Every package of the graph ends the run with its type reads recorded or marked never, whichever way it took: walked
// as its last action finished, with printf's facts reaching every dependency; walked after the rules, where a rule
// that learns no facts reaches no dependency; or, warm, restored from the findings cache, which marks it never with
// the reason. None is left unknown.
func TestEveryPackageEndsItsRunRecordedOrNever(test *testing.T) {
	test.Parallel()

	for _, enabled := range [][]rule.Rule{{houseRule(quietAnalyzer)}, {registeredRule(test, printf.Analyzer)}} {
		_, loaded := loadedTypeReads(test)
		result, runError := Run(loaded.Packages, enabled, Options{Reads: loaded.Reads})
		if runError != nil {
			test.Fatal(runError)
		}
		// The type-read walks run past Run, so the reads are whole only once they end.
		result.ReadWalks.Wait()
		if unknown, missing := unsettledReads(loaded, result.Reads); len(unknown)+len(missing) > 0 {
			test.Errorf("running %s, want every package recorded or never, got unknown %q and missing %q",
				enabled[0].Name, unknown, missing)
		}
	}

	directory, _ := loadedTypeReads(test)
	enabled := []rule.Rule{registeredRule(test, printf.Analyzer)}
	caching := testCaching(test, enabled)
	for _, run := range []string{"cold", "warm"} {
		session := NewCacheSession(caching, enabled)
		loaded, loadError := load.PackagesCached(directory, []string{"./..."}, []string{"GOWORK=off"}, nil,
			load.Caching{Restorer: session, Listings: session, RecordReads: true})
		if loadError != nil {
			test.Fatal(loadError)
		}
		result, runError := Run(loaded.Packages, enabled, Options{Cache: session, Withheld: loaded.Withheld,
			Reads: loaded.Reads})
		if runError != nil {
			test.Fatal(runError)
		}
		// The type-read walks run past Run, so the reads are whole only once they end.
		result.ReadWalks.Wait()
		if unknown, missing := unsettledReads(loaded, result.Reads); len(unknown)+len(missing) > 0 {
			test.Errorf("%s, want every package recorded or never, got unknown %q and missing %q", run, unknown,
				missing)
		}
		if never := readsOfPath(test, result.Reads, "example.com/reads/user").Never(); run == "warm" &&
			never != build_reads.RestoredFromCache {
			test.Errorf("warm, want user restored from the findings cache and marked so, got %q", never)
		}
	}
}

// The type-read walks run past Run's return, so Result.Reads read before ReadWalks.Wait can be partial: with user's
// walk held, user reads unknown, and once the walk is let go and waited for, recorded.
func TestTheReadsAreWholeOnlyOnceTheWalksAreWaitedFor(test *testing.T) {
	test.Parallel()

	_, loaded := loadedTypeReads(test)
	hold := make(chan struct{})
	options := Options{Reads: loaded.Reads, holdWalk: func(owner *packages.Package) {
		if owner.PkgPath == "example.com/reads/user" && owner.ForTest == "" {
			<-hold
		}
	}}
	result, runError := Run(loaded.Packages, []rule.Rule{houseRule(quietAnalyzer)}, options)
	if runError != nil {
		close(hold)
		test.Fatal(runError)
	}
	user := readsOfPath(test, result.Reads, "example.com/reads/user")
	if restorable, _ := user.Restorable(); restorable || user.Recorded() {
		close(hold)
		test.Fatal("user's reads read whole while its walk was held")
	}
	close(hold)
	result.ReadWalks.Wait()
	if restorable, why := user.Restorable(); !restorable {
		test.Fatalf("user's reads, waited for, are not restorable: %s", why)
	}
}
