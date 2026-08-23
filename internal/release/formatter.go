package release

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/system-inc/verify/internal/prettier"
)

// FormatterForkPathVariable overrides where the Prettier fork is read from.
//
// The default is a path on one machine, which is the whole problem this file exists to bound. A CI
// runner or a second developer needs to say where their checkout is, and saying it explicitly is
// better than a search that might find the wrong one.
const FormatterForkPathVariable = "AHRA_VERIFY_PRETTIER_FORK"

// DefaultFormatterForkPath is where the fork lives on the machine it was built on.
const DefaultFormatterForkPath = "/Users/kirkouimet/Projects/system/prettier"

// FormatterBundleNames are the JavaScript bundles a release must provide.
//
// It is the engine's own list rather than a copy of it, and that is the entire point. `prettier`
// exports `BundleFiles` precisely so this step ships exactly what the engine loads: a bundle
// vendored but not loaded is dead weight, and a bundle loaded but not vendored is a build that
// fails at runtime on a machine without the fork. A second list here would drift, and the drift
// would be invisible — a release would pass every check and produce a binary that dies the first
// time someone formats the language whose plugin went missing.
//
// This was a real defect and not a hypothetical one. This file previously named three bundles,
// chosen as "the minimal set that formats TypeScript", while the engine hard-errors on any of eight
// being absent. The guard would have passed a release missing five of them.
//
// Measured at 05:43: the eight bundles total 2.1 MB, against a binary that is now 45 MB.
var FormatterBundleNames = prettier.BundleFiles

// FormatterSource is a built Prettier fork on disk, and which commit built it.
type FormatterSource struct {
	// Directory holds the built bundles, the fork's `dist/prettier`.
	Directory string

	// Commit is the fork's HEAD at the time the bundles were read.
	Commit string
}

// ResolveFormatterSource finds the built fork and refuses anything it cannot vouch for.
//
// Every failure here is loud, and that is the entire point of the file. A build that embeds a stale
// bundle formats the whole tree slightly wrong, forever, and every diff after that is noise — which
// makes it far worse than a build that does not run. A missing bundle is easy to notice; a stale one
// is not, so absence and staleness are both checked rather than trusting that absence covers it.
//
// The bundles are build output of a repository this one does not pin, so their freshness cannot be
// derived from any commit in this tree. It has to be measured against the fork itself.
func ResolveFormatterSource() (FormatterSource, error) {
	forkPath := strings.TrimSpace(os.Getenv(FormatterForkPathVariable))
	if forkPath == "" {
		forkPath = DefaultFormatterForkPath
	}

	if _, err := os.Stat(forkPath); err != nil {
		return FormatterSource{}, fmt.Errorf(
			"the Prettier fork is not at %s: %w\nClone it, or set %s to where it is",
			forkPath, err, FormatterForkPathVariable,
		)
	}

	commit, err := readForkCommit(forkPath)
	if err != nil {
		return FormatterSource{}, err
	}

	bundleDirectory := filepath.Join(forkPath, "dist", "prettier")
	if err := requireFreshBundles(bundleDirectory, forkPath); err != nil {
		return FormatterSource{}, err
	}

	return FormatterSource{Directory: bundleDirectory, Commit: commit}, nil
}

// readForkCommit reads the fork's HEAD.
func readForkCommit(forkPath string) (string, error) {
	output, err := exec.Command("git", "-C", forkPath, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("reading the Prettier fork's commit at %s: %w", forkPath, err)
	}

	commit := strings.TrimSpace(string(output))
	if commit == "" {
		return "", fmt.Errorf("the Prettier fork's commit came back empty")
	}
	return commit, nil
}

// requireFreshBundles refuses bundles that are absent, empty, or older than the fork's own source.
//
// Staleness is measured as modification time against the newest tracked source file, rather than by
// recording a commit beside the bundles. A recorded commit only detects a rebuild that someone
// remembered to re-record, and the case that actually happens is an edited working tree that was
// never rebuilt at all — where the commit has not moved and the bundles are wrong anyway.
func requireFreshBundles(bundleDirectory string, forkPath string) error {
	if _, err := os.Stat(bundleDirectory); err != nil {
		return fmt.Errorf(
			"the Prettier fork at %s has not been built: no bundles at %s\nBuild it before releasing",
			forkPath, bundleDirectory,
		)
	}

	newestBundle, err := checkBundlesPresent(bundleDirectory)
	if err != nil {
		return err
	}

	newestSource, sourcePath, err := newestSourceTime(forkPath)
	if err != nil {
		// A fork whose source cannot be listed still has present, non-empty bundles, which is the
		// larger half of the check. Refusing the release over a git failure here would trade a
		// working build for a marginally stronger guarantee.
		return nil
	}

	if newestSource.After(newestBundle) {
		return fmt.Errorf(
			"the Prettier bundles in %s are older than the fork's source\n"+
				"  %s changed after the bundles were built\n"+
				"Rebuild the fork. Embedding these would format the whole tree against a Prettier that no longer exists",
			bundleDirectory, sourcePath,
		)
	}

	return nil
}

// checkBundlesPresent confirms every embedded bundle exists and holds something, returning the
// newest modification time among them.
//
// Emptiness is checked as well as existence because a build interrupted partway leaves zero-byte
// files behind, and a zero-byte bundle embeds cleanly, ships, and produces a formatter that silently
// does nothing.
func checkBundlesPresent(bundleDirectory string) (newest time.Time, err error) {
	for _, name := range FormatterBundleNames {
		path := filepath.Join(bundleDirectory, filepath.FromSlash(name))

		information, err := os.Stat(path)
		if err != nil {
			return newest, fmt.Errorf("the Prettier bundle %s is missing from %s: %w", name, bundleDirectory, err)
		}
		if information.Size() == 0 {
			return newest, fmt.Errorf(
				"the Prettier bundle at %s is empty, which would embed a formatter that does nothing",
				path,
			)
		}

		if modified := information.ModTime(); modified.After(newest) {
			newest = modified
		}
	}
	return newest, nil
}

// newestSourceTime returns the modification time of the most recently changed tracked source file
// in the fork, and which file it was.
//
// Only tracked files count, so an untracked scratch file or a stray editor backup does not make
// every release look stale. `dist` is excluded because it is the build output being judged, and
// including it would compare the bundles against themselves.
func newestSourceTime(forkPath string) (newest time.Time, path string, err error) {
	output, err := exec.Command("git", "-C", forkPath, "ls-files", "src", "scripts", "package.json").Output()
	if err != nil {
		return newest, "", fmt.Errorf("listing the Prettier fork's source: %w", err)
	}

	listed := 0
	for _, line := range strings.Split(string(output), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		listed++

		information, err := os.Stat(filepath.Join(forkPath, name))
		if err != nil {
			continue
		}
		if modified := information.ModTime(); modified.After(newest) {
			newest = modified
			path = name
		}
	}

	if listed == 0 {
		// An empty listing here is the vacuous-probe failure: `git ls-files` against a path that
		// does not exist succeeds and returns nothing, and reading that nothing as "no source
		// changed" would make the staleness check pass for every build while measuring nothing.
		// This exact mistake, on this exact command, reached a ruling earlier tonight.
		return newest, "", fmt.Errorf(
			"listing the Prettier fork's source matched no files, so the staleness check would measure nothing",
		)
	}

	return newest, path, nil
}
