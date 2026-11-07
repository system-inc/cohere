package global_rename

import (
	"errors"
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
