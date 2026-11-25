package release

import (
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/testing/build_inputs"
)

// A release is profile-guided and built at arm64 v8.4 on darwin (#r6sqd2c), and nothing fails when either quietly
// stops: the build succeeds, the packages stage, the binaries run about 2% slower. So each is asserted where the
// compile is defined, and once on a binary Go actually built.

func TestOnlyTheDarwinArm64BuildAsksForGoArm64V84(test *testing.T) {
	test.Parallel()
	requireGoArm64Levels(test)
}

// An arm64 level in the caller's environment would otherwise reach every target, and a Linux or Windows arm64
// binary built at v8.4 dies on an illegal instruction on the v8.0 cores still in service.
// Not parallel: it sets GOARM64 in the process's environment, which every build command in the package reads.
func TestAnInheritedGoArm64ReachesNoReleaseBuild(test *testing.T) {
	test.Setenv("GOARM64", "v9.0")
	requireGoArm64Levels(test)
}

// requireGoArm64Levels holds every target's build command to the GOARM64 entries it carries: v8.4 alone on
// darwin/arm64, and none on any other target.
func requireGoArm64Levels(test *testing.T) {
	test.Helper()
	sawDarwinArm64 := false
	for _, target := range Targets {
		command := GoBuildCommand(test.TempDir(), target, "-s -w", Profile{}, "cohere")
		levels := []string{}
		for _, variable := range command.Env {
			if level, isLevel := strings.CutPrefix(variable, "GOARM64="); isLevel {
				levels = append(levels, level)
			}
		}
		expected := []string{}
		if target.GoOperatingSystem == "darwin" && target.GoArchitecture == "arm64" {
			expected = []string{"v8.4"}
			sawDarwinArm64 = true
		}
		if !slices.Equal(levels, expected) {
			test.Errorf("the %s build carries GOARM64 %q, and it should carry %q", target, levels, expected)
		}
	}
	if !sawDarwinArm64 {
		test.Fatalf("no darwin/arm64 among the release's targets, so the v8.4 build was never checked")
	}
}

func TestTheBuildPassesTheProfileWhenTheModuleHoldsOne(test *testing.T) {
	test.Parallel()

	module := test.TempDir()
	writeFile(test, filepath.Join(module, "command", "cohere", "default.pgo"), "a profile")
	profile, readProfileError := ReadProfile(module)
	if readProfileError != nil {
		test.Fatal(readProfileError)
	}
	digest := sha256.Sum256([]byte("a profile"))
	expected := Profile{Path: ProfilePath, Digest: hex.EncodeToString(digest[:])}
	if profile != expected {
		test.Fatalf("the module's profile read as %+v, and it is %+v", profile, expected)
	}

	for _, target := range Targets {
		flags := profileFlags(GoBuildCommand(module, target, "-s -w", profile, "cohere").Args)
		if !slices.Equal(flags, []string{"-pgo=command/cohere/default.pgo"}) {
			test.Errorf("the %s build passes %q, and it should pass the profile by its path alone", target, flags)
		}
	}
	stamp := "-X github.com/system-inc/cohere/internal/release/packaging.profileDigest=" + expected.Digest
	if flags := releaseLinkerFlags("0.2.0", compilerPin{}, "go1.27.1", profile); !strings.Contains(flags, stamp) {
		test.Errorf("the linker flags %q don't stamp the profile's digest, so --version can't say which it is", flags)
	}
	if described := profile.Describe(); !strings.Contains(described, ShortDigest(expected.Digest)) {
		test.Errorf("the summary's %q doesn't name the profile by the digest --version prints", described)
	}
}

func TestTheBuildTurnsTheProfileOffAndSaysSoWhenTheModuleHoldsNone(test *testing.T) {
	test.Parallel()

	profile, readProfileError := ReadProfile(test.TempDir())
	if readProfileError != nil {
		test.Fatal(readProfileError)
	}
	if profile != (Profile{}) {
		test.Fatalf("a module with no profile read as %+v", profile)
	}
	// Off by name, so an inherited `GOFLAGS` can't put a profile in that the summary and --version don't name.
	for _, target := range Targets {
		flags := profileFlags(GoBuildCommand(test.TempDir(), target, "-s -w", profile, "cohere").Args)
		if !slices.Equal(flags, []string{"-pgo=off"}) {
			test.Errorf("the %s build with no profile passes %q, and it should pass -pgo=off", target, flags)
		}
	}
	flags := releaseLinkerFlags("0.2.0", compilerPin{}, "go1.27.1", profile)
	if strings.Contains(flags, "profileDigest") {
		test.Errorf("the linker flags %q stamp a digest for a profile the build doesn't carry", flags)
	}
	if described := profile.Describe(); !strings.Contains(described, "not profile-guided") {
		test.Errorf("a build with no profile says %q, which doesn't say it isn't profile-guided", described)
	}
}

// The tests above read the command, which agrees with whatever this package believes Go does with it. This one
// builds a program through it and reads back what Go wrote into the binary, through the same reader `--version`
// uses, so a flag Go ignored or a setting it names differently fails here.
func TestTheProfileAndTheArm64LevelReachTheBinaryGoBuilds(test *testing.T) {
	test.Parallel()
	if testing.Short() {
		test.Skip("builds a program twice")
	}

	darwinArm64 := Target{GoOperatingSystem: "darwin", GoArchitecture: "arm64"}
	for _, withProfile := range []bool{true, false} {
		module := test.TempDir()
		writeFile(test, filepath.Join(module, "go.mod"), "module profilecheck\n\ngo 1.27\n")
		writeFile(test, filepath.Join(module, "command", "cohere", "main.go"), "package main\n\nfunc main() {}\n")
		expected := ""
		if withProfile {
			// Go takes an empty file as a profile with no samples, which is enough to show it was passed.
			writeFile(test, filepath.Join(module, "command", "cohere", "default.pgo"), "")
			expected = "default.pgo"
		}
		profile, readProfileError := ReadProfile(module)
		if readProfileError != nil {
			test.Fatal(readProfileError)
		}
		binary := filepath.Join(test.TempDir(), "cohere")
		command := GoBuildCommand(build_inputs.OutsideModule(test, module), darwinArm64, "-s -w", profile, binary)
		if output, outputError := command.CombinedOutput(); outputError != nil {
			test.Fatalf("building with the profile %t: %v\n%s", withProfile, outputError, output)
		}
		information, readFileError := buildinfo.ReadFile(binary)
		if readFileError != nil {
			test.Fatal(readFileError)
		}
		if read := profileFromSettings(information.Settings); read != expected {
			test.Errorf("a build with the profile %t reads its profile as %q, and it is %q", withProfile, read,
				expected)
		}
		level := ""
		for _, setting := range information.Settings {
			if setting.Key == "GOARM64" {
				level = setting.Value
			}
		}
		if level != "v8.4" {
			test.Errorf("the darwin/arm64 binary was built at GOARM64 %q, and it should be v8.4", level)
		}
	}
}

// profileFlags are the `-pgo` flags among a command's arguments.
func profileFlags(arguments []string) []string {
	flags := []string{}
	for _, argument := range arguments {
		if strings.HasPrefix(argument, "-pgo") {
			flags = append(flags, argument)
		}
	}
	return flags
}
