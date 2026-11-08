package printf

import (
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// wrapperKinds are what each function a pass has met is among print functions, found as a wrapper or looked up:
// x/tools' printf Result.
type wrapperKinds map[types.Object]wrapperKind

// of is what object, a function or a variable holding one, is among print functions: a known one by name, or a
// wrapper a fact says it is, looked up on its generic declaration: x/tools' printf callKind. The answer is kept, under
// the generic declaration for a fact's, as the original keeps it.
func (kinds wrapperKinds) of(pass *analysis.Pass, object types.Object) wrapperKind {
	if kind, cached := kinds[object]; cached {
		return kind
	}
	_, known := knownPrintFunctions[fullName(object)]
	if !known {
		// A name the -funcs flag gave without a dot, such as "printf".
		_, known = knownPrintFunctions[strings.ToLower(object.Name())]
	}
	if !known {
		// A wrapper's fact is on its generic declaration, not an instantiation.
		declared := origin(object)
		kind := kindNone
		var fact isWrapper
		if pass.ImportObjectFact(declared, &fact) {
			kind = fact.Kind
		}
		kinds[declared] = kind
		return kind
	}
	kind := kindPrint
	switch {
	case fullName(object) == "fmt.Errorf":
		kind = kindErrorf
	case strings.HasSuffix(object.Name(), "f"):
		kind = kindPrintf
	}
	kinds[object] = kind
	return kind
}
