package printf

import (
	"strings"

	"github.com/system-inc/go-types/types"
)

// knownNames are the print functions known by name, knownPrintFunctions with what the -funcs flag added, as
// wrapperKinds.of asks them of every function a pass meets: by the function's own name first, so most functions, which
// are no print function, are passed by without the full name, which allocates, or the name lowercased.
type knownNames struct {
	// functions are the names themselves.
	functions stringSet

	// lastSegments are each name's part after its last dot, the whole name when it has none.
	lastSegments map[string]bool

	// anyWithoutDot is whether a name has no dot, which only the -funcs flag adds and which matches a function's name
	// in any case.
	anyWithoutDot bool
}

// newKnownNames is functions as knownNames asks them. It is built for each pass rather than once for the process,
// since the -funcs flag adds to knownPrintFunctions after the analyzer is built, and a pass reads the set as it is
// then, as before.
func newKnownNames(functions stringSet) knownNames {
	names := knownNames{functions: functions, lastSegments: make(map[string]bool, len(functions))}
	for name := range functions {
		lastDot := strings.LastIndex(name, ".")
		names.lastSegments[name[lastDot+1:]] = true
		if lastDot < 0 {
			names.anyWithoutDot = true
		}
	}
	return names
}

// holds reports whether object's full name is among the names, or its name lowercased is among those without a dot.
// The answer is the full lookups' exactly: a full name is the name itself or ends in a dot and the name, which has no
// dot, so a full name among the names has the name among lastSegments; and the name lowercased has no dot, so only a
// name without a dot can equal it.
func (names knownNames) holds(object types.Object) bool {
	if names.lastSegments[object.Name()] {
		if _, known := names.functions[fullName(object)]; known {
			return true
		}
	}
	if !names.anyWithoutDot {
		return false
	}
	_, known := names.functions[strings.ToLower(object.Name())]
	return known
}
