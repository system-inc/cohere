package formatoptions

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Resolver resolves directories for one run, remembering each directory's answer so a tree of many
// directories walks each ancestor once, and sharing every settings file's chain across runs (see
// chainOf).
//
// A directory's answer is kept for the Resolver's life, which is one run: a Prettier config or a
// settings file appearing mid-run is not seen until the next one. Measured on ahra's 3,926 program
// files (#mtf2sxt): 888 directories resolving afresh re-read and re-parsed one of 3 chains each, about
// 1.8ms and 2.6 megabytes a directory, most of the formatter's processor time.
type Resolver struct {
	mutex       sync.Mutex
	byDirectory map[string]resolved
}

// Resolve answers as the package's Resolve does, from memory where it can.
func (resolver *Resolver) Resolve(directory string) (Resolution, error) {
	absolute, absolutePathError := filepath.Abs(directory)
	if absolutePathError != nil {
		return Resolution{}, absolutePathError
	}
	answer := resolver.resolve(absolute)
	return answer.resolution, answer.resolveError
}

func (resolver *Resolver) resolve(directory string) resolved {
	resolver.mutex.Lock()
	known, present := resolver.byDirectory[directory]
	resolver.mutex.Unlock()
	if present {
		return known
	}

	answer := resolver.resolveHere(directory)
	resolver.mutex.Lock()
	resolver.byDirectory[directory] = answer
	resolver.mutex.Unlock()
	return answer
}

// resolveHere is one step of the walk up. Old config is refused wherever the walk meets it, up to and
// including the directory whose settings win: cohere never reads it, so keeping it is a second
// statement of the options that nothing checks. A directory with neither answers as its parent does.
func (resolver *Resolver) resolveHere(directory string) resolved {
	leftover, found, prettierConfigurationInError := prettierConfigIn(directory)
	if prettierConfigurationInError != nil {
		return resolved{resolveError: prettierConfigurationInError}
	}
	if found {
		return resolved{
			resolveError: fmt.Errorf(
				"%s: %w; delete it, since cohere reads format options only from a \"format\" block in %s",
				leftover,
				ErrPrettierConfigRemains,
				SettingsFileName,
			),
		}
	}

	path := filepath.Join(directory, SettingsFileName)
	if _, statError := os.Stat(path); statError == nil {
		resolution, chainOfError := chainOf(path)
		return resolved{resolution: resolution, resolveError: chainOfError}
	}

	parent := filepath.Dir(directory)
	if parent == directory {
		// No settings anywhere above: zero config, which is the house stack and its format.
		resolution, houseResolutionError := houseResolution()
		return resolved{resolution: resolution, resolveError: houseResolutionError}
	}
	return resolver.resolve(parent)
}
