package dispatch

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// The launcher builds the cohere every project's gate runs from the checkout's committed tree, never
// from its working tree.
//
// The checkout is shared. Several members edit it at once, and until this existed the binary was
// named for a hash of whatever `go list` found on disk, so any member's half-written rule went live in
// every project's gate on the next run. On 2026-10-02 one member's uncommitted reading of nested
// literals turned 22 Tailwind findings on in ahra before anyone had classified the work. The work was
// right; the gate was still running code nobody had committed, which is the honesty doctrine failing
// inside the toolchain that enforces it.
//
// So a gate run builds from a snapshot of HEAD: this repository extracted with `git archive`, and the
// vendored compiler extracted at the commit HEAD pins it to, with HEAD's own patches applied. Nothing
// in either comes from a working tree, the compiler submodule's included. Testing uncommitted work is
// still possible, and it is explicit: `--dev` builds the working tree and says so on every run.

// snapshotTag is mixed into the binary hash so a committed-tree build can never share a name with a
// binary built any other way. Both kinds land in the same directory.
const snapshotTag = "committed-tree"

// compilerSubdirectory is the part of the vendored compiler the build reads. `go.work` uses only
// `./TypeScript/tsc`, and every patch touches a path under it, so the rest of the submodule (another
// few hundred files of tooling) is left out of the snapshot.
const compilerSubdirectory = "tsc"

// PatchPresentMarker is the state `cohere --version` prints for a compiler patch whose probe measured
// the patched behaviour in that binary. It is written out here rather than imported, because the
// package that prints it links the whole compiler, and the launcher's job is one stat and one exec.
// A test in `patches` pins the two together.
const PatchPresentMarker = "present (measured in this binary)"

// committedBuild is what one committed-tree build is made from.
type committedBuild struct {
	// Commit is the commit of this repository being built.
	Commit string

	// CompilerCommit is the vendored compiler commit that Commit pins, read from Commit's own tree.
	// The submodule's checkout is not consulted, because a member bumping the pin locally is exactly
	// an uncommitted change.
	CompilerCommit string

	// GoVersion identifies the toolchain, as Inputs.GoVersion does.
	GoVersion string
}

// hash names the binary this build produces.
//
// The commit decides everything committed: the rules, the shims, the pinned compiler commit and the
// patch files are all in its tree. What it does not decide is the toolchain and the flags, so those
// are framed in beside it.
func (build committedBuild) hash() string {
	digest := sha256.New()
	fmt.Fprintf(digest, "kind\x00%s\x00", snapshotTag)
	fmt.Fprintf(digest, "commit\x00%s\x00", build.Commit)
	fmt.Fprintf(digest, "goVersion\x00%s\x00", build.GoVersion)
	for _, flag := range ReleaseBuildFlags {
		fmt.Fprintf(digest, "buildFlag\x00%s\x00", flag)
	}
	return hex.EncodeToString(digest.Sum(nil))[:hashLength]
}

// SnapshotDirectory holds the per-build extractions of this repository. Each is removed once its
// binary is built, because the binary is self-contained and the next commit needs its own.
func (paths Paths) SnapshotDirectory() string {
	return filepath.Join(paths.CacheDirectory, "snapshot")
}

// CompilerDirectory holds patched compiler extractions, one per pinned commit and patch set.
//
// These are kept, because the compiler is 66,000 files and takes about seven seconds to extract,
// while the pin and the patches change rarely. Two commits of this repository that pin the same
// compiler with the same patches build against the same directory.
func (paths Paths) CompilerDirectory() string {
	return filepath.Join(paths.CacheDirectory, "compiler")
}

// ResolveCommitted returns a cohere binary built from the checkout's HEAD, building it when the
// cache has none.
//
// The returned string is the commit the binary was built from, so the caller can say which one ran,
// and the boolean reports whether a build happened.
func ResolveCommitted(paths Paths, packagePath string) (binaryPath string, commit string, built bool, err error) {
	build, err := readCommittedBuild(paths.ModuleDirectory)
	if err != nil {
		return "", "", false, err
	}

	binaryPath = paths.BinaryPath(build.hash())
	if _, statErr := os.Stat(binaryPath); statErr == nil {
		return binaryPath, build.Commit, false, nil
	}

	if err := buildCommitted(paths, packagePath, build, binaryPath); err != nil {
		return "", "", false, err
	}
	return binaryPath, build.Commit, true, nil
}

