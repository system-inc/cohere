package program

import "github.com/microsoft/TypeScript/tsc/shim/core"

// ambientModules is the module names the program's files declare with `declare module "name"` outside a
// module, exact names and patterns like `*.css` alike, which the checker answers an import with whether or
// not resolution found a file.
type ambientModules struct {
	names    map[string]bool
	patterns []core.Pattern
}

// answers is whether an ambient module answers an import of name, as the checker decides it: an exact name
// answers only a name that is not relative, and a pattern answers any name it matches.
func (declared ambientModules) answers(name string, relative bool) bool {
	if !relative && declared.names[name] {
		return true
	}
	for index := range declared.patterns {
		if declared.patterns[index].Matches(name) {
			return true
		}
	}
	return false
}
