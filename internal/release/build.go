package release

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BuildFlags are the flags every released binary is built with.
//
// They match `dispatch.ReleaseBuildFlags` deliberately: a locally rebuilt binary and a shipped one
// should differ only in their stamps, so that a bug reproduced against one reproduces against the
// other. `-trimpath` also removes the build machine's absolute paths from the binary, which a
// published artifact should not carry.
var BuildFlags = []string{"-trimpath"}

// Options configure a release build.
type Options struct {
	// ModuleDirectory is the root of the verify module.
	ModuleDirectory string

	// OutputDirectory is where the staged packages are written. Each platform package lands in a
	// subdirectory named for the platform, and the dispatcher package in "verify".
	OutputDirectory string

	// Version is the published version, like "0.3.1".
	Version string

	// Targets are the platforms to build. Empty means every target.
	Targets []Target

	// Signing configures macOS codesigning. A zero value stages unsigned binaries, which is a
	// supported outcome rather than a failure: signing needs credentials a contributor may not
	// have, and an npm install does not set the quarantine attribute that Gatekeeper checks. What
	// is not supported is an unsigned release that looks signed, so the state is always reported.
	Signing Signing
}

// Result reports what a release build produced.
type Result struct {
	// Packages are the staged package directories, in the order they were built.
	Packages []StagedPackage
}

// StagedPackage is one built package on disk.
type StagedPackage struct {
	// Name is the npm package name.
	Name string

	// Directory is where it was staged.
	Directory string

	// BinaryPath is the executable inside it, empty for the dispatcher-only package.
	BinaryPath string

	// SizeInBytes is the binary's size, for the release summary. A binary that comes out
	// drastically smaller than its siblings is usually a build that failed into an empty file.
	SizeInBytes int64
}

// Build stages every platform package and the dispatcher package.
//
// It fails on the first target that does not build, rather than collecting errors and reporting at
// the end. A release missing one platform is not a release with a warning: it is a published
// version that resolves to nothing on that platform, and the dispatcher's loud failure would be
// reported as a bug against a version that looked fine everywhere else.
func Build(options Options) (Result, error) {
	if options.Version == "" {
		// A release with no version stamps "dev" into the binary, and a binary that says "dev"
		// cannot be traced back to what shipped. That is precisely the thing --version exists for.
		return Result{}, fmt.Errorf("a release needs a version")
	}

	targets := options.Targets
	if len(targets) == 0 {
		targets = Targets
	}

	typeScriptGoCommit, err := readTypeScriptGoCommit(options.ModuleDirectory)
	if err != nil {
		return Result{}, err
	}

	goToolchain, err := readGoToolchain(options.ModuleDirectory)
	if err != nil {
		return Result{}, err
	}

	result := Result{}

	for _, target := range targets {
		staged, err := buildPlatformPackage(options, target, typeScriptGoCommit, goToolchain)
		if err != nil {
			return Result{}, fmt.Errorf("building %s: %w", target, err)
		}
		result.Packages = append(result.Packages, staged)
	}

	dispatcher, err := buildDispatcherPackage(options, typeScriptGoCommit, goToolchain)
	if err != nil {
		return Result{}, fmt.Errorf("building the dispatcher package: %w", err)
	}
	result.Packages = append(result.Packages, dispatcher)

	return result, nil
}

// buildPlatformPackage cross-compiles one target and writes its package around the binary.
func buildPlatformPackage(options Options, target Target, typeScriptGoCommit string, goToolchain string) (StagedPackage, error) {
	directory := filepath.Join(options.OutputDirectory, target.DirectoryName())
	binaryPath := filepath.Join(directory, "bin", target.BinaryFileName())

	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		return StagedPackage{}, fmt.Errorf("creating the package directory: %w", err)
	}

	if err := compile(options, target, binaryPath, typeScriptGoCommit, goToolchain); err != nil {
		return StagedPackage{}, err
	}

	size, err := verifyBinary(binaryPath)
	if err != nil {
		return StagedPackage{}, err
	}

	// Signed before the manifest is written, so a signing failure leaves an obviously incomplete
	// package rather than one that looks finished. Only macOS is signed: Linux and Windows have no
	// equivalent gate on an npm-installed binary.
	if target.IsMacOS() && options.Signing.IsConfigured() {
		if err := Sign(binaryPath, options.Signing); err != nil {
			return StagedPackage{}, err
		}
	}

	manifest, err := PlatformManifest(target, options.Version)
	if err != nil {
		return StagedPackage{}, err
	}
	if err := os.WriteFile(filepath.Join(directory, "package.json"), manifest, 0o644); err != nil {
		return StagedPackage{}, fmt.Errorf("writing the package manifest: %w", err)
	}

	return StagedPackage{
		Name:        target.PackageName(),
		Directory:   directory,
		BinaryPath:  binaryPath,
		SizeInBytes: size,
	}, nil
}

