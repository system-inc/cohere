package configuration

// VersionRange is the "cohere" key of a project's settings: the cohere releases that project accepts,
// written the way npm writes a dependency range ("^1.0.0", "~1.4.2", ">=1.2.0 <2.0.0", "1.3.0 || 1.4.0").
//
// A project pins cohere so a machine running a different release refuses rather than reporting a
// verdict the project never agreed to. Rule names, options and sets are part of a release's contract,
// so a binary outside the range can read the same file and mean something else by it.
type VersionRange struct {
	// Text is the range as the file wrote it, for messages.
	Text string

	// alternatives are the `||`-separated comparator sets; a version satisfies the range when it
	// satisfies every comparator of any one set.
	alternatives [][]versionComparator
}

// Admits reports whether a release version falls inside the range.
//
// It follows npm's rule for prereleases: a prerelease satisfies a set of comparators only when one of
// them names the same major, minor and patch with a prerelease of its own. So `^1.0.0` does not admit
// 1.1.0-beta.1, and `>=1.1.0-beta.0 <2.0.0` does. A project opts into prereleases by writing one.
func (versionRange VersionRange) Admits(version string) (bool, error) {
	candidate, parseSemanticVersionError := parseSemanticVersion(version)
	if parseSemanticVersionError != nil {
		return false, parseSemanticVersionError
	}
	for _, comparators := range versionRange.alternatives {
		if admitsAll(comparators, candidate) {
			return true, nil
		}
	}
	return false, nil
}
