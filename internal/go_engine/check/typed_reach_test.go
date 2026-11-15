package check

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/load"
)

// typedReach is every package a rule's run reaches (ruleReach), loaded with types, and what the load cost.
type typedReach struct {
	// byDirectory is each reached package by its directory relative to the go_engine directory.
	byDirectory map[string]*packages.Package

	// wall is how long the load took.
	wall time.Duration
}

// reachedEngine is the rules' reach loaded with types, once for every test of the package that reads it.
var reachedEngine = sync.OnceValues(func() (*typedReach, error) {
	parsed, parseError := engineSource()
	if parseError != nil {
		return nil, parseError
	}
	var patterns []string
	for _, directory := range ruleReach(parsed) {
		// A directory an import names that holds no Go file of its own is no package to load.
		if parsed[directory] != nil {
			patterns = append(patterns, "./internal/go_engine/"+directory)
		}
	}
	started := time.Now()
	// The module root, three directories above the check package's own, holds the go.mod whose replaces name every
	// module the engine imports, so the load needs no workspace file. Each package is loaded alone, never its test
	// variant, since the guard reads what a run executes.
	loaded, loadError := load.PackagesCached(filepath.Join("..", "..", ".."), patterns, []string{"GOWORK=off"}, nil,
		load.Caching{WithoutTests: true})
	if loadError != nil {
		return nil, loadError
	}
	wall := time.Since(started)
	// A package that does not type-check has holes in its type maps, where a write would go unseen.
	if len(loaded.Diagnostics) > 0 {
		first := loaded.Diagnostics[0]
		return nil, fmt.Errorf("the reached packages do not type-check, %d errors, the first %s:%d: %s",
			len(loaded.Diagnostics), first.File, first.Line, first.Message)
	}
	reach := &typedReach{byDirectory: map[string]*packages.Package{}, wall: wall}
	for _, root := range loaded.Roots {
		if directory, isBelow := strings.CutPrefix(root.PkgPath, engineImportPath); isBelow {
			reach.byDirectory[directory] = root
		}
	}
	return reach, nil
})

// loadedReach is reachedEngine, failing the test when it could not be loaded.
func loadedReach(test *testing.T) *typedReach {
	test.Helper()

	reach, loadError := reachedEngine()
	if loadError != nil {
		test.Fatalf("loading the rules' reach with types: %v", loadError)
	}
	return reach
}
