package release

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/executable_testing"
)

// Every check here is shown failing on a tampered binary as well as passing on an intact one, because a
// verification that has never once refused anything has not been shown to look at the bytes. The tamper
// is one flipped byte, the smallest change there is, and in the launcher's fixture it is a flip that
// leaves the script runnable: if the launcher ran it anyway, its output would say so.

// placeholderChecksums stands in for a release's sums where a test builds the dispatcher for some other
// reason, since a dispatcher with none is refused.
var placeholderChecksums = []byte(strings.Repeat("0", 64) + "  cohere-linux-x64/bin/cohere\n")

// fixtureScript is the stand-in platform binary. The tamper flips the case of its first letter, which
// keeps it a working script, so a launcher that ran it after all would print "Fixture ran".
const fixtureScript = "#!/bin/sh\necho \"fixture ran: $*\"\n"

func TestChecksumsCheckWithShasumAndFailOnAFlippedByte(test *testing.T) {
	test.Parallel()

	// shasum is what the README tells a reader to run, so the format is proven by shasum rather than by a
	// parser of ours, which would agree with whatever this package wrote.
	shasum, lookPathError := exec.LookPath("shasum")
	if lookPathError != nil {
		test.Fatalf("shasum is not installed, and it is the checker these sums are written for: %v", lookPathError)
	}

	output := test.TempDir()
	stageFixturePlatform(test, output, Target{GoOperatingSystem: "darwin", GoArchitecture: "arm64"})
	stageFixturePlatform(test, output, Target{GoOperatingSystem: "linux", GoArchitecture: "amd64"})

	checksums, checksumsError := Checksums(output, []string{"cohere-linux-x64", "cohere-darwin-arm64"})
	if checksumsError != nil {
		test.Fatal(checksumsError)
	}
	writeFile(test, filepath.Join(output, ChecksumsFileName), string(checksums))

	lines := strings.Split(strings.TrimSuffix(string(checksums), "\n"), "\n")
	wanted := []string{
		"cohere-darwin-arm64/bin/cohere",
		"cohere-darwin-arm64/bin/cohere-swift",
		"cohere-linux-x64/bin/cohere",
		"cohere-linux-x64/bin/cohere-swift",
	}
	if len(lines) != len(wanted) {
		test.Fatalf("SHA256SUMS has %d lines, wanted one per file in each bin/:\n%s", len(lines), checksums)
	}
	for index, line := range lines {
		if !strings.HasSuffix(line, "  "+wanted[index]) {
			test.Errorf("line %d is %q, wanted the file %s, sorted by path", index, line, wanted[index])
		}
	}

	check := exec.Command(shasum, "-a", "256", "-c", ChecksumsFileName)
	check.Dir = output
	if result, outputError := check.CombinedOutput(); outputError != nil {
		test.Fatalf("shasum refused the sums of the files they were taken from: %v\n%s", outputError, result)
	}

	flipByte(test, filepath.Join(output, "cohere-linux-x64", "bin", "cohere"))
	check = exec.Command(shasum, "-a", "256", "-c", ChecksumsFileName)
	check.Dir = output
	result, outputError := check.CombinedOutput()
	if outputError == nil {
		test.Fatalf("shasum passed a binary with a flipped byte:\n%s", result)
	}
	if !strings.Contains(string(result), "cohere-linux-x64/bin/cohere: FAILED") {
		test.Fatalf("shasum failed, but not by naming the flipped binary:\n%s", result)
	}
}

func TestChecksumsRefuseAPackageWithNothingToCheck(test *testing.T) {
	test.Parallel()

	output := test.TempDir()
	if createDirectoryError := os.MkdirAll(
		filepath.Join(output, "cohere-linux-x64", "bin"),
		0o755,
	); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	if checksums, checksumsError := Checksums(output, []string{"cohere-linux-x64"}); checksumsError == nil {
		test.Fatalf("an empty bin/ produced sums %q, so a release with no binary would look checked", checksums)
	}

	if checksums, checksumsError := Checksums(output, []string{"cohere-darwin-arm64"}); checksumsError == nil {
		test.Fatalf("a package that was never staged produced sums %q", checksums)
	}
}

func TestChecksumsRefuseALinkInBin(test *testing.T) {
	test.Parallel()

	if runtime.GOOS == "windows" {
		test.Skip("creating a symbolic link needs a privilege Windows does not grant by default")
	}
	output := test.TempDir()
	stageFixturePlatform(test, output, Target{GoOperatingSystem: "linux", GoArchitecture: "amd64"})
	if symlinkError := os.Symlink(
		"/bin/sh",
		filepath.Join(output, "cohere-linux-x64", "bin", "shell"),
	); symlinkError != nil {
		test.Fatal(symlinkError)
	}
	if checksums, checksumsError := Checksums(output, []string{"cohere-linux-x64"}); checksumsError == nil {
		test.Fatalf("a link in bin/ was hashed as though it were the package's own file:\n%s", checksums)
	}
}