// readCommittedBuild reads HEAD, the compiler commit HEAD pins, and the toolchain.
func readCommittedBuild(moduleDirectory string) (committedBuild, error) {
	commitOutput, err := gitOutput(moduleDirectory, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return committedBuild{}, fmt.Errorf("reading the commit to build cohere from: %w", err)
	}
	commit := strings.TrimSpace(commitOutput)

	compilerCommit, err := pinnedCompilerCommit(moduleDirectory, commit)
	if err != nil {
		return committedBuild{}, err
	}

	goVersion, err := goEnvironment(moduleDirectory)
	if err != nil {
		return committedBuild{}, err
	}

	return committedBuild{Commit: commit, CompilerCommit: compilerCommit, GoVersion: goVersion}, nil
}

// pinnedCompilerCommit reads the vendored compiler's commit from a commit's tree.
//
// The gitlink recorded in the commit is the pin. The submodule's own HEAD is what the working tree
// has checked out, and it differs exactly when someone has moved it without committing.
func pinnedCompilerCommit(moduleDirectory string, commit string) (string, error) {
	output, err := gitOutput(moduleDirectory, "ls-tree", commit, "TypeScript")
	if err != nil {
		return "", fmt.Errorf("reading the compiler pin from %s: %w", shortCommit(commit), err)
	}
	// "160000 commit <sha>\tTypeScript" for a submodule. Anything else means the commit does not pin
	// a compiler at all, and building would pick one up from somewhere unnamed.
	fields := strings.Fields(output)
	if len(fields) < 3 || fields[0] != "160000" || fields[1] != "commit" {
		return "", fmt.Errorf("commit %s does not pin the vendored compiler as a submodule at TypeScript (ls-tree printed %q)",
			shortCommit(commit), strings.TrimSpace(output))
	}
	return fields[2], nil
}

// buildCommitted extracts the snapshot, builds it, and proves the result before it can be found.
func buildCommitted(paths Paths, packagePath string, build committedBuild, binaryPath string) error {
	for _, directory := range []string{paths.BinaryDirectory(), paths.GoCacheDirectory(), paths.SnapshotDirectory(), paths.CompilerDirectory()} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", directory, err)
		}
	}

	fmt.Fprintf(os.Stderr, "cohere: building from commit %s, its committed tree only\n", shortCommit(build.Commit))

	// A directory of its own, so two runs building at once never extract over each other.
	created, err := os.MkdirTemp(paths.SnapshotDirectory(), shortCommit(build.Commit)+"-*")
	if err != nil {
		return fmt.Errorf("creating a snapshot directory: %w", err)
	}
	defer os.RemoveAll(created)

	// Every path below is the resolved one. Go resolves the build's working directory through any
	// symlink and compares it with the workspace file's location, so a snapshot named through a link
	// (macOS's `/var` is one, to `/private/var`) is reported as outside its own workspace and fails to
	// build. Found by the test fixture, which lives under `/var`.
	snapshot, err := filepath.EvalSymlinks(created)
	if err != nil {
		return fmt.Errorf("resolving the snapshot directory: %w", err)
	}

	if err := extractGitArchive(paths.ModuleDirectory, build.Commit, "", snapshot); err != nil {
		return fmt.Errorf("extracting commit %s: %w", shortCommit(build.Commit), err)
	}

	patchFiles, err := committedPatchFiles(snapshot)
	if err != nil {
		return err
	}

	compiler, err := ensureCompiler(paths, build.CompilerCommit, patchFiles)
	if err != nil {
		return err
	}

	// `git archive` writes the submodule as an empty directory. The patched compiler goes in its place
	// as a link, which Go follows for a workspace module, and which keeps the snapshot to this
	// repository's own few thousand files.
	submodulePath := filepath.Join(snapshot, "TypeScript")
	if err := os.Remove(submodulePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clearing the submodule placeholder in the snapshot: %w", err)
	}
	if err := os.Symlink(compiler, submodulePath); err != nil {
		return fmt.Errorf("linking the patched compiler into the snapshot: %w", err)
	}

	// Built to a temporary name and renamed only once proven, so a concurrent run never finds, and
	// execs, a binary that is half-written or that failed its proof.
	temporary := binaryPath + fmt.Sprintf(".partial-%d", os.Getpid())
	defer os.Remove(temporary)

	if err := goBuildSnapshot(paths, snapshot, packagePath, build, temporary); err != nil {
		return err
	}
	if err := proveCommittedBinary(temporary, snapshot, build, len(patchFiles)); err != nil {
		return err
	}
	if err := os.Rename(temporary, binaryPath); err != nil {
		return fmt.Errorf("moving the built binary into the cache: %w", err)
	}
	return nil
}

