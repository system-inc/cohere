package release

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/build_inputs"
)

// None of these fetch a toolchain or reach the network. The generator itself is proven in internal/go_engine; what
// is proven here is what the release hands it and what the release does with what it writes.

// Every shipped target carries a table but windows/arm64, which has no runner to verify one.
func TestEveryTargetButWindowsOnArmCarriesAStandardLibraryTable(test *testing.T) {
	test.Parallel()

	windowsOnArm := Target{GoOperatingSystem: "windows", GoArchitecture: "arm64"}
	if !slices.Contains(Targets, windowsOnArm) {
		test.Fatalf("%s is no longer a target, so the exclusion of it names nothing", windowsOnArm)
	}
	for _, target := range Targets {
		want := target != windowsOnArm
		if carriesStandardLibraryTable(target) != want {
			test.Errorf("%s carries a table: %v, want %v", target, carriesStandardLibraryTable(target), want)
		}
	}
}

// A release of windows/arm64 alone generates nothing and builds no host binary, which this would fail to do from a
// module that is not there.
func TestATargetWithoutATableGetsNoOverlay(test *testing.T) {
	test.Parallel()

	overlays, generateError := generateStandardLibraryTables(
		Options{ModuleDirectory: filepath.Join(test.TempDir(), "no-module")},
		[]Target{{GoOperatingSystem: "windows", GoArchitecture: "arm64"}},
		"",
		test.TempDir(),
	)
	if generateError != nil {
		test.Fatalf("a target with no table started a generation: %v", generateError)
	}
	if len(overlays) != 0 {
		test.Fatalf("a target with no table got an overlay: %v", overlays)
	}
}

// The overlay's key is the module's own table.bin, absolute, which must exist, so a moved or renamed table fails
// here rather than shipping a binary whose overlay replaced nothing.
func TestTheOverlayReplacesTheModulesTableWithTheGeneratedOne(test *testing.T) {
	test.Parallel()

	generated := filepath.Join(test.TempDir(), "darwin-arm64.table.bin")
	contents, overlayError := standardLibraryOverlay(filepath.Join("..", "..", ".."), generated)
	if overlayError != nil {
		test.Fatal(overlayError)
	}
	var overlay struct {
		Replace map[string]string
	}
	if decodeError := json.Unmarshal(contents, &overlay); decodeError != nil {
		test.Fatalf("the overlay is not JSON: %v\n%s", decodeError, contents)
	}
	if len(overlay.Replace) != 1 {
		test.Fatalf("the overlay replaces %d files, want table.bin alone: %v", len(overlay.Replace), overlay.Replace)
	}
	for key, value := range overlay.Replace {
		if !filepath.IsAbs(key) || !strings.HasSuffix(key, standardLibraryTableModulePath) {
			test.Errorf("the overlay replaces %s, want the module's %s, absolute", key, standardLibraryTableModulePath)
		}
		if _, statError := os.Stat(key); statError != nil {
			test.Errorf("the overlay replaces %s, which is not in the module: %v", key, statError)
		}
		if value != generated {
			test.Errorf("the overlay replaces table.bin with %s, want %s", value, generated)
		}
	}
}

// The command line the release runs for one target's table, as the verb in internal/go_engine takes it.
func TestTheGeneratorCommandLine(test *testing.T) {
	test.Parallel()

	command := standardLibraryTableCommand(
		"/scratch/host/cohere",
		Target{GoOperatingSystem: "linux", GoArchitecture: "arm64"},
		"/scratch/cohere-linux-arm64.table.bin",
		"/scratch",
	)
	want := []string{
		"/scratch/host/cohere", "go-engine", "standard-library-table",
		"--out", "/scratch/cohere-linux-arm64.table.bin",
		"--target", "linux/arm64",
		"--versions", "go1.27.0,go1.27.1,go1.27.2,go1.27.3",
		"--cgo", "1,0",
	}
	if !slices.Equal(command.Args, want) {
		test.Errorf("the generator runs\n  %q\nwant\n  %q", command.Args, want)
	}
	if command.Dir != "/scratch" {
		test.Errorf("the generator runs in %q, want the scratch, outside the module", command.Dir)
	}
}

func TestTheReleaseStampIsTheVersionTheCommitAndTheToolchain(test *testing.T) {
	test.Parallel()

	stamp, stampError := standardLibraryReleaseStamp("0.3.1", "0123456789abcdef0123456789abcdef01234567", "go1.27.3")
	if stampError != nil {
		test.Fatal(stampError)
	}
	if want := "0.3.1 0123456789abcdef0123456789abcdef01234567 go1.27.3"; stamp != want {
		test.Errorf("the stamp is %q, want %q", stamp, want)
	}
	for _, refused := range [][3]string{
		{"", "abc", "go1.27.3"},
		{"0.3.1", "", "go1.27.3"},
		{"0.3.1", "abc", ""},
		{"0.3.1", "abc", "go1.27.3'"},
	} {
		if _, refusedError := standardLibraryReleaseStamp(refused[0], refused[1], refused[2]); refusedError == nil {
			test.Errorf("a stamp of %q was accepted", refused)
		}
	}
}

// The stamp goes into the same flags as the version, quoted once.
func TestTheLinkerFlagsCarryTheStandardLibraryStamp(test *testing.T) {
	test.Parallel()

	flags := releaseLinkerFlags("0.3.1", compilerPin{Commit: "c", Upstream: "o/n"}, "go1.27.3", "0.3.1 abc go1.27.3")
	want := "-X 'github.com/system-inc/cohere/internal/go_engine/standard_library_table.ReleaseStamp=" +
		"0.3.1 abc go1.27.3'"
	if strings.Count(flags, want) != 1 {
		test.Errorf("the linker flags %q do not carry %q once", flags, want)
	}
}

