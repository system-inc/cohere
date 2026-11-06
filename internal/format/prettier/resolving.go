package prettier

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/system-inc/cohere/internal/format/formatfiles"
	"github.com/system-inc/cohere/internal/format/formatoptions"
)

// Resolving formats each file with the options Prettier would resolve for that file.
//
// The format phase used to build one Engine from formatoptions.Default, ahra's block, and run it over
// whatever repository cohere was pointed at. That formatted api-phi-health with the wrong
// bracketSameLine, which a differential measured as 41 files "rewritten" that are in fact a fixed point
// of their own configuration. Formatting a repository with another repository's options is a bug,
// not a default.
//
// So resolution is per file, from the file's own directory, as Prettier's own resolution is. It does
// not depend on the caller having walked the tree first, or on the tree having one config, both of
// which would be true of our repositories today and would quietly stop being true when one grows a
// nested config.
//
// Engines are built per distinct set of options and kept, so a repository with one config pays for
// one engine. The parser table is independent of options, which is why Handles and Enumerate need no
// resolution at all.
type Resolving struct {
	mutex       sync.Mutex
	first       *Engine
	engines     map[formatoptions.Options]*Engine
	byDirectory map[string]formatoptions.Resolution
}

// NewResolving builds the engine for startDirectory's options eagerly.
//
// Eagerly, so a formatter whose bundles cannot be evaluated, or whose starting directory carries a
// config cohere refuses, stops the run before any phase does work, rather than failing on the first
// file after the fix phase has begun.
func NewResolving(startDirectory string) (*Resolving, error) {
	resolving := &Resolving{
		engines:     map[formatoptions.Options]*Engine{},
		byDirectory: map[string]formatoptions.Resolution{},
	}
	resolution, resolveError := resolving.resolve(startDirectory)
	if resolveError != nil {
		return nil, resolveError
	}
	engine, engineForError := resolving.engineFor(resolution.Options)
	if engineForError != nil {
		return nil, engineForError
	}
	resolving.first = engine
	return resolving, nil
}

// Resolution returns what a file resolves to, for callers that need to say which config applied.
func (resolving *Resolving) Resolution(fileName string) (formatoptions.Resolution, error) {
	return resolving.resolve(filepath.Dir(fileName))
}

// Handles reports whether any engine formats this file type. Options do not change the answer.
func (resolving *Resolving) Handles(fileName string) bool {
	return resolving.first.Handles(fileName)
}

// Enumerate walks a tree for formattable files. Options do not change which files those are.
func (resolving *Resolving) Enumerate(root string) (formatfiles.Enumeration, error) {
	return resolving.first.Enumerate(root)
}

// Format formats one file with the options its own directory resolves to.
func (resolving *Resolving) Format(fileName string, text string) (string, error) {
	resolution, resolveError := resolving.resolve(filepath.Dir(fileName))
	if resolveError != nil {
		return "", fmt.Errorf("resolving the Prettier config for %s: %w", fileName, resolveError)
	}
	engine, engineForError := resolving.engineFor(resolution.Options)
	if engineForError != nil {
		return "", engineForError
	}
	return engine.Format(fileName, text)
}

func (resolving *Resolving) resolve(directory string) (formatoptions.Resolution, error) {
	absolute, absolutePathError := filepath.Abs(directory)
	if absolutePathError != nil {
		return formatoptions.Resolution{}, absolutePathError
	}
	resolving.mutex.Lock()
	cached, present := resolving.byDirectory[absolute]
	resolving.mutex.Unlock()
	if present {
		return cached, nil
	}

	resolution, resolveError := formatoptions.Resolve(absolute)
	if resolveError != nil {
		return formatoptions.Resolution{}, resolveError
	}
	resolving.mutex.Lock()
	resolving.byDirectory[absolute] = resolution
	resolving.mutex.Unlock()
	return resolution, nil
}

func (resolving *Resolving) engineFor(options formatoptions.Options) (*Engine, error) {
	resolving.mutex.Lock()
	defer resolving.mutex.Unlock()
	if engine, present := resolving.engines[options]; present {
		return engine, nil
	}
	engine, newPrettierError := New(options)
	if newPrettierError != nil {
		return nil, newPrettierError
	}
	resolving.engines[options] = engine
	return engine, nil
}