// committedPatchFiles lists the compiler patches as the snapshot holds them, in the order they apply.
//
// Read from the snapshot rather than from this binary's embedded copy, because the patches are part
// of the commit being built. The files are numbered, so name order is application order, which is
// also the order `patches.All` lists them in.
func committedPatchFiles(snapshot string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(snapshot, "patches", "*.patch"))
	if err != nil {
		return nil, fmt.Errorf("listing the committed compiler patches: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

// ensureCompiler returns a patched extraction of the compiler at compilerCommit, creating it once.
//
// It is keyed by the commit and by the patch contents together, so a changed patch can never reuse a
// compiler patched by its previous version.
func ensureCompiler(paths Paths, compilerCommit string, patchFiles []string) (string, error) {
	digest := sha256.New()
	for _, path := range patchFiles {
		contents, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("reading committed patch %s: %w", filepath.Base(path), err)
		}
		fmt.Fprintf(digest, "patch\x00%s\x00%d\x00", filepath.Base(path), len(contents))
		digest.Write(contents)
	}
	key := shortCommit(compilerCommit) + "-" + hex.EncodeToString(digest.Sum(nil))[:hashLength]
	compiler := filepath.Join(paths.CompilerDirectory(), key)

	if _, err := os.Stat(compiler); err == nil {
		return compiler, nil
	}

	fmt.Fprintf(os.Stderr, "cohere: extracting the compiler at %s with %d patch(es), once per pin (about seven seconds)\n",
		shortCommit(compilerCommit), len(patchFiles))

	partial, err := os.MkdirTemp(paths.CompilerDirectory(), key+".partial-*")
	if err != nil {
		return "", fmt.Errorf("creating a compiler directory: %w", err)
	}
	// After a successful rename the name no longer exists and this does nothing.
	defer os.RemoveAll(partial)

	submodule := filepath.Join(paths.ModuleDirectory, "TypeScript")
	if err := extractGitArchive(submodule, compilerCommit, compilerSubdirectory, partial); err != nil {
		return "", fmt.Errorf("extracting the compiler at %s (if the commit is missing, the submodule has not fetched it: "+
			"run `git submodule update --init` in %s): %w", shortCommit(compilerCommit), paths.ModuleDirectory, err)
	}

	for _, patchFile := range patchFiles {
		if err := applyPatch(partial, patchFile); err != nil {
			return "", err
		}
	}

	if err := os.Rename(partial, compiler); err != nil {
		// Another run finished the same extraction first. Its directory is identical by construction,
		// so it is used and this one is discarded.
		if _, statErr := os.Stat(compiler); statErr == nil {
			return compiler, nil
		}
		return "", fmt.Errorf("moving the patched compiler into the cache: %w", err)
	}
	return compiler, nil
}

// applyPatch applies one committed patch to an extracted compiler, refusing if it does not apply.
//
// The extraction is not a repository, so `git apply` patches files the way `patch` would. The
// directory does sit inside this checkout's ignored cache, and git would otherwise search upward and
// find the checkout; the ceiling stops that search at the extraction. Measured 2026-10-02: in an
// ignored directory the apply landed correctly even without the ceiling. It is set so that the result
// cannot depend on the state of an enclosing repository at all. The real guard is downstream: the
// built binary's probe measures whether each patch is present, and a build where one is not is refused.
func applyPatch(compiler string, patchFile string) error {
	name := filepath.Base(patchFile)

	command := exec.Command("git", "apply", patchFile)
	command.Dir = compiler
	command.Env = append(os.Environ(), "GIT_CEILING_DIRECTORIES="+filepath.Dir(compiler))
	var standardError bytes.Buffer
	command.Stderr = &standardError
	if err := command.Run(); err != nil {
		// A patch that upstream has since merged applies in reverse. That build would still be correct,
		// but the patch is now dead weight, and nothing else would ever say so.
		reverse := exec.Command("git", "apply", "--reverse", "--check", patchFile)
		reverse.Dir = compiler
		reverse.Env = command.Env
		if reverse.Run() == nil {
			fmt.Fprintf(os.Stderr, "cohere: %s is already in the pinned compiler; delete it and its entry in patches/patches.go\n", name)
			return nil
		}
		return fmt.Errorf("compiler patch %s does not apply to the pinned compiler, so no binary was built "+
			"(building the stock compiler under a binary that lists the patch would be the defect patches exist to prevent): %v\n%s",
			name, err, strings.TrimSpace(standardError.String()))
	}
	return nil
}

// goBuildSnapshot compiles the snapshot.
//
// VCS stamping is off, and the commits are stamped explicitly instead. The snapshot has no `.git` of
// its own and sits inside this checkout, so with stamping on Go walks up to the checkout and records
// the working tree's revision and its dirty state, which is the state this build exists to exclude.
//
// GOWORK is set to the snapshot's own workspace file. An inherited GOWORK pointing at the checkout
// would build the checkout's modules from inside the snapshot without any error.
func goBuildSnapshot(paths Paths, snapshot string, packagePath string, build committedBuild, outputPath string) error {
	const packagingPath = "github.com/system-inc/cohere/internal/release/packaging"

	linkerFlags := []string{}
	arguments := []string{"build", "-buildvcs=false"}
	for _, flag := range ReleaseBuildFlags {
		// The release flags carry their own `-ldflags`, and Go keeps only the last one given, so the
		// stamps are joined onto it rather than passed as a second flag that would erase the first.
		if value, isLinkerFlags := strings.CutPrefix(flag, "-ldflags="); isLinkerFlags {
			linkerFlags = append(linkerFlags, value)
			continue
		}
		arguments = append(arguments, flag)
	}
	linkerFlags = append(linkerFlags,
		"-X", packagingPath+".selfCommit="+build.Commit,
		"-X", packagingPath+".compilerCommit="+build.CompilerCommit,
	)
	arguments = append(arguments, "-ldflags="+strings.Join(linkerFlags, " "), "-o", outputPath, packagePath)

	command := exec.Command("go", arguments...)
	command.Dir = snapshot
	command.Env = append(os.Environ(),
		"GOCACHE="+paths.GoCacheDirectory(),
		"GOWORK="+filepath.Join(snapshot, "go.work"),
		"CGO_ENABLED=0",
	)
	command.Stderr = os.Stderr

	if err := command.Run(); err != nil {
		return fmt.Errorf("building cohere from commit %s: %w", shortCommit(build.Commit), err)
	}
	if _, err := os.Stat(outputPath); err != nil {
		return fmt.Errorf("go build reported success but produced no binary at %s: %w", outputPath, err)
	}
	return nil
}

// proveCommittedBinary asks the built binary what it is, and refuses it unless the answer matches the
// build.
//
// Go's exit code says the compile succeeded. It does not say the stamps took, or that the compiler
// patches are in the checker this binary links. The binary's own `--version` measures both, the
// patches by running each probe against its own checker, so the proof is read off the artifact
// rather than off the steps that made it.
func proveCommittedBinary(binaryPath string, workingDirectory string, build committedBuild, patchCount int) error {
	command := exec.Command(binaryPath, "--version")
	command.Dir = workingDirectory
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("the binary built from %s failed its own --version (%w), so it was not cached:\n%s",
			shortCommit(build.Commit), err, output)
	}
	return checkCommittedVersion(string(output), build.Commit, patchCount)
}