func TestGoBuildCommandPassesTheOverlayOnlyWhenGiven(test *testing.T) {
	test.Parallel()

	target := Target{GoOperatingSystem: "linux", GoArchitecture: "amd64"}
	with := GoBuildCommand("/module", target, "-s", "/out/cohere", "/scratch/overlay.json").Args
	index := slices.Index(with, "-overlay=/scratch/overlay.json")
	if index < 0 || index > slices.Index(with, "./command/cohere") {
		test.Errorf("the build with an overlay runs %q, want -overlay before the package", with)
	}
	without := GoBuildCommand("/module", target, "-s", "/out/cohere", "").Args
	for _, argument := range without {
		if strings.HasPrefix(argument, "-overlay") {
			test.Errorf("the build with no overlay runs %q", without)
		}
	}
}

// A build through the overlay embeds the generated table and the stamp, and leaves the committed file as it was. The
// fixture stands a module where cohere's is, with the table's package at its real path, and builds it with the
// release's own flags and command. Its directory is under the test's temporary one, which on macOS is reached
// through a symbolic link, so it also proves the overlay matches where go sees the module.
func TestABuildThroughTheOverlayEmbedsTheGeneratedTableAndTheStamp(test *testing.T) {
	test.Parallel()

	if testing.Short() {
		test.Skip("builds a fixture twice")
	}

	module := build_inputs.OutsideModule(test, test.TempDir())
	writeFile(test, filepath.Join(module, "go.mod"), "module github.com/system-inc/cohere\n\ngo 1.21\n")
	tablePackage := filepath.Join(module, filepath.Dir(standardLibraryTableModulePath))
	writeFile(test, filepath.Join(tablePackage, "table.go"), "package standard_library_table\n\n"+
		"import _ \"embed\"\n\n//go:embed table.bin\nvar Contents []byte\n\nvar ReleaseStamp = \"\"\n")
	committed := filepath.Join(module, standardLibraryTableModulePath)
	writeFile(test, committed, "committed")
	writeFile(test, filepath.Join(module, "command", "cohere", "main.go"), "package main\n\n"+
		"import (\n\t\"fmt\"\n\n\t\"github.com/system-inc/cohere/internal/go_engine/standard_library_table\"\n)\n\n"+
		"func main() {\n\tfmt.Printf(\"%s|%s\", standard_library_table.Contents, "+
		"standard_library_table.ReleaseStamp)\n}\n")

	scratch := test.TempDir()
	generated := filepath.Join(scratch, "table.bin")
	writeFile(test, generated, "generated")
	overlay, overlayError := standardLibraryOverlay(module, generated)
	if overlayError != nil {
		test.Fatal(overlayError)
	}
	overlayPath := filepath.Join(scratch, "overlay.json")
	writeFile(test, overlayPath, string(overlay))

	stamp := "0.3.1 0123456789abcdef0123456789abcdef01234567 go1.27.3"
	flags := releaseLinkerFlags("0.3.1", compilerPin{Commit: "c", Upstream: "o/n"}, "go1.27.3", stamp)
	host := Target{GoOperatingSystem: runtime.GOOS, GoArchitecture: runtime.GOARCH}

	run := func(overlayPath string) string {
		test.Helper()
		binary := filepath.Join(test.TempDir(), host.BinaryFileName())
		build := GoBuildCommand(module, host, flags, binary, overlayPath)
		if output, buildError := build.CombinedOutput(); buildError != nil {
			test.Fatalf("building the fixture: %v\n%s", buildError, output)
		}
		output, runError := exec.Command(binary).Output()
		if runError != nil {
			test.Fatalf("running the fixture: %v", runError)
		}
		return string(output)
	}

	if got, want := run(overlayPath), "generated|"+stamp; got != want {
		test.Errorf("the build through the overlay printed %q, want %q", got, want)
	}
	// The control: with no overlay the same fixture embeds the committed file, so the line above tells them apart.
	if got, want := run(""), "committed|"+stamp; got != want {
		test.Errorf("the build with no overlay printed %q, want %q", got, want)
	}
	if contents, readError := os.ReadFile(committed); readError != nil || string(contents) != "committed" {
		test.Errorf("the committed table.bin reads %q after the builds (%v), want it untouched", contents, readError)
	}
}

// Every generated table's size is written to the release's progress, by target, so the next growth past Kirk's
// budget is seen where it happens (#pcv6eff); a table that cannot be read fails the release.
func TestEveryTablesSizeIsReported(test *testing.T) {
	test.Parallel()

	tablePath := filepath.Join(test.TempDir(), "darwin-arm64.table.bin")
	if writeError := os.WriteFile(tablePath, make([]byte, 2973677), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	var progress strings.Builder
	target := Target{GoOperatingSystem: "darwin", GoArchitecture: "arm64"}
	if reportError := reportTableSize(&progress, target, tablePath); reportError != nil {
		test.Fatal(reportError)
	}
	want := "the standard library's table for " + target.String() + " is 2973677 bytes\n"
	if progress.String() != want {
		test.Errorf("the progress reads %q, want %q", progress.String(), want)
	}
	if reportError := reportTableSize(&progress, target, filepath.Join(test.TempDir(), "missing")); reportError == nil {
		test.Error("a table that cannot be read was reported, want the release failed")
	}
}
