package global_rename

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"github.com/system-inc/cohere/internal/go_rename/rename"
	"github.com/system-inc/cohere/internal/go_rename/workspace"
)

// platformFixture is a package with no file another platform builds, and one whose function a file only windows
// builds calls.
func platformFixture(test *testing.T, windowsFile string) fixture {
	test.Helper()
	return newFixture(test, map[string]string{
		"internal/a/a.go": "package a\n\nfunc ParseURL(text string) string { return text }\n",
		"internal/b/b.go": "package b\n\nfunc FormatURL() string { return \"\" }\n\nfunc Run() string { " +
			"return FormatURL() }\n",
		"internal/b/b_windows.go": windowsFile,
	})
}

// countingLoader is workspace's own platform load, counting each load by platform.
func countingLoader(loaded *workspace.Workspace) (
	func(platform workspace.Platform, overlay map[string][]byte) (*workspace.PlatformLoad, error), map[string]int,
) {
	var lock sync.Mutex
	counts := map[string]int{}
	return func(platform workspace.Platform, overlay map[string][]byte) (*workspace.PlatformLoad, error) {
		lock.Lock()
		counts[platform.String()]++
		lock.Unlock()
		return loaded.LoadPlatform(platform, overlay)
	}, counts
}

// A plan loads a platform only for the files of a package a rename reaches: a rename in a package with no file another
// platform builds loads none, the cost a plan that never reads platforms pays, and a rename whose package holds a
// file only windows builds loads windows once and renames the name there too.
func TestOnlyAPlatformARenameReachesIsLoaded(test *testing.T) {
	test.Parallel()
	fixture := platformFixture(test, "package b\n\nfunc windowsOnly() string { return FormatURL() }\n")
	loaded := fixture.load(test)

	loader, counts := countingLoader(loaded)
	untouched := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/a", Name: "ParseURL", Kind: KindFunction, NewName: "ParseUrl"},
	), Options{LoadPlatform: loader})
	requireAccepted(test, untouched, 1)
	if len(counts) != 0 || len(untouched.Platforms()) != 0 {
		test.Fatalf("a rename reaching no platform's file loaded %v", counts)
	}

	reaching := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/b", Name: "FormatURL", Kind: KindFunction, NewName: "FormatUrl"},
	), Options{LoadPlatform: loader})
	requireAccepted(test, reaching, 1)
	if counts["windows/amd64"] != 1 || len(counts) != 1 || len(reaching.TwinEdits) != 1 ||
		len(reaching.RenamesUnder(workspace.Platform{OperatingSystem: "windows", Architecture: "amd64"})) != 1 {
		test.Fatalf("want windows loaded once and the rename made there, got %v and %d twin edits", counts,
			len(reaching.TwinEdits))
	}
}

// With RequireSoundPlatforms, a rename reaching a package that does not type-check under a platform that builds one
// of its files is refused, naming the platform. Without it, the file is read as text, as it always was.
func TestRequireSoundPlatformsRefusesWhatAPlatformCannotType(test *testing.T) {
	test.Parallel()
	fixture := platformFixture(test, "package b\n\nvar broken int = FormatURL()\n")
	loaded := fixture.load(test)
	formatUrl := target{PackageSuffix: "internal/b", Name: "FormatURL", Kind: KindFunction, NewName: "FormatUrl"}

	strict := Plan(loaded, propose(test, loaded, formatUrl), Options{RequireSoundPlatforms: true})
	requireRefused(test, strict, "FormatURL", "platform")
	if !strings.Contains(strict.Rejections[0].Reason, "does not type-check as windows/amd64") {
		test.Errorf("the refusal reads %q, want it to name windows/amd64", strict.Rejections[0].Reason)
	}
	lenient := Plan(loaded, propose(test, loaded, formatUrl), Options{})
	for _, rejection := range lenient.Rejections {
		if rejection.Class == "platform" {
			test.Errorf("without RequireSoundPlatforms the rename was refused as %q", rejection.Reason)
		}
	}
}

