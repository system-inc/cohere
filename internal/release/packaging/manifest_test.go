package release

import (
	"encoding/json"
	"strings"
	"testing"
)

// The defect these guard is quiet by construction. A manifest whose `os` or `cpu` disagrees with
// the binary beside it installs on the wrong machine or on none, and either way what a user sees is
// "no binary for this platform" — the same message a genuinely unsupported platform produces. So
// the fields are asserted against the same target that builds the binary, which is the only way the
// two cannot drift.

func TestPlatformManifestDeclaresTheOperatingSystemAndArchitectureNpmResolvesBy(test *testing.T) {
	test.Parallel()

	cases := []struct {
		target                  Target
		expectedName            string
		expectedOperatingSystem string
		expectedArchitecture    string
	}{
		{Target{"darwin", "arm64"}, "@system-inc/cohere-darwin-arm64", "darwin", "arm64"},
		{Target{"darwin", "amd64"}, "@system-inc/cohere-darwin-x64", "darwin", "x64"},
		{Target{"linux", "amd64"}, "@system-inc/cohere-linux-x64", "linux", "x64"},
		{Target{"windows", "amd64"}, "@system-inc/cohere-win32-x64", "win32", "x64"},
		{Target{"windows", "arm64"}, "@system-inc/cohere-win32-arm64", "win32", "arm64"},
	}

	for _, testCase := range cases {
		encoded := mustPlatformManifest(test, testCase.target, "1.2.3")
		manifest := decodeManifest(test, encoded)

		if manifest["name"] != testCase.expectedName {
			test.Errorf("%s published as %v, wanted %s", testCase.target, manifest["name"], testCase.expectedName)
		}
		if got := singleString(test, encoded, "os"); got != testCase.expectedOperatingSystem {
			test.Errorf(
				"%s declared os %q, wanted %q — npm would install it on the wrong machine",
				testCase.target,
				got,
				testCase.expectedOperatingSystem,
			)
		}
		if got := singleString(test, encoded, "cpu"); got != testCase.expectedArchitecture {
			test.Errorf(
				"%s declared cpu %q, wanted %q — npm would install it on the wrong machine",
				testCase.target,
				got,
				testCase.expectedArchitecture,
			)
		}
	}
}

func TestPlatformManifestMatchesWhatTheLauncherLooksFor(test *testing.T) {
	test.Parallel()

	// The launcher builds the package name from Node's process.platform and process.arch, which are
	// npm's spelling. This asserts the published name is the one it will ask for, for every target
	// we ship. A mismatch here is a package that publishes, installs, and is never found.
	for _, target := range Targets {
		encoded := mustPlatformManifest(test, target, "1.2.3")
		manifest := decodeManifest(test, encoded)

		expected := PlatformPackagePrefix +
			singleString(test, encoded, "os") +
			"-" +
			singleString(test, encoded, "cpu")
		if manifest["name"] != expected {
			test.Errorf(
				"%s publishes as %v, but the launcher on that machine asks for %s",
				target, manifest["name"], expected,
			)
		}
	}
}

func TestPlatformManifestClaimsNoCommand(test *testing.T) {
	test.Parallel()

	// Only the dispatcher owns the `cohere` name. A platform package with a `bin` field would race
	// it for the same link in node_modules/.bin, and which one wins depends on install order.
	for _, target := range Targets {
		manifest := decodeManifest(test, mustPlatformManifest(test, target, "1.2.3"))
		if _, claimed := manifest["bin"]; claimed {
			test.Errorf(
				"%s declares a bin field, which would fight the dispatcher for node_modules/.bin/cohere",
				target,
			)
		}
	}
}

func TestDispatcherDependsOnEveryPlatformExactly(test *testing.T) {
	test.Parallel()

	manifest := decodeManifest(test, mustDispatcherManifest(test, "1.2.3"))

	optional, ok := manifest["optionalDependencies"].(map[string]any)
	if !ok {
		test.Fatalf("the dispatcher declares no optionalDependencies, so no binary would ever install")
	}

	for _, target := range Targets {
		pinned, listed := optional[target.PackageName()]
		if !listed {
			test.Errorf("%s is built but never depended on, so it would never install", target.PackageName())
			continue
		}
		// A range would let a resolver pair this dispatcher with a binary built from other rules.
		if pinned != "1.2.3" {
			test.Errorf("%s pinned as %v rather than the exact version", target.PackageName(), pinned)
		}
	}

	if len(optional) != len(Targets) {
		test.Errorf("the dispatcher depends on %d platform packages but %d are built", len(optional), len(Targets))
	}
}

// The published name is a contract with code outside this repository, so it is pinned as a literal
// rather than compared against the constant that produces it.
//
// Structure tells a consumer whose gate found no binary to run `pnpm add -D @system-inc/cohere`. A
// constant renamed here alone would still pass every other test, publish cleanly, and leave that
// instruction naming a package that does not exist, which is how this name came to be decided: the
// error text and the registry disagreed, and the error text is what people read.
func TestDispatcherPublishesUnderTheNameStructureTellsPeopleToInstall(test *testing.T) {
	test.Parallel()

	manifest := decodeManifest(test, mustDispatcherManifest(test, "1.2.3"))
	if manifest["name"] != "@system-inc/cohere" {
		test.Errorf(
			"the dispatcher publishes as %v, but Structure tells people to install @system-inc/cohere",
			manifest["name"],
		)
	}

	// Every platform package lives in the same scope, so one scope's access settings govern the
	// whole release and no binary publishes somewhere the dispatcher's owner does not control.
	for _, target := range Targets {
		if !strings.HasPrefix(target.PackageName(), PackageScope+"/") {
			test.Errorf("%s publishes outside %s", target.PackageName(), PackageScope)
		}
	}
}

