package configuration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The `swift` block is the Swift engine's: cohere-swift reads it itself
// (swift/Sources/CohereSwift/Command/RuleConfiguration.swift), from the CohereSettings.json beside the
// project's root or the one --lint-config names, and refuses what it does not read. That reader takes one
// file and never follows `extends`, so this loader leaves the block's insides alone and refuses only a block
// no Swift run would ever read, naming the file: a block that loads and configures nothing is the silent
// drop checkTopLevelKeys exists to stop.

// swiftBlockKey is the top-level key the Swift engine reads.
const swiftBlockKey = "swift"

// checkSwiftBlockPlacement refuses a swift block in a file the Swift engine never reads as its settings: a
// base another file extends, or a set cohere carries.
//
// Here in readConfigLayers rather than in loadLayers beside the other keys a base may not carry, because a
// Swift-only run never loads a lint configuration: the only read of its settings this side of the engine is
// discovery's, through IgnorePatternsOf, so a check made only on Load would never run where it matters most.
//
// chain is the files extending this one, nearest last, so a file read as a base has a non-empty chain.
func checkSwiftBlockPlacement(path string, chain []string, present map[string]bool) error {
	if !present[swiftBlockKey] {
		return nil
	}
	if IsSet(path) {
		return fmt.Errorf("rule set %s declares \"swift\": the Swift engine reads its block from a settings file "+
			"on disk, beside a Swift project or named by --lint-config, never from a set, so the block would "+
			"configure nothing", path)
	}
	if len(chain) > 0 {
		return fmt.Errorf("lint config %s declares \"swift\", and it is extended by %s: the Swift engine reads its "+
			"block from one file and never follows \"extends\", so the block would configure nothing. Move it to "+
			"the settings file beside the Swift project", path, chain[len(chain)-1])
	}
	return nil
}

// CheckSwiftBlockHasAReader refuses a swift block in the settings file at path when the file's directory
// holds no Swift project, since then no Swift run reads it. A file with no swift block, or one that does not
// parse, is left to the loader.
//
// The rule lives here, with the key, but the command decides when to ask, and asks only of a settings file
// the command line did not name. The Swift engine reads the file --lint-config names wherever it sits, so a
// named file away from any Swift project is still read by every Swift run handed it. The loader cannot tell a
// named file from the one found beside a project, and refusing every file away from a Swift project would
// refuse that one wrongly.
func CheckSwiftBlockHasAReader(path string) error {
	contents, readError := os.ReadFile(path)
	if readError != nil {
		return fmt.Errorf("reading lint config %s: %w", path, readError)
	}
	var keyed map[string]json.RawMessage
	if json.Unmarshal(contents, &keyed) != nil {
		return nil
	}
	if _, declared := keyed[swiftBlockKey]; !declared {
		return nil
	}
	directory := filepath.Dir(path)
	holds, holdsSwiftProjectError := holdsSwiftProject(directory)
	if holdsSwiftProjectError != nil {
		return holdsSwiftProjectError
	}
	if holds {
		return nil
	}
	return fmt.Errorf("lint config %s declares \"swift\", and %s holds no Swift project (a Package.swift, a "+
		"project.yml, or exactly one .xcodeproj): the Swift engine reads the block only from the settings file "+
		"beside a Swift project, or the one --lint-config names, so nothing would read it. Move it to the "+
		"settings file beside the Swift project it configures, or remove it", path, directory)
}

// holdsSwiftProject reports whether directory is a Swift project's root by the engine's own rule
// (swift/Contract.md, the invocation): a regular file Package.swift, or a regular file project.yml, or
// exactly one entry ending .xcodeproj, a bundle the engine counts whatever its type. Two .xcodeproj the
// engine refuses, so they are no project.
func holdsSwiftProject(directory string) (bool, error) {
	for _, name := range []string{"Package.swift", "project.yml"} {
		if information, statError := os.Stat(
			filepath.Join(directory, name),
		); statError == nil && information.Mode().IsRegular() {
			return true, nil
		}
	}
	entries, readDirectoryError := os.ReadDir(directory)
	if readDirectoryError != nil {
		return false, fmt.Errorf("reading %s: %w", directory, readDirectoryError)
	}
	bundles := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".xcodeproj") {
			bundles++
		}
	}
	return bundles == 1, nil
}