// A platform the rewrite cannot be loaded under is a PlatformLoad mismatch naming it, never a proof that held.
func TestAPlatformThatDoesNotLoadIsAMismatch(test *testing.T) {
	test.Parallel()
	fixture := platformFixture(test, "package b\n\nfunc windowsOnly() string { return FormatURL() }\n")
	loaded := fixture.load(test)
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/b", Name: "FormatURL", Kind: KindFunction, NewName: "FormatUrl"},
	), Options{})
	windows := workspace.Platform{OperatingSystem: "windows", Architecture: "amd64"}
	failing := Options{LoadPlatform: func(workspace.Platform, map[string][]byte) (*workspace.PlatformLoad, error) {
		return nil, errors.New("planted")
	}}
	verification := ProvePlatform(loaded, Expect(loaded, result), windows, nil, failing)
	if len(verification.Mismatches) != 1 || verification.Mismatches[0].Kind != MismatchKindPlatformLoad ||
		verification.Mismatches[0].Platform != "windows/amd64" {
		test.Fatalf("want one PlatformLoad mismatch under windows/amd64, got %+v", verification.Mismatches)
	}
	files, rewriteError := RewrittenFiles(result)
	if rewriteError != nil {
		test.Fatal(rewriteError)
	}
	proven := ProvePlatform(loaded, Expect(loaded, result), windows, files, Options{})
	if len(proven.Mismatches) != 0 || proven.Compared == 0 {
		test.Fatalf("the plan's rewrite under windows: %d compared, mismatches %v", proven.Compared, proven.Mismatches)
	}
}

// windowsPlan is a plan, over the windows platform fixture, renaming name in internal/b, whose package holds a file
// only windows builds, and its rewrite.
func windowsPlan(
	test *testing.T,
	loaded *workspace.Workspace,
	name string,
	newName string,
	options Options,
) (*Result, map[string][]byte) {
	test.Helper()
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/b", Name: name, Kind: KindFunction, NewName: newName},
	), options)
	requireAccepted(test, result, 1)
	files, rewriteError := RewrittenFiles(result)
	if rewriteError != nil {
		test.Fatal(rewriteError)
	}
	return result, files
}

// A platform's proof from the fingerprint the plan took of the platform's load reads exactly what a proof that loads
// the platform again before the rewrite reads, mismatches included: over the rewrite, over no rewrite, where every
// renamed identifier reads back under its old name, and over a rewrite that leaves the windows file unrenamed, where
// its package fails to type-check after.
func TestAPlatformProofFromThePlansFingerprintReadsAsAFreshLoad(test *testing.T) {
	test.Parallel()
	fixture := platformFixture(test, "package b\n\nfunc windowsOnly() string { return FormatURL() }\n")
	loaded := fixture.load(test)
	windows := workspace.Platform{OperatingSystem: "windows", Architecture: "amd64"}
	result, files := windowsPlan(test, loaded, "FormatURL", "FormatUrl", Options{})
	expectation := Expect(loaded, result)
	if _, stored := expectation.platformDifferences[windows]; !stored {
		test.Fatalf("the plan read windows and kept no fingerprint of it: %v", result.Platforms())
	}
	hostOnly := map[string][]byte{}
	for filename, contents := range files {
		if !strings.HasSuffix(filename, "_windows.go") {
			hostOnly[filename] = contents
		}
	}
	if len(hostOnly) == len(files) {
		test.Fatalf("the rewrite edits no windows file: %v", files)
	}
	for _, overlay := range []struct {
		name       string
		files      map[string][]byte
		mismatches bool
	}{
		{"the rewrite", files, false},
		{"no rewrite", nil, true},
		{"the rewrite without the windows file", hostOnly, true},
	} {
		stored := ProvePlatform(loaded, expectation, windows, overlay.files, Options{})
		fresh, freshError := provePlatform(loaded.LoadPlatform, windows, overlay.files, expectation, nil)
		if freshError != nil {
			test.Fatal(freshError)
		}
		if !reflect.DeepEqual(stored, fresh) {
			test.Errorf("over %s, the stored fingerprint's proof reads %+v, a fresh load's %+v", overlay.name, stored,
				fresh)
		}
		if (len(stored.Mismatches) > 0) != overlay.mismatches || stored.Compared == 0 {
			test.Errorf("over %s, %d compared and mismatches %v", overlay.name, stored.Compared, stored.Mismatches)
		}
	}
}

