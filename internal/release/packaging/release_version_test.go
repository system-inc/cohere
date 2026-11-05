package release

import (
	"strings"
	"testing"
)

// TestReleaseVersionFloor holds both halves of the version rule: semver, and nothing below 0.1.0.
// The accepted cases matter as much as the refused ones, because a gate that refused everything
// would pass every refusal below.
func TestReleaseVersionFloor(test *testing.T) {
	test.Parallel()

	for _, version := range []string{"0.1.0", "0.1.0-rc.1", "0.3.1", "0.12.0", "1.0.0", "1.0.0-rc.1", "1.2.3", "12.0.0", "2.10.31-beta.2"} {
		if requireReleaseVersionError := requireReleaseVersion(version); requireReleaseVersionError != nil {
			test.Errorf("%s was refused: %v", version, requireReleaseVersionError)
		}
	}

	refused := map[string]string{
		"0.0.0":       "below 0.1.0",
		"0.0.9":       "below 0.1.0",
		"0.0.1-rc.1":  "below 0.1.0",
		"1.0":         "not a semantic version",
		"v1.0.0":      "not a semantic version",
		"01.0.0":      "not a semantic version",
		"1.0.0+build": "not a semantic version",
		"1.0.0-":      "not a semantic version",
		" 1.0.0":      "not a semantic version",
	}
	for version, reason := range refused {
		requireReleaseVersionError := requireReleaseVersion(version)
		if requireReleaseVersionError == nil {
			test.Errorf("%q was accepted", version)
			continue
		}
		if !strings.Contains(requireReleaseVersionError.Error(), reason) {
			test.Errorf("%q was refused for the wrong reason: %v", version, requireReleaseVersionError)
		}
	}
}

// TestBuildRefusesAVersionBelowTheFloor proves Build calls the check, which the table above cannot: it
// drives requireReleaseVersion directly and would pass with the call deleted from Build.
func TestBuildRefusesAVersionBelowTheFloor(test *testing.T) {
	test.Parallel()

	_, buildError := Build(Options{Version: "0.0.9", ModuleDirectory: test.TempDir(), OutputDirectory: test.TempDir()})
	if buildError == nil {
		test.Fatal("Build accepted 0.0.9")
	}
	if !strings.Contains(buildError.Error(), "start at 0.1.0") {
		test.Fatalf("Build failed, but not on the version: %v", buildError)
	}
}