// checkCommittedVersion is the judgement proveCommittedBinary makes, separate so it can be tested
// against the text a real binary prints without building one.
func checkCommittedVersion(version string, commit string, patchCount int) error {
	commitNamed := false
	present := 0
	for line := range strings.SplitSeq(version, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "commit:" && fields[1] == shortCommit(commit) {
			commitNamed = true
		}
		if strings.Contains(line, PatchPresentMarker) {
			present++
		}
	}

	if !commitNamed {
		return fmt.Errorf("the binary built from %s does not name that commit in --version, so it cannot be traced back to it "+
			"and was not cached:\n%s", shortCommit(commit), version)
	}
	if strings.Contains(version, "uncommitted changes") {
		return fmt.Errorf("the binary built from %s reports uncommitted changes, which a committed-tree build cannot have, "+
			"so it was not cached:\n%s", shortCommit(commit), version)
	}
	if present != patchCount {
		return fmt.Errorf("commit %s carries %d compiler patch(es) and the binary measured %d present, so it was not cached:\n%s",
			shortCommit(commit), patchCount, present, version)
	}
	return nil
}

// extractGitArchive writes a commit's tree, or one subdirectory of it, into an empty directory.
//
// `git archive` reads objects rather than files, so nothing in the working tree can reach the
// extraction, which is the whole point. The tar is read here rather than handed to `tar`, so every
// entry's path is checked against the destination before anything is written.
func extractGitArchive(repository string, commit string, subdirectory string, destination string) error {
	arguments := []string{"-C", repository, "archive", "--format=tar", commit}
	if subdirectory != "" {
		arguments = append(arguments, subdirectory)
	}
	command := exec.Command("git", arguments...)
	var standardError bytes.Buffer
	command.Stderr = &standardError
	stream, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}

	written, extractErr := extractTar(stream, destination)
	// Drained so git is never blocked writing into a pipe nobody reads after an extraction error.
	_, _ = io.Copy(io.Discard, stream)
	waitErr := command.Wait()

	if extractErr != nil {
		return extractErr
	}
	if waitErr != nil {
		return fmt.Errorf("git archive: %w: %s", waitErr, strings.TrimSpace(standardError.String()))
	}
	if written == 0 {
		// An empty extraction builds into nothing that runs, or worse, into something that runs and
		// checks nothing. A commit always has files, so zero is a failure that has to say so.
		return fmt.Errorf("git archive of %s wrote no files, which cannot be right", shortCommit(commit))
	}
	return nil
}

