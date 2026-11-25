package lookalike

import (
	. "lookalike/dotted"
	"lookalike/result"
)

// An imported package named result reaching its own Result is the package-name stutter, a different idea with a
// different repair, and the package is not a variable this code chose.
func importedPackage() int {
	var made result.Result = result.Make(1)
	return made.Result + result.Result{Result: 2}.Result
}

// A dot-imported variable is reached bare but named by its own package, so the stutter is not this code's to fix.
func dotImported() int {
	return Value.Value
}