func TestDispatcherPublishesItsChecksums(test *testing.T) {
	test.Parallel()

	// Staged and left out of `files` is the failure this catches: npm publish would drop SHA256SUMS, and
	// every install would then refuse to run, from a package that looked complete in dist/.
	manifest := decodeManifest(test, mustDispatcherManifest(test, "1.2.3"))
	files, _ := manifest["files"].([]any)
	if !slices.Contains(files, any(ChecksumsFileName)) {
		test.Fatalf("the dispatcher's files are %v, so npm publish would leave %s out", files, ChecksumsFileName)
	}

	module, absolutePathError := filepath.Abs(filepath.Join("..", "..", ".."))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	staged, buildDispatcherPackageError := buildDispatcherPackage(
		Options{ModuleDirectory: module, OutputDirectory: test.TempDir(), Version: "1.0.0"},
		placeholderChecksums,
	)
	if buildDispatcherPackageError != nil {
		test.Fatal(buildDispatcherPackageError)
	}
	shipped, readError := os.ReadFile(filepath.Join(staged.Directory, ChecksumsFileName))
	if readError != nil {
		test.Fatalf("the dispatcher package does not hold %s: %v", ChecksumsFileName, readError)
	}
	if !bytes.Equal(shipped, placeholderChecksums) {
		test.Fatalf("the dispatcher holds %q, not the sums it was given", shipped)
	}

	if _, buildDispatcherPackageError := buildDispatcherPackage(
		Options{ModuleDirectory: module, OutputDirectory: test.TempDir(), Version: "1.0.0"},
		nil,
	); buildDispatcherPackageError == nil {
		test.Fatalf("a dispatcher was staged with no checksums, and its launcher would run nothing anywhere")
	}
}

// The launcher, run by Node the way npm's shim runs it, against a release staged into a node_modules
// layout by the same functions the release uses.

func TestLauncherRunsAnIntactRelease(test *testing.T) {
	test.Parallel()

	installation := stageInstalledRelease(test)
	result := installation.run(test, "--version")
	result.requireRan(test, "--version")

	if _, statError := os.Stat(installation.verdictPath(test)); statError != nil {
		test.Fatalf(
			"a passing check left no remembered verdict, so every run would hash the binaries again: %v",
			statError,
		)
	}
}

func TestLauncherRefusesAFlippedByte(test *testing.T) {
	test.Parallel()

	installation := stageInstalledRelease(test)
	flipByte(test, installation.binary)

	result := installation.run(test, "--version")
	result.requireRefused(test, installation.binary, "does not match the checksum")
}

func TestLauncherRefusesATamperAfterAVerdictIsRemembered(test *testing.T) {
	test.Parallel()

	// The remembered verdict is the shortcut a tamper would hide behind: the binary passed once, then
	// changed. The change here keeps the size and puts the modification time back, so only the change
	// time and the bytes differ, which is everything a careful edit can leave behind.
	installation := stageInstalledRelease(test)
	installation.run(test, "--version").requireRan(test, "--version")

	before, statError := os.Stat(installation.binary)
	if statError != nil {
		test.Fatal(statError)
	}
	flipByte(test, installation.binary)
	if changeTimesError := os.Chtimes(
		installation.binary,
		before.ModTime(),
		before.ModTime(),
	); changeTimesError != nil {
		test.Fatal(changeTimesError)
	}
	after, statError := os.Stat(installation.binary)
	if statError != nil {
		test.Fatal(statError)
	}
	if !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		test.Fatalf("the tamper changed the modification time or the size, so this would not test the change time")
	}

	result := installation.run(test, "--version")
	result.requireRefused(test, installation.binary, "does not match the checksum")
}

