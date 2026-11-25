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

// PrintShaped reports whether typed, an interface method's signature or a variable's or a field's type as its generic
// declaration declares it, or any instance of it, has the shape of a print or printf function, a final `args ...any`
// with or without a format string before it. A candidate is admitted by the shape of the signature it is gathered by
// alone (wrapperSearch.add): an interface method's, an embedded instance's among them, or a function literal's, which
// is the type of the variable or field it is assigned to, an instance's for a field of a generic type, since only an
// identical one is assignable but to an interface, through which no call is made, so no fact of it is ever read. The
// fact is on the generic declaration all the same (seats' review-1 on #zqx7gke, must 1), so a type parameter counts
// as the shape any instance could give it: the whole type, which an instance can make a function, or the variadic
// element, which an instance can make any. Anything else keeps its kind in every instance. So no object whose generic
// declaration fails this holds a wrapper fact any call reads, in any build.
func PrintShaped(typed types.Type) bool {
	if _, isParameter := types.Unalias(typed).(*types.TypeParam); isParameter {
		return true
	}
	signature, isSignature := typed.Underlying().(*types.Signature)
	if !isSignature || !signature.Variadic() {
		return false
	}
	// A variadic signature has a parameter, a slice.
	parameters := signature.Params()
	variadic, isSlice := parameters.At(parameters.Len() - 1).Type().(*types.Slice)
	if !isSlice {
		return false
	}
	switch element := types.Unalias(variadic.Elem()).(type) {
	case *types.Interface:
		return element.Empty()
	case *types.TypeParam:
		return true
	}
	return false
}
