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

// Of is the words of the wrapper fact of object's generic declaration, a function's, an interface method's or a
// variable's holding a function literal, empty for no fact, and false for an object the pass never looked a fact up
// of: one known by name (ByName), or one no call of the package names.
func (wrappers *Wrappers) Of(object types.Object) (string, bool) {
	words, isLooked := wrappers.byObject[origin(object)]
	return words, isLooked
}

// ByName is whether object is a print function known by name the pass met, whose kind is its name's, never a fact's.
func (wrappers *Wrappers) ByName(object types.Object) bool {
	return wrappers.byName[origin(object)]
}

// PrintShaped reports whether signature has the shape of a print or printf function, a final `args ...any` with or
// without a format string before it. A candidate is admitted by that shape alone (wrapperSearch.add): a function's or
// an interface method's declared signature, or a function literal's, which is the underlying type of the variable it
// is assigned to, since only an identical one is assignable. So no object of another shape holds a wrapper fact in any
// build.
func PrintShaped(signature *types.Signature) bool {
	_, arguments := formatArgumentsParameters(signature)
	return arguments != nil
}