// One stored difference serves every proof of a round, each with its own renames, and none of them changes it or the
// host's expectation it reads: two proofs of two plans' rewrites over one plan's difference each read what a fresh
// load reads, the difference is still the platform's own from the host's after both, and each host expectation is
// what a fresh one is.
func TestAPlatformProofLeavesTheStoredFingerprintAsItWas(test *testing.T) {
	test.Parallel()
	fixture := platformFixture(test, "package b\n\nfunc windowsOnly() string { return FormatURL() + Run() }\n")
	loaded := fixture.load(test)
	windows := workspace.Platform{OperatingSystem: "windows", Architecture: "amd64"}
	formatResult, formatFiles := windowsPlan(test, loaded, "FormatURL", "FormatUrl", Options{})
	runResult, runFiles := windowsPlan(test, loaded, "Run", "RunAll", Options{})
	stored, read := formatResult.platformDifferences[windows]
	if !read {
		test.Fatalf("the plan read windows and kept no fingerprint of it: %v", formatResult.Platforms())
	}
	for _, plan := range []struct {
		name   string
		result *Result
		files  map[string][]byte
	}{
		{"FormatURL", formatResult, formatFiles},
		{"Run", runResult, runFiles},
	} {
		expectation := Expect(loaded, plan.result)
		fromStored, storedError := provePlatform(loaded.LoadPlatform, windows, plan.files, expectation, &stored)
		fresh, freshError := provePlatform(loaded.LoadPlatform, windows, plan.files, expectation, nil)
		if storedError != nil || freshError != nil {
			test.Fatal(storedError, freshError)
		}
		if !reflect.DeepEqual(fromStored, fresh) || len(fresh.Mismatches) != 0 {
			test.Errorf("renaming %s, the stored fingerprint's proof reads %+v, a fresh load's %+v", plan.name,
				fromStored, fresh)
		}
		if !reflect.DeepEqual(expectation.Fingerprints, Expect(loaded, plan.result).Fingerprints) {
			test.Errorf("renaming %s, the host's expectation changed under the proofs that read it", plan.name)
		}
	}
	freshLoad, loadError := loaded.LoadPlatform(windows, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	if !reflect.DeepEqual(stored, differenceFrom(fingerprintBefore(loaded), fingerprintBefore(freshLoad.Clean))) {
		test.Error("the stored fingerprint changed under the proofs it served")
	}
}

// A proof with the plan's fingerprint of a platform asks for the rewrite's load only, never the load before it; a
// platform the plan did not read is loaded before the rewrite too, as it always was.
func TestAPlatformProofWithTheStoredFingerprintLoadsOnlyTheRewrite(test *testing.T) {
	test.Parallel()
	if checkDifference != nil {
		test.Skip("a rename_differential build loads every stored proof's before side too, to check it")
	}
	fixture := platformFixture(test, "package b\n\nfunc windowsOnly() string { return FormatURL() }\n")
	loaded := fixture.load(test)
	var lock sync.Mutex
	// loads counts each load by platform and whether it read an overlay.
	loads := map[string]int{}
	loader := func(platform workspace.Platform, overlay map[string][]byte) (*workspace.PlatformLoad, error) {
		lock.Lock()
		if len(overlay) == 0 {
			loads[platform.String()+" before"]++
		} else {
			loads[platform.String()+" rewrite"]++
		}
		lock.Unlock()
		return loaded.LoadPlatform(platform, overlay)
	}
	options := Options{LoadPlatform: loader}
	result, files := windowsPlan(test, loaded, "FormatURL", "FormatUrl", options)
	expectation := Expect(loaded, result)
	windows := workspace.Platform{OperatingSystem: "windows", Architecture: "amd64"}
	proven := ProvePlatform(loaded, expectation, windows, files, options)
	if len(proven.Mismatches) != 0 || proven.Compared == 0 {
		test.Fatalf("the rewrite under windows: %d compared, mismatches %v", proven.Compared, proven.Mismatches)
	}
	if want := map[string]int{"windows/amd64 before": 1, "windows/amd64 rewrite": 1}; !reflect.DeepEqual(loads, want) {
		test.Fatalf("the plan and its windows proof loaded %v, want %v: the plan's load only, then the rewrite's",
			loads, want)
	}
	darwin := workspace.Platform{OperatingSystem: "darwin", Architecture: "amd64"}
	ProvePlatform(loaded, expectation, darwin, files, options)
	if loads["darwin/amd64 before"] != 1 || loads["darwin/amd64 rewrite"] != 1 {
		test.Errorf("a proof under a platform the plan did not read loaded %v, want its load before and after", loads)
	}
}

// A file the host and a platform both build whose identifiers resolve otherwise there, here a call to a function each
// declares in a file of its own, is kept whole in the platform's difference, never read as the host's, and the proof
// from the difference still reads exactly what a fresh load's does; a file both read alike is only marked.
func TestASharedFileThatResolvesOtherwiseUnderAPlatformIsKept(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": "package a\n\nfunc ParseURL(text string) string { return text }\n",
		"internal/b/b.go": "package b\n\nfunc FormatURL() string { return \"\" }\n\nfunc Run() string { " +
			"return helper() + FormatURL() }\n",
		"internal/b/c.go":              "package b\n\nfunc Format() string { return FormatURL() }\n",
		"internal/b/helper_windows.go": "package b\n\nfunc helper() string { return FormatURL() }\n",
		"internal/b/helper_other.go":   "//go:build !windows\n\npackage b\n\nfunc helper() string { return \"\" }\n",
	})
	loaded := fixture.load(test)
	windows := workspace.Platform{OperatingSystem: "windows", Architecture: "amd64"}
	result, files := windowsPlan(test, loaded, "FormatURL", "FormatUrl", Options{})
	expectation := Expect(loaded, result)
	difference, stored := expectation.platformDifferences[windows]
	if !stored {
		test.Fatalf("the plan read windows and kept no difference of it: %v", result.Platforms())
	}
	// fileStates says, for each file of package b under windows, whether the difference keeps it or marks it.
	fileStates := map[string]string{}
	for id, byFile := range difference.fingerprints {
		for filename := range byFile {
			fileStates[filepath.Base(filename)] = "kept " + id
		}
	}
	for id, sameAsHost := range difference.sameAsHost {
		for filename := range sameAsHost {
			fileStates[filepath.Base(filename)] = "marked " + id
		}
	}
	want := map[string]string{
		"a.go": "marked fixture/internal/a", "b.go": "kept fixture/internal/b", "c.go": "marked fixture/internal/b",
		"helper_windows.go": "kept fixture/internal/b",
	}
	if !reflect.DeepEqual(fileStates, want) {
		test.Errorf("windows' difference reads %v, want %v", fileStates, want)
	}
	hostOnly := map[string][]byte{}
	for filename, contents := range files {
		if !strings.HasSuffix(filename, "_windows.go") {
			hostOnly[filename] = contents
		}
	}
	for _, overlay := range []struct {
		name       string
		files      map[string][]byte
		mismatches bool
	}{
		{"the rewrite", files, false},
		{"no rewrite", nil, true},
		{"the rewrite without the windows file", hostOnly, true},
	} {
		fromStored := ProvePlatform(loaded, expectation, windows, overlay.files, Options{})
		fresh, freshError := provePlatform(loaded.LoadPlatform, windows, overlay.files, expectation, nil)
		if freshError != nil {
			test.Fatal(freshError)
		}
		if !reflect.DeepEqual(fromStored, fresh) {
			test.Errorf("over %s, the stored difference's proof reads %+v, a fresh load's %+v", overlay.name,
				fromStored, fresh)
		}
		if (len(fromStored.Mismatches) > 0) != overlay.mismatches || fromStored.Compared == 0 {
			test.Errorf("over %s, %d compared and mismatches %v", overlay.name, fromStored.Compared,
				fromStored.Mismatches)
		}
	}
}

