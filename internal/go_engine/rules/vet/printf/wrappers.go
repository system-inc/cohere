package printf

import (
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// Wrappers is the pass's result: for each function the pass looked a wrapper fact up of, as it does of every callee
// that is no print function known by name, and each of its own package's it found to wrap one, the fact's words
// (isWrapper.String), empty for none. No rule reads it; the facts' reach compares a build's with the host's of each
// function a body uses (#5vbs1np), and the host reads the same words from the pass's facts. It is made of what the
// pass looked up already, never a listing of every fact the pass inherited, which no read would name.
type Wrappers struct {
	byObject map[types.Object]string

	// byName are the print functions known by name the pass met, whose kind is their name's in every build, so no fact
	// of theirs is read.
	byName map[types.Object]bool
}

// newWrappers is the wrappers of what kinds met in pass, once its search and its checks are done: each object met
// that is no print function known by name, by its generic declaration, whose fact the pass reads again, a read it
// made already, or one of its own package's, which is no read of another.
func newWrappers(pass *analysis.Pass, kinds wrapperKinds) *Wrappers {
	wrappers := &Wrappers{byObject: map[types.Object]string{}, byName: map[types.Object]bool{}}
	for object := range kinds.byObject {
		if kinds.names.holds(object) {
			wrappers.byName[origin(object)] = true
			continue
		}
		var fact isWrapper
		words := ""
		if pass.ImportObjectFact(origin(object), &fact) {
			words = fact.String()
		}
		wrappers.byObject[origin(object)] = words
	}
	return wrappers
}

// Of is the words of the wrapper fact of function's generic declaration, empty for no fact, and false for a function
// the pass never looked a fact up of: one known by name (ByName), or one no call of the package names.
func (wrappers *Wrappers) Of(function *types.Func) (string, bool) {
	words, isLooked := wrappers.byObject[function.Origin()]
	return words, isLooked
}

// ByName is whether function is a print function known by name the pass met, whose kind is its name's, never a fact's.
func (wrappers *Wrappers) ByName(function *types.Func) bool {
	return wrappers.byName[function.Origin()]
}