// Both command names must be installed, and both must reach the same launcher.
//
// Two entries rather than one because `v` is what gets typed on the loop and `cohere` is what reads
// in a script, and because a single letter is short enough to collide with something already on a
// machine. A consumer who hits that keeps a working command instead of a broken install.
//
// Asserted per name rather than by counting. An earlier version of this test checked only `cohere`,
// which would have passed unchanged if `v` had silently stopped being declared: the guarantee it
// stated was true and the guarantee anyone reading it believed was not.
func TestDispatcherOwnsBothCommandNames(test *testing.T) {
	test.Parallel()

	manifest := decodeManifest(test, mustDispatcherManifest(test, "1.2.3"))

	binaries, ok := manifest["bin"].(map[string]any)
	if !ok {
		test.Fatalf("the dispatcher declares no bin, so installing it would create no command at all")
	}

	for _, name := range []string{ShortCommandName, FullCommandName} {
		if binaries[name] != launcherRelativePath {
			test.Errorf("the %q command points at %v, wanted %s", name, binaries[name], launcherRelativePath)
		}
	}

	if len(binaries) != 2 {
		// A third name would be a command nobody documented, installed on every consumer's machine.
		test.Errorf("the dispatcher declares %d commands, wanted exactly the two named ones: %v",
			len(binaries), binaries)
	}
}

func TestLauncherHoldsTheNoFallbackRule(test *testing.T) {
	test.Parallel()

	// The launcher is generated as a string, so a compiler cannot check it. These assert the
	// substance rather than the wording: that it exits non-zero on a missing binary, and that no
	// bare command name appears as something it could run instead.
	launcher := DispatcherLauncher()

	for _, placeholder := range []string{
		"__PLATFORM_PACKAGE_PREFIX__",
		"__PACKAGE_SCOPE__",
		"__OVERRIDE_VARIABLE__",
		"__CHECKSUMS_FILE_NAME__",
	} {
		if strings.Contains(launcher, placeholder) {
			test.Fatalf("the launcher shipped with %s unreplaced, so it would resolve or check nothing", placeholder)
		}
	}
	if !strings.Contains(launcher, "'"+PlatformPackagePrefix+"'") {
		test.Errorf(
			"the launcher never names the %s prefix, so it cannot find a platform package",
			PlatformPackagePrefix,
		)
	}
	if !strings.Contains(launcher, BinaryOverrideVariable) {
		test.Errorf("the launcher never reads %s, so a local build cannot be pointed at", BinaryOverrideVariable)
	}
	if !strings.Contains(launcher, "process.exit(1)") {
		test.Errorf("the launcher has no non-zero exit, so a missing binary could report success")
	}

	// The historical defect, spelled out: resolving to a bare name and letting the shell find
	// something. If this ever appears, the launcher can spawn a stranger.
	for _, forbidden := range []string{"spawnSync('cohere'", `spawnSync("cohere"`, "PATH"} {
		if strings.Contains(launcher, forbidden) {
			test.Errorf("the launcher contains %q, which is the fallback-to-PATH bug this rule forbids", forbidden)
		}
	}
}

func TestLauncherDoesNotReportSuccessForASignalledRun(test *testing.T) {
	test.Parallel()

	// spawnSync reports a null status when the child is killed by a signal, and null is falsy — so
	// the obvious implementation exits zero for a run that was terminated. That is a green gate over
	// a killed process.
	launcher := DispatcherLauncher()
	if !strings.Contains(launcher, "result.status === null") {
		test.Errorf("the launcher never checks for a null status, so a signalled run would exit zero")
	}
}

func mustPlatformManifest(test *testing.T, target Target, version string) []byte {
	test.Helper()
	encoded, platformManifestError := PlatformManifest(target, version)
	if platformManifestError != nil {
		test.Fatal(platformManifestError)
	}
	return encoded
}

func mustDispatcherManifest(test *testing.T, version string) []byte {
	test.Helper()
	encoded, dispatcherManifestError := DispatcherManifest(version)
	if dispatcherManifestError != nil {
		test.Fatal(dispatcherManifestError)
	}
	return encoded
}

// decodeManifest parses generated JSON, failing the test if it is not valid.
func decodeManifest(test *testing.T, encoded []byte) map[string]any {
	test.Helper()
	manifest := map[string]any{}
	if decodeError := json.Unmarshal(encoded, &manifest); decodeError != nil {
		test.Fatalf("the generated manifest is not valid JSON, so npm would reject the package: %v", decodeError)
	}
	return manifest
}

// singleString reads a field of a generated manifest that should be an array holding exactly one string.
func singleString(test *testing.T, encoded []byte, field string) string {
	test.Helper()
	fields := map[string]json.RawMessage{}
	if decodeError := json.Unmarshal(encoded, &fields); decodeError != nil {
		test.Fatalf("the generated manifest is not a JSON object: %v", decodeError)
	}
	var values []string
	if decodeError := json.Unmarshal(fields[field], &values); decodeError != nil || len(values) != 1 {
		test.Fatalf("expected %s to be a one-element array of strings, got %s", field, fields[field])
	}
	return values[0]
}
