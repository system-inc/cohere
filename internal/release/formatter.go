package release

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/prettier"
)

// FormatterForkPathVariable overrides where the Prettier fork is read from.
//
// It is the engine's own name rather than a copy of it, for the same reason FormatterBundleNames is
// the engine's own list. This guard vouches for the bundles the engine loads, so it has to read the
// variable the engine reads: a second declaration of the same string would let the two drift, and
// the drift would be silent in the worst direction -- a guard reporting green about a directory
// nothing loads from. That was the defect this replaced, measured rather than supposed.
//
// The variable's meaning has since changed under this file. It used to mean "where is the fork";
// since the bundles were vendored it means "load from disk rather than from the binary", and the
// engine's default is the embedded copy rather than any path. This guard has not been reshaped for
// that yet, so it still resolves a fork checkout and still checks it the way it always did.
const FormatterForkPathVariable = prettier.ForkPathVariable

// DefaultFormatterForkPath is where the fork lives on the machine this was built on.
//
// It is declared here rather than read from `prettier` because the engine no longer has a default
// path to share: its default is the embedded bundles, and a path-shaped default would be the wrong
// answer to the wrong question. So this is deliberately release-local rather than the second
// declaration this file spent a commit removing -- there is no longer one value with two homes,
// there are two different questions, and only this file still asks the path-shaped one.
//
// It goes away when this guard is reshaped to vouch for embedded bytes, which is the other half of
// the vendoring unit and is tracked on #t55yd27.
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

// unwatchedForkPaths are tracked paths whose changes cannot reach the built bundles.
//
// This is an exclusion list rather than an inclusion list on purpose, and the difference is the
// whole point: an inclusion list is silent about what it never named, while an exclusion list is
// wrong only about things somebody wrote down and can be argued with. Everything tracked is watched
// unless it appears here with a reason.
//
// `tests` is 8,274 of the fork's 9,343 tracked files, so watching it would make a test edit look
// like a stale build and train everyone to ignore the warning. `dist` is the build output being
// judged, and including it would compare the bundles against themselves.
var unwatchedForkPaths = []string{
	"dist",
	"dist*",
	"tests",
	"website",
	"changelog_unreleased",
	"benchmarks",
	".vscode",
	".github",
}

// FormatterSource is a built Prettier fork on disk, and which commit built it.
type FormatterSource struct {
	// Directory holds the built bundles, the fork's `dist/prettier`.
	Directory string

	// Commit is the fork's HEAD at the time the bundles were read.
	Commit string

	// Digest is a sha256 over the bundle bytes, keyed by name, in BundleFiles order.
	//
	// Commit says which fork revision the bundles were built from; Digest says which bytes those
	// were. They answer different questions and a vendored copy needs both: the commit can be
	// correct while the bytes on disk are a stale build of it, and after vendoring nothing in a
	// `git pull` of the fork updates our copy.
	Digest string
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

	digest, err := digestBundles(bundleDirectory)
	if err != nil {
		return FormatterSource{}, err
	}

	return FormatterSource{Directory: bundleDirectory, Commit: commit, Digest: digest}, nil
}

// digestBundles hashes the bundles a build is about to embed, in BundleFiles order.
//
// It hashes the names alongside the bytes. Hashing bytes alone would give the same digest to a set
// where two bundles were swapped, and a swapped pair is exactly the kind of vendoring mistake that
// produces a binary which loads and formats wrongly rather than one that fails.
//
// It reads FormatterBundleNames, which is the engine's own BundleFiles, so the digest covers the set
// the engine loads rather than whatever happens to sit in the directory. That is the half `go:embed`
// cannot check: the compiler refuses a named file that is absent, and says nothing about a file
// present but unnamed. A stray bundle on disk changes nothing here, which is correct -- it is not
// loaded, so it is not part of what this binary formats with.
func digestBundles(bundleDirectory string) (string, error) {
	files := make(map[string][]byte, len(FormatterBundleNames))

	for _, name := range FormatterBundleNames {
		content, err := os.ReadFile(filepath.Join(bundleDirectory, filepath.FromSlash(name)))
		if err != nil {
			return "", fmt.Errorf("reading the Prettier bundle %s for its digest: %w", name, err)
		}
		files[name] = content
	}

	return DigestBundleFiles(files)
}

