//go:build rename_differential

package global_rename

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_rename/rename"
	"github.com/system-inc/cohere/internal/go_rename/workspace"
)

// The differential holds a stored difference's proof equal to the full way's and is silent, and panics, naming the
// platform, package and file, when a marked file's records are not the full way's.
func TestTheDifferentialPanicsOnAMarkedFileThatReadsOtherwise(test *testing.T) {
	test.Parallel()
	fixture := platformFixture(test, "package b\n\nfunc windowsOnly() string { return FormatURL() }\n")
	loaded := fixture.load(test)
	windows := workspace.Platform{OperatingSystem: "windows", Architecture: "amd64"}
	result, files := windowsPlan(test, loaded, "FormatURL", "FormatUrl", Options{})
	expectation := Expect(loaded, result)
	stored := expectation.platformDifferences[windows]
	if _, proveError := provePlatform(loaded.LoadPlatform, windows, files, expectation, &stored); proveError != nil {
		test.Fatal(proveError)
	}
	// tampered is the host expectation with the first record of one marked file given another name.
	tampered := &Expectation{
		Fingerprints: map[string]map[string][]rename.IdentifierRecord{},
		Offsets:      expectation.Offsets,
		newNameAt:    expectation.newNameAt,
	}
	marked := ""
	for _, id := range slices.Sorted(maps.Keys(expectation.Fingerprints)) {
		tampered.Fingerprints[id] = maps.Clone(expectation.Fingerprints[id])
		for _, filename := range slices.Sorted(maps.Keys(stored.sameAsHost[id])) {
			if marked != "" {
				break
			}
			records := slices.Clone(expectation.Fingerprints[id][filename])
			records[0].Name += "Tampered"
			tampered.Fingerprints[id][filename] = records
			marked = filename
		}
	}
	if marked == "" {
		test.Fatalf("windows' difference marks no file: %+v", stored.sameAsHost)
	}
	defer func() {
		recovered, _ := recover().(string)
		if !strings.Contains(recovered, "windows/amd64") || !strings.Contains(recovered, marked) ||
			!strings.Contains(recovered, "marked true") {
			test.Errorf("want a panic naming windows/amd64 and the marked %s, got %q", marked, recovered)
		}
	}()
	_, proveError := provePlatform(loaded.LoadPlatform, windows, files, tampered, &stored)
	test.Errorf("a marked file that reads otherwise did not panic, and the proof returned %v", proveError)
}
