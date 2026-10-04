package native

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/system-inc/cohere/internal/format/formatfiles"
	"github.com/system-inc/cohere/internal/format/formatoptions"
)

// Resolving formats each file with the options its own directory resolves to, the way
// prettier.Resolving does for the goja engine, so switching the format phase from one to the other
// changes the printers and nothing about which options a file is printed with.
//
// It holds no engines, because a native printer costs nothing to construct: only resolutions are
// cached, one per directory.
type Resolving struct {
	mutex       sync.Mutex
	byDirectory map[string]formatoptions.Resolution
}

// NewResolving resolves startDirectory's options eagerly, so a config cohere refuses stops the run
// before any phase does work rather than on the first file after fixes have begun.
func NewResolving(startDirectory string) (*Resolving, error) {
	resolving := &Resolving{byDirectory: map[string]formatoptions.Resolution{}}
	if _, err := resolving.resolve(startDirectory); err != nil {
		return nil, err
	}
	return resolving, nil
}

// Handles reports whether a native printer is registered for the file's type.
func (resolving *Resolving) Handles(fileName string) bool {
	_, present := lookup(fileName)
	return present
}

// Enumerate walks a tree for the files a native printer handles.
func (resolving *Resolving) Enumerate(root string) (formatfiles.Enumeration, error) {
	return formatfiles.Enumerate(root, resolving.Handles)
}

// Format formats one file with the options its own directory resolves to.
func (resolving *Resolving) Format(fileName string, text string) (string, error) {
	resolution, err := resolving.resolve(filepath.Dir(fileName))
	if err != nil {
		return "", fmt.Errorf("resolving the format options for %s: %w", fileName, err)
	}
	return Formatter{Options: resolution.Options}.Format(fileName, text)
}

// OptionsFingerprint names the options a file formats with: every field, by name, so two resolutions
// that differ anywhere print differently.
func (resolving *Resolving) OptionsFingerprint(fileName string) (string, error) {
	resolution, err := resolving.resolve(filepath.Dir(fileName))
	if err != nil {
		return "", fmt.Errorf("resolving the format options for %s: %w", fileName, err)
	}
	return fmt.Sprintf("%+v", resolution.Options), nil
}

func (resolving *Resolving) resolve(directory string) (formatoptions.Resolution, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return formatoptions.Resolution{}, err
	}
	resolving.mutex.Lock()
	cached, present := resolving.byDirectory[absolute]
	resolving.mutex.Unlock()
	if present {
		return cached, nil
	}

	resolution, err := formatoptions.Resolve(absolute)
	if err != nil {
		return formatoptions.Resolution{}, err
	}
	resolving.mutex.Lock()
	resolving.byDirectory[absolute] = resolution
	resolving.mutex.Unlock()
	return resolution, nil
}
