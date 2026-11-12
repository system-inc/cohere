// Package dormant reads nothing of another package of the module, and declares a function nothing calls, which is
// unused only if it is unused in every build.
package dormant

// Exported is used by whoever imports the package.
func Exported() int { return 1 }

// helper is called by nothing.
func helper() int { return 2 }