func TestLauncherHonorsARememberedVerdictOnlyForTheChecksumItWasReachedOn(test *testing.T) {
	test.Parallel()

	// Two halves, so neither passes by accident. A new release names a new hash for a file whose bytes
	// have not moved, and the old verdict must not vouch for it. And a verdict that does match is
	// honored without hashing: shown by pointing both the sums and the verdict at a hash the file does
	// not have, which only a skipped hash would let run. That second half is what keeps the check off
	// every run's clock.
	installation := stageInstalledRelease(test)
	installation.run(test, "--version").requireRan(test, "--version")

	checksums, readError := os.ReadFile(installation.checksums)
	if readError != nil {
		test.Fatal(readError)
	}
	actual := sha256Hex([]byte(fixtureScript))
	other := sha256Hex([]byte("another release's binary"))
	if !bytes.Contains(checksums, []byte(actual)) {
		test.Fatalf("the staged sums do not hold the fixture's hash %s:\n%s", actual, checksums)
	}
	writeFile(test, installation.checksums, strings.ReplaceAll(string(checksums), actual, other))

	installation.run(test, "--version").requireRefused(test, installation.binary, "does not match the checksum")

	verdict, readError := os.ReadFile(installation.verdictPath(test))
	if readError != nil {
		test.Fatal(readError)
	}
	if !bytes.Contains(verdict, []byte(actual)) {
		test.Fatalf("the remembered verdict does not name the hash it was reached on:\n%s", verdict)
	}
	writeFile(test, installation.verdictPath(test), strings.ReplaceAll(string(verdict), actual, other))

	installation.run(test, "--version").requireRan(test, "--version")
}

func TestLauncherRefusesWithoutItsChecksums(test *testing.T) {
	test.Parallel()

	installation := stageInstalledRelease(test)
	if removeError := os.Remove(installation.checksums); removeError != nil {
		test.Fatal(removeError)
	}
	installation.run(test, "--version").requireRefused(test, installation.checksums, "is missing")
}

func TestLauncherRefusesChecksumsThatDoNotListThisPlatform(test *testing.T) {
	test.Parallel()

	// Every platform but the one running the test, so the sums are a release's in all but this platform. A fixed
	// list once named linux-x64 and refused to run there, testing nothing on Linux (#8ktz3ny).
	installation := stageInstalledRelease(test)
	var others strings.Builder
	for _, target := range Targets {
		if target.DirectoryName() != installation.directoryName {
			others.WriteString(strings.Repeat("0", 64) + "  " + target.DirectoryName() + "/bin/cohere\n")
		}
	}
	if others.Len() == 0 || strings.Contains(others.String(), installation.directoryName+"/") {
		test.Fatalf("the planted sums list %s or nothing at all, so this would test no platform they leave out",
			installation.directoryName)
	}
	writeFile(test, installation.checksums, others.String())
	installation.run(test, "--version").requireRefused(test, installation.checksums, "lists no binary for")
}

func TestLauncherRefusesWhenAShippedFileIsMissing(test *testing.T) {
	test.Parallel()

	// cohere-swift, which cohere runs on a Mac without the launcher in between, so it is checked here or
	// nowhere.
	installation := stageInstalledRelease(test)
	engine := filepath.Join(filepath.Dir(installation.binary), SwiftEngineFileName)
	if removeError := os.Remove(engine); removeError != nil {
		test.Fatal(removeError)
	}
	installation.run(test, "--version").requireRefused(test, engine, "is missing")
}

func TestLauncherRefusesATamperedEngine(test *testing.T) {
	test.Parallel()

	installation := stageInstalledRelease(test)
	engine := filepath.Join(filepath.Dir(installation.binary), SwiftEngineFileName)
	flipByte(test, engine)
	installation.run(test, "--version").requireRefused(test, engine, "does not match the checksum")
}

func TestLauncherRunsTheOverrideUnchecked(test *testing.T) {
	test.Parallel()

	// Someone who names a binary has said what to run, usually their own build, which no release sum
	// describes. The installed binary is tampered with too, so a launcher that checked it anyway refuses.
	installation := stageInstalledRelease(test)
	flipByte(test, installation.binary)

	override := filepath.Join(test.TempDir(), "cohere-local")
	writeFixtureExecutable(test, override)

	installation.run(test, "--version", BinaryOverrideVariable+"="+override).requireRan(test, "--version")
}

// installedRelease is a dispatcher and this machine's platform package, staged where npm installs them.
type installedRelease struct {
	root          string
	home          string
	launcher      string
	binary        string
	checksums     string
	directoryName string
}