// extractTar writes a tar stream under destination, returning how many files it wrote.
func extractTar(stream io.Reader, destination string) (int, error) {
	reader := tar.NewReader(stream)
	written := 0
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return written, nil
		}
		if err != nil {
			return written, fmt.Errorf("reading the archive: %w", err)
		}

		target := filepath.Join(destination, filepath.FromSlash(header.Name))
		if target != destination && !strings.HasPrefix(target, destination+string(filepath.Separator)) {
			return written, fmt.Errorf("archive entry %q would be written outside %s", header.Name, destination)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return written, err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return written, err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode)&0o777)
			if err != nil {
				return written, err
			}
			if _, err := io.Copy(file, reader); err != nil {
				file.Close()
				return written, err
			}
			if err := file.Close(); err != nil {
				return written, err
			}
			written++
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return written, err
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				return written, err
			}
		case tar.TypeXGlobalHeader:
			// git archive opens with a pax header carrying the commit id. It describes the archive and
			// is not a file.
		default:
			return written, fmt.Errorf("archive entry %q has type %q, which the extraction does not handle", header.Name, header.Typeflag)
		}
	}
}

// gitOutput runs git in a directory and returns its standard output, carrying stderr into the error.
func gitOutput(directory string, arguments ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", directory}, arguments...)...)
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(standardError.String()))
	}
	return string(output), nil
}

// shortCommit trims a commit to the twelve characters `cohere --version` prints.
func shortCommit(commit string) string {
	const shortLength = 12
	if len(commit) <= shortLength {
		return commit
	}
	return commit[:shortLength]
}

// platformBinaryPrefix is how every hash-named cohere binary for this platform begins.
func platformBinaryPrefix() string {
	return fmt.Sprintf("cohere-%s-%s-", runtime.GOOS, runtime.GOARCH)
}