// An expectation of a workspace other than the one the plan read, as a codemod that plans over one load and applies
// over a reload makes, carries none of the plan's platform differences, which are differences from the plan's own
// workspace, so its platform proof loads the platform before the rewrite and reads what a fresh load does.
func TestAnExpectationOfAnotherWorkspaceCarriesNoPlatformDifference(test *testing.T) {
	test.Parallel()
	fixture := platformFixture(test, "package b\n\nfunc windowsOnly() string { return FormatURL() }\n")
	loaded := fixture.load(test)
	reloaded := fixture.load(test)
	windows := workspace.Platform{OperatingSystem: "windows", Architecture: "amd64"}
	result, files := windowsPlan(test, loaded, "FormatURL", "FormatUrl", Options{})
	expectation := Expect(reloaded, result)
	if len(expectation.platformDifferences) != 0 {
		test.Fatalf("an expectation of a reload carries the plan's differences for %d platforms",
			len(expectation.platformDifferences))
	}
	var lock sync.Mutex
	// loads counts each load by whether it read an overlay.
	loads := map[string]int{}
	loader := func(platform workspace.Platform, overlay map[string][]byte) (*workspace.PlatformLoad, error) {
		lock.Lock()
		if len(overlay) == 0 {
			loads["before"]++
		} else {
			loads["rewrite"]++
		}
		lock.Unlock()
		return reloaded.LoadPlatform(platform, overlay)
	}
	proven := ProvePlatform(reloaded, expectation, windows, files, Options{LoadPlatform: loader})
	if want := map[string]int{"before": 1, "rewrite": 1}; !reflect.DeepEqual(loads, want) {
		test.Errorf("the proof over a reload loaded %v, want %v", loads, want)
	}
	fresh, freshError := provePlatform(reloaded.LoadPlatform, windows, files, expectation, nil)
	if freshError != nil {
		test.Fatal(freshError)
	}
	if !reflect.DeepEqual(proven, fresh) || len(proven.Mismatches) != 0 || proven.Compared == 0 {
		test.Errorf("the proof over a reload reads %+v, a fresh load's %+v", proven, fresh)
	}
}