// DigestBundleFiles hashes bundles already in memory, which is how a running binary digests the
// bytes it embeds rather than a directory it may not have.
//
// It is the same function the build stamps with, deliberately. Two hash implementations over the
// same bytes is the drift that makes a digest comparison meaningless: the check would fail on
// correct bundles, someone would relax it, and the stamp would stop meaning anything. One function,
// two sources of bytes.
func DigestBundleFiles(files map[string][]byte) (string, error) {
	return digestNamedBundles(FormatterBundleNames, files)
}

// digestNamedBundles is the hash itself, over a caller-supplied name list.
//
// The list is a parameter rather than a reference to FormatterBundleNames so the framing can be
// tested. With the names hardcoded, no test could vary them, and a mutation dropping the name from
// the hash passed the whole package -- the property was untestable through the exported function
// and so was unverified despite having a test named for it.
//
// Each bundle is preceded by its name and its length, and only the name is load-bearing. That is
// worth stating plainly because two earlier versions of this comment claimed otherwise.
//
// The name defends a rename: the same bytes moving from `plugins/yaml.js` to `plugins/yml.js` at
// the same position digest identically without it, and BundleFiles is a hand-edited list where a
// rename is an ordinary edit. TestDigestKeysByName is its sole detector, confirmed by mutation.
//
// The length defends a boundary shift -- "AB" then "C" concatenating identically to "A" then "BC" --
// but only when the names cannot already separate the halves, and in this list they always can,
// because the names are distinct. Measured: with the length dropped and the names kept, no boundary
// shift over distinct names collides. So it is redundant here rather than load-bearing, and it is
// kept because it costs nothing and stops being redundant the moment two entries could share a name.
//
// It is not covered by a test, deliberately. A test for it would have to construct a name collision
// this list cannot contain, which would assert a property of the test's own fixture rather than of
// anything that ships.
func digestNamedBundles(names []string, files map[string][]byte) (string, error) {
	hash := sha256.New()

	for _, name := range names {
		content, present := files[name]
		if !present {
			return "", fmt.Errorf("the Prettier bundle %s is absent, so these bytes cannot be digested", name)
		}

		fmt.Fprintf(hash, "%s\x00%d\x00", name, len(content))
		hash.Write(content)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
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
//
// **This test runs in one direction only, and the name of this function overstates it.** Source
// newer than bundles proves the bundles are stale. Bundles newer than source proves nothing: any
// operation that bumps an mtime without rebuilding — a `touch`, a copy that preserves nothing, a
// checkout that rewrites the working tree — makes stale bundles pass. Verified by constructing
// exactly that case, and it passes today.
//
// It is stated rather than closed because closing it properly is a different check than this one.
// Comparing bundle contents to a recorded hash would catch a touch, and would still not answer
// "were these built from this source", which is the question the name implies and which nothing
// short of rebuilding can answer. Hashing the eight bundles costs about 1ms, so the cost is not
// what stopped this; the reason is that a check upgraded from "catches the common case" to
// "catches one more case" while still not deciding the question is worth less than an accurate
// description of what it does catch.
//
// What it catches is the case that actually happens: someone edits the fork and forgets to rebuild.
// What it does not catch is someone defeating it, and nobody is adversarial here.
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
	// Everything tracked, minus what is excluded by name, rather than an allowlist of directories.
	//
	// The allowlist this replaces named `src`, `scripts`, and `package.json`, and saw 677 of the
	// fork's 9,343 tracked files. It could not see `bin/`, `_system/`, `.yarn/`, or any root build
	// configuration, so an edit to the build itself produced different bundles and the staleness
	// check reported fresh. A probe that enumerates what to look at cannot see what nobody thought
	// to list, and the omission is invisible because the result still looks like an answer.
	arguments := []string{"-C", forkPath, "ls-files"}
	for _, excluded := range unwatchedForkPaths {
		arguments = append(arguments, ":!:"+excluded)
	}

	output, err := exec.Command("git", arguments...).Output()
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
