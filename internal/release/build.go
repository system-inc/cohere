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

// StripFlags remove the symbol table and DWARF from a released binary.
//
// Named rather than inlined so that DescribeBuild reports the flags a release actually used. A
// hand-written description drifts from the build it claims to describe, and a size label that names
// the wrong build is worse than an unlabeled size: it is confidently wrong rather than ambiguous.
var StripFlags = []string{"-s", "-w"}

// DescribeBuild names the build a released binary comes from, for any size reported about it.
//
// A size without its build is not a measurement. The same commit measures 43.1 MB stripped and
// 61.9 MB from a plain `go build`, an 18 MB spread, so two people quoting sizes from different
// builds can both be right and still disagree — which is exactly what happened here, and cost two
// messages to reconcile. The same trap has a sharper form for anyone attributing those bytes:
// `go tool nm` reports every symbol as size zero on a stripped binary, so an attribution run
// against a release build sums to zero rather than failing.
func DescribeBuild() string {
	return strings.Join(BuildFlags, " ") + " -ldflags=\"" + strings.Join(StripFlags, " ") + " ...\""
}

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

	// EmbedFormatter pulls the Prettier fork's bundles into the binary and stamps which commit
	// built them.
	//
	// Off by default because the formatter is not wired into verify yet, and a release that
	// demanded a built fork before anything could use it would block every build for a feature
	// nobody reaches. When it is on, a fork that is absent, unbuilt, or stale fails the release
	// rather than embedding whatever is on disk.
	EmbedFormatter bool

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

	pin, err := readCompilerPin(options.ModuleDirectory)
	if err != nil {
		return Result{}, err
	}

	goToolchain, err := readGoToolchain(options.ModuleDirectory)
	if err != nil {
		return Result{}, err
	}

	// Resolved once, before any target is built, so a stale or missing fork fails the release
	// immediately rather than after six cross-compilations.
	formatter := FormatterSource{}
	if options.EmbedFormatter {
		formatter, err = ResolveFormatterSource()
		if err != nil {
			return Result{}, err
		}
	}

	result := Result{}

	for _, target := range targets {
		staged, err := buildPlatformPackage(options, target, pin, goToolchain, formatter)
		if err != nil {
			return Result{}, fmt.Errorf("building %s: %w", target, err)
		}
		result.Packages = append(result.Packages, staged)
	}

	dispatcher, err := buildDispatcherPackage(options)
	if err != nil {
		return Result{}, fmt.Errorf("building the dispatcher package: %w", err)
	}
	result.Packages = append(result.Packages, dispatcher)

	return result, nil
}

// buildPlatformPackage cross-compiles one target and writes its package around the binary.
func buildPlatformPackage(options Options, target Target, pin compilerPin, goToolchain string, formatter FormatterSource) (StagedPackage, error) {
	directory := filepath.Join(options.OutputDirectory, target.DirectoryName())
	binaryPath := filepath.Join(directory, "bin", target.BinaryFileName())

	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		return StagedPackage{}, fmt.Errorf("creating the package directory: %w", err)
	}

	if err := compile(options, target, binaryPath, pin, goToolchain, formatter); err != nil {
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
//
// It takes no compiler pin and no toolchain for that reason: there is nothing here to stamp them
// into, and threading them in would imply this package carries provenance that it does not.
func buildDispatcherPackage(options Options) (StagedPackage, error) {
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
	launcherPath := filepath.Join(directory, "bin", FullCommandName)
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
func compile(options Options, target Target, binaryPath string, pin compilerPin, goToolchain string, formatter FormatterSource) error {
	const packagePath = "github.com/system-inc/verify/internal/release"

	// Strip the symbol table and DWARF. Measured on a comparable binary: marginally faster to link
	// and 29% smaller, with nothing traded away that a released binary needs.
	stamps := append([]string{}, StripFlags...)
	stamps = append(stamps,
		"-X", packagePath+".version="+options.Version,
		"-X", packagePath+".compilerCommit="+pin.Commit,
		"-X", packagePath+".compilerUpstream="+pin.Upstream,
		"-X", packagePath+".goToolchain="+goToolchain,
	)

	// Stamped only when a formatter is actually embedded. A binary carrying no formatter must not
	// report a commit for one, because a reader would take that as the Prettier it formats with.
	if formatter.Commit != "" {
		stamps = append(stamps, "-X", packagePath+".formatterCommit="+formatter.Commit)
	}

	linkerFlags := strings.Join(stamps, " ")

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

// compilerPin is which commit of which repository the vendored compiler is pinned to.
type compilerPin struct {
	// Commit is the submodule's HEAD.
	Commit string

	// Upstream is the repository that commit lives in, as "owner/name".
	Upstream string
}

// readCompilerPin reads the pinned commit of the vendored compiler and the repository it came from.
//
// Both halves are read from the submodule rather than written down here, because the upstream has
// already moved once: the compiler was vendored from `microsoft/typescript-go` until that
// repository was archived, and the pin is now against `microsoft/TypeScript`. A hardcoded label
// survives a migration like that while quietly becoming false, and a commit reported against the
// wrong repository is worse than no commit at all — it resolves to nothing and gives a reader no
// hint why.
//
// The directory name stays `typescript-go` through the migration, so it is a path rather than a
// claim about the upstream and is left alone.
func readCompilerPin(moduleDirectory string) (compilerPin, error) {
	submoduleDirectory := filepath.Join(moduleDirectory, "typescript-go")

	output, err := exec.Command("git", "-C", submoduleDirectory, "rev-parse", "HEAD").Output()
	if err != nil {
		return compilerPin{}, fmt.Errorf("reading the pinned compiler commit: %w", err)
	}

	commit := strings.TrimSpace(string(output))
	if commit == "" {
		return compilerPin{}, fmt.Errorf("the pinned compiler commit came back empty")
	}

	return compilerPin{Commit: commit, Upstream: readCompilerUpstream(submoduleDirectory)}, nil
}

// readCompilerUpstream names the repository the vendored compiler is checked out from.
//
// A missing remote degrades to "unknown" rather than failing the release. The commit is the fact a
// bug report needs most, and refusing to build because a submodule has no configured origin would
// trade a complete release for a slightly better label.
func readCompilerUpstream(submoduleDirectory string) string {
	output, err := exec.Command("git", "-C", submoduleDirectory, "remote", "get-url", "origin").Output()
	if err != nil {
		return "unknown"
	}

	return normalizeUpstream(string(output))
}

// normalizeUpstream reduces a git remote url to "owner/name".
//
// The two url shapes git accepts — the ssh `git@github.com:owner/name.git` and the https
// `https://github.com/owner/name.git` — have to produce the same label, because otherwise the same
// pin reads differently depending on how the build machine happened to clone, and a reader
// comparing two reports would see a difference that is not one.
//
// It is separate from the command that reads the remote so it can be tested without a git
// repository: the parsing is where the bugs are, and it should not need a fixture clone to exercise.
func normalizeUpstream(remoteUrl string) string {
	url := strings.TrimSpace(remoteUrl)
	if url == "" {
		return "unknown"
	}

	url = strings.TrimSuffix(url, ".git")
	if _, path, found := strings.Cut(url, ":"); found && !strings.HasPrefix(url, "http") {
		url = path
	}

	segments := strings.Split(strings.Trim(url, "/"), "/")
	if len(segments) < 2 {
		return "unknown"
	}
	return strings.Join(segments[len(segments)-2:], "/")
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