// buildDispatcherPackage writes the package consumers actually install.
//
// Nothing is compiled here, which is the point. The dispatcher is one package every machine
// installs regardless of platform, so it cannot be a Go binary without becoming a seventh platform
// package and defeating the single-install premise. It is a Node script instead — Node is present
// by construction in an npm install — and it resolves the platform package through the package
// manager rather than by guessing at directory layouts.
func buildDispatcherPackage(options Options, typeScriptGoCommit string, goToolchain string) (StagedPackage, error) {
	directory := filepath.Join(options.OutputDirectory, DispatcherPackageName)
	if err := os.MkdirAll(filepath.Join(directory, "bin"), 0o755); err != nil {
		return StagedPackage{}, fmt.Errorf("creating the dispatcher package directory: %w", err)
	}

	manifest, err := DispatcherManifest(options.Version)
	if err != nil {
		return StagedPackage{}, err
	}
	if err := os.WriteFile(filepath.Join(directory, "package.json"), manifest, 0o644); err != nil {
		return StagedPackage{}, fmt.Errorf("writing the dispatcher manifest: %w", err)
	}

	launcher := DispatcherLauncher()
	launcherPath := filepath.Join(directory, "bin", "verify")
	if err := os.WriteFile(launcherPath, []byte(launcher), 0o755); err != nil {
		return StagedPackage{}, fmt.Errorf("writing the dispatcher launcher: %w", err)
	}
	// WriteFile's mode is masked by umask, so the executable bit is set explicitly. A launcher that
	// ships without it fails at exec with a permission error on every machine that installs it.
	if err := os.Chmod(launcherPath, 0o755); err != nil {
		return StagedPackage{}, fmt.Errorf("making the dispatcher launcher executable: %w", err)
	}

	return StagedPackage{Name: DispatcherPackageName, Directory: directory}, nil
}

// compile cross-compiles one target, stamping the provenance in.
func compile(options Options, target Target, binaryPath string, typeScriptGoCommit string, goToolchain string) error {
	const packagePath = "github.com/system-inc/verify/internal/release"

	linkerFlags := strings.Join([]string{
		// Strip the symbol table and DWARF. Measured on a comparable binary: marginally faster to
		// link and 29% smaller, with nothing traded away that a released binary needs.
		"-s", "-w",
		"-X", packagePath + ".version=" + options.Version,
		"-X", packagePath + ".typeScriptGoCommit=" + typeScriptGoCommit,
		"-X", packagePath + ".goToolchain=" + goToolchain,
	}, " ")

	arguments := append([]string{"build"}, BuildFlags...)
	arguments = append(arguments, "-ldflags="+linkerFlags, "-o", binaryPath, "./cmd/verify")

	command := exec.Command("go", arguments...)
	command.Dir = options.ModuleDirectory
	command.Env = append(os.Environ(),
		"GOOS="+target.GoOperatingSystem,
		"GOARCH="+target.GoArchitecture,
		// Nothing here needs cgo, and disabling it is what makes the Linux binaries statically
		// linked. A dynamically linked binary would depend on the glibc of the build machine and
		// fail on any container older than it, which is a failure that only shows up in someone
		// else's CI.
		"CGO_ENABLED=0",
	)
	command.Stderr = os.Stderr

	if err := command.Run(); err != nil {
		return fmt.Errorf("go build: %w", err)
	}
	return nil
}

// verifyBinary confirms the build actually produced a runnable file, returning its size.
//
// Go reports success by exit code, but the artifact is the file. This closes the gap between "the
// build said it worked" and "there is a binary here", which is the same gap the rebuild cache
// checks for and the same reason: a missing or empty binary that reaches packaging becomes a
// published package that resolves to nothing.
func verifyBinary(path string) (int64, error) {
	information, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("go build reported success but produced no binary at %s: %w", path, err)
	}

	// A real verify binary statically links the whole TypeScript compiler and is over ten megabytes.
	// Anything near zero is a build that failed into an empty file, which packages perfectly and
	// installs perfectly and does nothing.
	const implausiblySmall = 1 << 20
	if information.Size() < implausiblySmall {
		return 0, fmt.Errorf("the binary at %s is %d bytes, which is far too small to be a real build", path, information.Size())
	}

	return information.Size(), nil
}

// readTypeScriptGoCommit reads the pinned commit of the vendored compiler.
func readTypeScriptGoCommit(moduleDirectory string) (string, error) {
	command := exec.Command("git", "-C", filepath.Join(moduleDirectory, "typescript-go"), "rev-parse", "HEAD")

	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("reading the pinned typescript-go commit: %w", err)
	}

	commit := strings.TrimSpace(string(output))
	if commit == "" {
		return "", fmt.Errorf("the pinned typescript-go commit came back empty")
	}
	return commit, nil
}

// readGoToolchain reads the version of the toolchain performing the build.
func readGoToolchain(moduleDirectory string) (string, error) {
	command := exec.Command("go", "env", "GOVERSION")
	command.Dir = moduleDirectory

	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("reading the Go toolchain version: %w", err)
	}

	toolchain := strings.TrimSpace(string(output))
	if toolchain == "" {
		return "", fmt.Errorf("the Go toolchain version came back empty")
	}
	return toolchain, nil
}