// stageInstalledRelease stages a fixture release into node_modules/@system-inc, which the staging layout
// already is: one directory per package, named without the scope.
func stageInstalledRelease(test *testing.T) installedRelease {
	test.Helper()
	if runtime.GOOS == "windows" {
		test.Skip(
			"the fixture binary is a shell script; the Windows package is proven by check-platform-package.sh on Windows",
		)
	}
	if _, lookPathError := exec.LookPath("node"); lookPathError != nil {
		test.Fatalf(
			"node is not installed, and the launcher is a Node script, so it cannot be tested: %v",
			lookPathError,
		)
	}

	// Through every link, as Node resolves the launcher and the package, so the paths its messages name
	// are the paths this test holds (macOS's temporary directory is under a link).
	root, resolveSymlinksError := filepath.EvalSymlinks(test.TempDir())
	if resolveSymlinksError != nil {
		test.Fatal(resolveSymlinksError)
	}
	output := filepath.Join(root, "node_modules", PackageScope)
	target := Target{GoOperatingSystem: runtime.GOOS, GoArchitecture: runtime.GOARCH}
	stageFixturePlatform(test, output, target)

	checksums, checksumsError := Checksums(output, []string{target.DirectoryName()})
	if checksumsError != nil {
		test.Fatal(checksumsError)
	}
	module, absolutePathError := filepath.Abs(filepath.Join("..", "..", ".."))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	dispatcher, buildDispatcherPackageError := buildDispatcherPackage(
		Options{ModuleDirectory: module, OutputDirectory: output, Version: "1.0.0"},
		checksums,
	)
	if buildDispatcherPackageError != nil {
		test.Fatal(buildDispatcherPackageError)
	}

	return installedRelease{
		root:          root,
		home:          test.TempDir(),
		launcher:      filepath.Join(dispatcher.Directory, launcherRelativePath),
		binary:        filepath.Join(output, target.DirectoryName(), "bin", BinaryFileName(runtime.GOOS)),
		checksums:     filepath.Join(dispatcher.Directory, ChecksumsFileName),
		directoryName: target.DirectoryName(),
	}
}

// verdictPath is where the launcher remembers a passing check for this installation: the user cache
// under this test's home, keyed by the package directory as Node resolves it, through every link.
func (installation installedRelease) verdictPath(test *testing.T) string {
	test.Helper()
	packageDirectory, resolveSymlinksError := filepath.EvalSymlinks(filepath.Dir(filepath.Dir(installation.binary)))
	if resolveSymlinksError != nil {
		test.Fatal(resolveSymlinksError)
	}
	cache := filepath.Join(installation.home, ".cache")
	if runtime.GOOS == "darwin" {
		cache = filepath.Join(installation.home, "Library", "Caches")
	}
	return filepath.Join(cache, "cohere", "verified-binaries", sha256Hex([]byte(packageDirectory)))
}

// run runs the launcher with this installation's home as the user's, so its remembered verdicts are the
// test's own, and with no override unless one is given.
func (installation installedRelease) run(test *testing.T, argument string, environment ...string) launcherRun {
	test.Helper()
	command := exec.Command("node", installation.launcher, argument)
	command.Dir = installation.root
	for _, variable := range os.Environ() {
		name, _, _ := strings.Cut(variable, "=")
		if name == "HOME" || name == "XDG_CACHE_HOME" || name == BinaryOverrideVariable {
			continue
		}
		command.Env = append(command.Env, variable)
	}
	command.Env = append(command.Env, "HOME="+installation.home)
	command.Env = append(command.Env, environment...)

	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	command.Stdout = &standardOutput
	command.Stderr = &standardError
	runError := command.Run()
	exitCode := 0
	if exitError, isExit := runError.(*exec.ExitError); isExit {
		exitCode = exitError.ExitCode()
	} else if runError != nil {
		test.Fatalf("running the launcher: %v", runError)
	}
	return launcherRun{
		exitCode:       exitCode,
		standardOutput: standardOutput.String(),
		standardError:  standardError.String(),
	}
}

// stageFixturePlatform writes a platform package whose bin/ holds the fixture cohere and a cohere-swift.
func stageFixturePlatform(test *testing.T, output string, target Target) {
	test.Helper()
	directory := filepath.Join(output, target.DirectoryName())
	manifest, platformManifestError := PlatformManifest(target, "1.0.0")
	if platformManifestError != nil {
		test.Fatal(platformManifestError)
	}
	writeFile(test, filepath.Join(directory, "package.json"), string(manifest))
	for _, name := range []string{BinaryFileName(target.GoOperatingSystem), SwiftEngineFileName} {
		writeFixtureExecutable(test, filepath.Join(directory, "bin", name))
	}
}

// writeFixtureExecutable writes the fixture script, executable whatever the umask.
func writeFixtureExecutable(test *testing.T, path string) {
	test.Helper()
	writeExecutable(test, path, 0o755)
	if writeError := executable_testing.WriteFile(path, []byte(fixtureScript), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
}

// flipByte flips one bit of a file in place: the case of its first "f", which in the fixture turns
// "fixture" into "Fixture" and leaves a script that still runs.
func flipByte(test *testing.T, path string) {
	test.Helper()
	contents, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatal(readError)
	}
	index := bytes.IndexByte(contents, 'f')
	if index < 0 {
		index = len(contents) / 2
	}
	contents[index] ^= 0x20
	if writeError := executable_testing.WriteFile(path, contents, 0o755); writeError != nil {
		test.Fatal(writeError)
	}
}

func sha256Hex(contents []byte) string {
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}
