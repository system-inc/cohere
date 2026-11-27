package release

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// SwiftResolvedVariable names a laid resolved set of cohere-swift's packages: a directory whose `scratch` is SwiftPM's
// scratch path after `swift package resolve`, every pin of Package.resolved checked out under its `checkouts`. Set,
// every SwiftPM build cohere does builds from it with no network (SwiftResolvedArguments). Unset, SwiftPM resolves as
// it always has, from the pins' remotes.
//
// It is for a machine with no network, the integrate job's on Shackler (#je1r32a): SwiftPM fetches cohere-swift's
// five packages from github.com and starts a sandbox of its own for the manifests, and a job's sandbox allows neither.
// Machines lays each set read-only under /opt/loom-tools/swiftpm-cohere, named by the sha256 of the Package.resolved
// it was resolved from, and the integrate job names the one its tree's Package.resolved hashes to.
const SwiftResolvedVariable = "COHERE_SWIFT_RESOLVED"

// swiftResolvedFlags are what every SwiftPM call adds when a set is named, the same on every call on a scratch path so
// a build and its `--show-bin-path` agree: no sandbox of SwiftPM's own, which can't start inside another, and no
// update from the pins' remotes, so a scratch holding every checkout resolves nothing over the network.
var swiftResolvedFlags = []string{"--disable-sandbox", "--skip-update"}

// SwiftResolvedArguments readies scratchDirectory, a SwiftPM scratch path, for a build from the set
// SwiftResolvedVariable names, and returns the flags every SwiftPM call on it adds: none when no set is named.
//
// The set's scratch is copied in when scratchDirectory holds no `checkouts` yet, since SwiftPM writes into its scratch
// and the set is read-only. One that holds them is left alone: its checkouts are an earlier build's, which SwiftPM
// keeps. The copy is `cp -cR`, APFS clones, so the set's 280 MB copies in moments. A sandbox that refuses clonefile,
// as an integrate job's on Shackler does ("clonefile failed: Operation not permitted", J-pjh3d4h), gets a plain `cp -R`
// into a scratch emptied of the clone's partial copy, the same files at the cost of their bytes (swiftResolvedCopy).
// Either way the copy is made writable by its owner, since the set is read-only and SwiftPM writes into its scratch. A
// copy that fails both ways is refused in its own words, never built around, since SwiftPM would then reach for the
// network it doesn't have.
func SwiftResolvedArguments(scratchDirectory string) ([]string, error) {
	set := strings.TrimSpace(os.Getenv(SwiftResolvedVariable))
	if set == "" {
		return nil, nil
	}
	if !filepath.IsAbs(set) {
		return nil, fmt.Errorf("%s=%q is no directory's whole path", SwiftResolvedVariable, set)
	}
	_, statError := os.Stat(filepath.Join(scratchDirectory, "checkouts"))
	if statError == nil {
		return slices.Clone(swiftResolvedFlags), nil
	}
	if !errors.Is(statError, fs.ErrNotExist) {
		return nil, fmt.Errorf("reading whether SwiftPM's scratch %s holds its checkouts: %w", scratchDirectory,
			statError)
	}
	if makeError := os.MkdirAll(scratchDirectory, 0o755); makeError != nil {
		return nil, fmt.Errorf("making SwiftPM's scratch %s: %w", scratchDirectory, makeError)
	}
	if copyError := swiftResolvedCopy(filepath.Join(set, "scratch"), scratchDirectory); copyError != nil {
		return nil, copyError
	}
	return slices.Clone(swiftResolvedFlags), nil
}

// swiftResolvedCopy copies scratch, a laid set's read-only SwiftPM scratch, into scratchDirectory, which exists and
// holds no checkouts: by APFS clones first, then, when cloning is refused, by a plain copy into a scratchDirectory
// emptied of whatever the clone left, and at the end makes every copied file and directory writable by its owner.
func swiftResolvedCopy(scratch string, scratchDirectory string) error {
	// The source's trailing slash copies the scratch's contents into scratchDirectory rather than a directory named
	// scratch inside it.
	source := scratch + string(filepath.Separator)
	cloned, cloneError := exec.Command("cp", "-cR", source, scratchDirectory).CombinedOutput()
	if cloneError != nil {
		entries, readError := os.ReadDir(scratchDirectory)
		if readError != nil {
			return fmt.Errorf("reading SwiftPM's scratch %s after its clone failed: %w", scratchDirectory, readError)
		}
		for _, entry := range entries {
			if removeError := os.RemoveAll(filepath.Join(scratchDirectory, entry.Name())); removeError != nil {
				return fmt.Errorf("emptying SwiftPM's scratch %s of a failed clone: %w", scratchDirectory, removeError)
			}
		}
		if copied, copyError := exec.Command("cp", "-R", source, scratchDirectory).CombinedOutput(); copyError != nil {
			return fmt.Errorf("copying the laid resolved set %s into SwiftPM's scratch %s: cp -cR failed (%s), and "+
				"cp -R: %w: %s", source, scratchDirectory, strings.TrimSpace(string(cloned)), copyError,
				strings.TrimSpace(string(copied)))
		}
	}
	if output, modeError := exec.Command("chmod", "-R", "u+w", scratchDirectory).CombinedOutput(); modeError != nil {
		return fmt.Errorf("making SwiftPM's scratch %s writable: %w: %s", scratchDirectory, modeError,
			strings.TrimSpace(string(output)))
	}
	return nil
}