// Fingerprint keeps each key once: every record of one key holds the same string, as two uses of one declaration
// show, and each key reads as it always did, a declaration's by its declaring identifier's ordinal in its file.
func TestFingerprintSharesEachKey(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": "package a\n\nfunc ParseURL(text string) string { return text }\n\n" +
			"func One() string { return ParseURL(\"\") }\n\nfunc Two() string { return ParseURL(\"\") }\n",
	})
	fingerprints, _ := Fingerprint(fixture.load(test))
	byFile := fingerprints["fixture/internal/a"]
	if len(byFile) != 1 {
		test.Fatalf("want one file of package a, got %v", fingerprints)
	}
	// storage is where a string's bytes are, so two equal strings read as one only when they share them.
	storage := func(text string) *byte {
		// Unsafe: the pointer is only compared, never read through or kept past the fingerprint it points into.
		return unsafe.StringData(text)
	}
	held := map[string]*byte{}
	var uses []rename.IdentifierRecord
	for filename, records := range byFile {
		if records[1] != (rename.IdentifierRecord{Name: "ParseURL", Key: "declared:" + filename + "#1|-"}) {
			test.Errorf("ParseURL declares as %+v", records[1])
		}
		for _, record := range records {
			if pointer, seen := held[record.Key]; seen && pointer != storage(record.Key) {
				test.Errorf("two records of %q hold two strings", record.Key)
			}
			held[record.Key] = storage(record.Key)
			if record.Name == "ParseURL" && record.Key == "-|declared:"+filename+"#1" {
				uses = append(uses, record)
			}
		}
	}
	if len(uses) != 2 || storage(uses[0].Key) != storage(uses[1].Key) {
		test.Errorf("want ParseURL's two uses to share one key, got %+v", uses)
	}
}
