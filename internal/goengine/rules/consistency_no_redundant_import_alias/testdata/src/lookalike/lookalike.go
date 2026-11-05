// Package lookalike holds aliases that look redundant and are not, because each binds a name the package does not
// declare.
package lookalike

import (
	// The path's last element, where the package declares othername: the alias renames it.
	mismatched "mismatched"
	// The path's last element, where the package declares versioned: the alias renames it.
	v2 "versioned/v2"
	// Another standard package's name, given to path/filepath.
	path "path/filepath"
	// The package's name in another case, which Go reads as another name.
	Fmt "fmt"
)

// Used uses every import, so the fixture compiles.
var Used = Fmt.Sprint(mismatched.Count, v2.Version, path.Separator)
