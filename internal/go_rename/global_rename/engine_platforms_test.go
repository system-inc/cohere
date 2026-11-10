package global_rename

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

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
		target{PackageSuffix: "internal/a", Name: "ParseURL", Kind: KindFunc, NewName: "ParseUrl"},
	), Options{LoadPlatform: loader})
	requireAccepted(test, untouched, 1)
	if len(counts) != 0 || len(untouched.Platforms()) != 0 {
		test.Fatalf("a rename reaching no platform's file loaded %v", counts)
	}

	reaching := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/b", Name: "FormatURL", Kind: KindFunc, NewName: "FormatUrl"},
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
	formatUrl := target{PackageSuffix: "internal/b", Name: "FormatURL", Kind: KindFunc, NewName: "FormatUrl"}

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
		target{PackageSuffix: "internal/b", Name: "FormatURL", Kind: KindFunc, NewName: "FormatUrl"},
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
		target{PackageSuffix: "internal/b", Name: name, Kind: KindFunc, NewName: newName},
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
	if _, stored := expectation.platformFingerprints[windows]; !stored {
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
		fresh, freshError := provePlatform(loaded.LoadPlatform, windows, overlay.files, expectation.newNameAt, nil)
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

// One stored fingerprint serves every proof of a round, each with its own renames, and none of them changes it: two
// proofs of two plans' rewrites over one plan's fingerprint each read what a fresh load reads, and the fingerprint is
// still the platform's own after both.
func TestAPlatformProofLeavesTheStoredFingerprintAsItWas(test *testing.T) {
	test.Parallel()
	fixture := platformFixture(test, "package b\n\nfunc windowsOnly() string { return FormatURL() + Run() }\n")
	loaded := fixture.load(test)
	windows := workspace.Platform{OperatingSystem: "windows", Architecture: "amd64"}
	formatResult, formatFiles := windowsPlan(test, loaded, "FormatURL", "FormatUrl", Options{})
	runResult, runFiles := windowsPlan(test, loaded, "Run", "RunAll", Options{})
	stored, read := formatResult.platformFingerprints[windows]
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
		newNameAt := Expect(loaded, plan.result).newNameAt
		fromStored, storedError := provePlatform(loaded.LoadPlatform, windows, plan.files, newNameAt, &stored)
		fresh, freshError := provePlatform(loaded.LoadPlatform, windows, plan.files, newNameAt, nil)
		if storedError != nil || freshError != nil {
			test.Fatal(storedError, freshError)
		}
		if !reflect.DeepEqual(fromStored, fresh) || len(fresh.Mismatches) != 0 {
			test.Errorf("renaming %s, the stored fingerprint's proof reads %+v, a fresh load's %+v", plan.name,
				fromStored, fresh)
		}
	}
	freshLoad, loadError := loaded.LoadPlatform(windows, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	if !reflect.DeepEqual(stored, fingerprintBefore(freshLoad.Clean)) {
		test.Error("the stored fingerprint changed under the proofs it served")
	}
}

// A proof with the plan's fingerprint of a platform asks for the rewrite's load only, never the load before it; a
// platform the plan did not read is loaded before the rewrite too, as it always was.
func TestAPlatformProofWithTheStoredFingerprintLoadsOnlyTheRewrite(test *testing.T) {
	test.Parallel()
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
