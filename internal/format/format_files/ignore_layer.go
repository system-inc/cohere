package format_files

import (
	"path/filepath"
	"strings"

	"github.com/system-inc/cohere/internal/gitignore"
	"github.com/system-inc/cohere/internal/lint/configuration"
)

// ignoreLayer is one of the project's ignore lists after git's, and how its patterns are read.
type ignoreLayer struct {
	name string

	// lines, when set, is the house list: lines of an ignore file, read with git's syntax relative to the
	// walk root. Otherwise globs are lint's globs, relative to globsFrom: the `ignorePatterns` layer.
	lines     *gitignore.Patterns
	globs     []string
	globsFrom string
}

// covers reports whether the layer excludes a path, given relative to the walk root. A directory is
// covered when the layer prunes it whole.
func (layer ignoreLayer) covers(root string, relative string, directory bool) bool {
	if layer.lines != nil {
		ignored, _ := layer.lines.Ignored(relative, directory)
		return ignored
	}

	fromSettings, relativePathError := filepath.Rel(layer.globsFrom, filepath.Join(root, relative))
	if relativePathError != nil ||
		fromSettings == ".." ||
		strings.HasPrefix(fromSettings, ".."+string(filepath.Separator)) {
		return false
	}
	fromSettings = filepath.ToSlash(fromSettings)
	for _, pattern := range layer.globs {
		// A glob prunes a directory only when it takes everything below it, `name/**` or `**`: lint
		// matches files, and `*.code.js` matching a directory's name says nothing about its contents.
		if directory && !configuration.TakesWholeDirectory(pattern) {
			continue
		}
		if configuration.Match(pattern, fromSettings) {
			return true
		}
	}
	return false
}
