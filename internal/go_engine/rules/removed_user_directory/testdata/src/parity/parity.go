// Package parity holds the shapes SA9007 depends on, each with a comment saying whether the two are expected to find
// it: the removal made, started as a goroutine or deferred, inside closures, through a function value and an
// interface; the directory passed straight in, held in a variable, merged through branches and loops, captured by a
// closure, or returned by a helper; and each function that returns a guarded directory, by either of its results.
package parity

import (
	"os"
	"path/filepath"
)

// Found by both: each guarded directory removed outright.
func everyDirectory() {
	os.RemoveAll(os.TempDir())
	cache, _ := os.UserCacheDir()
	os.RemoveAll(cache)
	config, _ := os.UserConfigDir()
	os.RemoveAll(config)
	home, _ := os.UserHomeDir()
	os.RemoveAll(home)
}

// Found by neither: a subdirectory of each.
func subdirectories() {
	os.RemoveAll(filepath.Join(os.TempDir(), "work"))
	home, _ := os.UserHomeDir()
	os.RemoveAll(filepath.Join(home, ".cache", "tool"))
}

// Found by both: deferred and started as a goroutine.
func deferredAndStarted() {
	temporary := os.TempDir()
	defer os.RemoveAll(temporary)
	home, _ := os.UserHomeDir()
	go os.RemoveAll(home)
}

// Found by neither: os.Remove deletes an empty directory alone.
func removeOnly() {
	os.Remove(os.TempDir())
}

// Found by neither: the error result is no directory.
func errorResult() {
	_, homeError := os.UserHomeDir()
	if homeError != nil {
		os.RemoveAll(homeError.Error())
	}
}

// Found by both: a branch that reads the directory and leaves it as it was.
func readInBranch(condition bool) {
	temporary := os.TempDir()
	if condition {
		println(temporary)
	}
	os.RemoveAll(temporary)
}

// Found by neither: two calls of os.TempDir merged are two values.
func twoCallsMerged(condition bool) {
	temporary := os.TempDir()
	if condition {
		temporary = os.TempDir()
	}
	os.RemoveAll(temporary)
}

// Found by neither: merged with a subdirectory on one path.
func mergedWithSubdirectory(condition bool) {
	home, _ := os.UserHomeDir()
	if condition {
		home = filepath.Join(home, "work")
	}
	os.RemoveAll(home)
}

// Found by both: removed in a loop the directory does not change in.
func inLoop(rows int) {
	cache, _ := os.UserCacheDir()
	for row := 0; row < rows; row++ {
		os.RemoveAll(cache)
	}
}

// Found by both: a loop whose variable is assigned the value it already holds, a phi of itself and the directory,
// which staticcheck simplifies away before either reads it.
func reassignedInLoop(rows int) {
	config, _ := os.UserConfigDir()
	for row := 0; row < rows; row++ {
		os.RemoveAll(config)
		if row > 2 {
			config = config
		}
	}
}

// Found by both: a loop whose variable is given back the directory a second variable saved, two phis that reach each
// other and the directory, which staticcheck keeps and flattening reads through.
func restoredInLoop(rows int) {
	temporary := os.TempDir()
	saved := temporary
	for row := 0; row < rows; row++ {
		if row > 2 {
			temporary = saved
		}
	}
	os.RemoveAll(temporary)
}

// Found by neither: a loop whose variable is reassigned a subdirectory on some path, even one that never runs.
func subdirectoryInLoop(rows int) {
	config, _ := os.UserConfigDir()
	for row := 0; row < rows; row++ {
		os.RemoveAll(config)
		if row > rows {
			config = filepath.Join(config, "never")
		}
	}
}

// Found by both: inside a closure called in place, the directory found within it.
func insideClosure() {
	func() {
		os.RemoveAll(os.TempDir())
	}()
}

// Found by both: inside a goroutine's literal.
func insideGoroutine() {
	go func() {
		home, _ := os.UserHomeDir()
		os.RemoveAll(home)
	}()
}

// Found by neither: the directory captured by a closure, which reads it through the variable it shares.
func capturedDirectory() {
	temporary := os.TempDir()
	remove := func() {
		os.RemoveAll(temporary)
	}
	remove()
}

// Found by neither: a deferred literal that removes a captured directory.
func deferredLiteral() {
	home, _ := os.UserHomeDir()
	defer func() {
		os.RemoveAll(home)
	}()
}

// Found by both: os.RemoveAll held in a variable is the function itself.
func functionValue() {
	remove := os.RemoveAll
	remove(os.TempDir())
}

// Found by neither: a function value passed in may be any function.
func functionParameter(remove func(string) error) {
	remove(os.TempDir())
}

type remover interface {
	RemoveAll(path string) error
}

// Found by neither: a method named RemoveAll on an interface.
func throughInterface(value remover) {
	value.RemoveAll(os.TempDir())
}

func temporaryDirectory() string {
	return os.TempDir()
}

// Found by neither: a helper that returns the directory is a call of another function.
func throughHelper() {
	os.RemoveAll(temporaryDirectory())
}

var globalHome, _ = os.UserHomeDir()

// Found by neither: a package variable is loaded, not called.
func packageVariable() {
	os.RemoveAll(globalHome)
}

// Found by neither: a parameter.
func parameter(directory string) {
	os.RemoveAll(directory)
}

// Found by neither: another function of os returning a string.
func otherFunction() {
	working, _ := os.Getwd()
	os.RemoveAll(working)
	os.RemoveAll(os.Getenv("HOME"))
}

// Found by both, twice: one directory removed by two calls.
func removedTwice() {
	temporary := os.TempDir()
	os.RemoveAll(temporary)
	os.RemoveAll(temporary)
}

// Found by both: an alias of string is string, so the directory is passed as it was returned.
func aliased() {
	type path = string
	var temporary path = os.TempDir()
	os.RemoveAll(temporary)
}

// Found by neither: converted through a named type, the argument is the conversion's value.
func converted() {
	type path string
	os.RemoveAll(string(path(os.TempDir())))
}
