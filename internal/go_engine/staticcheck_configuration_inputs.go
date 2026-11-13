package go_engine

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/tools/go/packages"
	"honnef.co/go/tools/config"

	"github.com/system-inc/cohere/internal/go_engine/staticcheck_configuration"
)

// staticcheckConfigurationInputs is what the findings cache keys a package on beyond its own files: the
// staticcheck.conf files staticcheck's config analyzer reads for it, which ST1001 and ST1013 require. The analyzer
// reads one in the package's directory and in every directory above it up to the root of the file system, outside the
// repository too, merging what it finds, and a malformed one fails the pass. So a package's inputs are each of those
// directories' staticcheck.conf, absent or by its sha256, and an edit to any of them misses every package below it.
// Packages share their ancestors, so each directory is read once.
type staticcheckConfigurationInputs struct {
	mutex       sync.Mutex
	directories map[string]string
}

// newStaticcheckConfigurationInputs reads nothing until a package asks.
func newStaticcheckConfigurationInputs() *staticcheckConfigurationInputs {
	return &staticcheckConfigurationInputs{directories: map[string]string{}}
}

// inputs is every staticcheck.conf the config analyzer would read for keyed, as cache.Keys' extraInputs hook. It
// never fails: a file it cannot read is an input saying so, which the analyzer fails on too.
func (configurationInputs *staticcheckConfigurationInputs) inputs(keyed *packages.Package) ([]string, error) {
	var inputs []string
	directory := configurationDirectory(keyed)
	for directory != "" {
		inputs = append(inputs, configurationInputs.read(directory))
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
		directory = parent
	}
	return inputs, nil
}

// read is the input for the staticcheck.conf in one directory, read once.
func (configurationInputs *staticcheckConfigurationInputs) read(directory string) string {
	configurationInputs.mutex.Lock()
	defer configurationInputs.mutex.Unlock()
	if input, read := configurationInputs.directories[directory]; read {
		return input
	}
	input := configurationInput(directory)
	configurationInputs.directories[directory] = input
	return input
}

// configurationDirectory is the directory the config analyzer starts from for keyed, found as it finds it:
// staticcheck_configuration.Directory over the names its files' package clauses are at, `//line` directives followed,
// which skips files in a build cache. Empty is a package the analyzer gives the default configuration without reading
// any file.
func configurationDirectory(keyed *packages.Package) string {
	if len(keyed.Syntax) == 0 {
		return staticcheck_configuration.Directory(keyed.CompiledGoFiles)
	}
	names := make([]string, 0, len(keyed.Syntax))
	for _, file := range keyed.Syntax {
		names = append(names, keyed.Fset.PositionFor(file.Pos(), true).Filename)
	}
	return staticcheck_configuration.Directory(names)
}

// configurationInput is the staticcheck.conf in directory as the analyzer reads it: absent when there is no regular
// file of that name, which it walks past, else the sha256 of its bytes, or why it could not be read.
func configurationInput(directory string) string {
	path := filepath.Join(directory, config.ConfigName)
	prefix := config.ConfigName + " at " + directory + ": "
	information, statError := os.Stat(path)
	if errors.Is(statError, fs.ErrNotExist) || (statError == nil && !information.Mode().IsRegular()) {
		return prefix + "absent"
	}
	if statError != nil {
		return prefix + "unreadable, " + statError.Error()
	}
	contents, readError := os.ReadFile(path)
	if readError != nil {
		return prefix + "unreadable, " + readError.Error()
	}
	digest := sha256.Sum256(contents)
	return prefix + hex.EncodeToString(digest[:])
}
