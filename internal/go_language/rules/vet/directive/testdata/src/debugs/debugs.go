// Package debugs is directive's fixture for the unit-isolation test (internal/go_language/check): //go:debug lines
// misplaced inside functions that call one another across two files, so a variant that stubs a callee and keeps its
// caller shows whether the caller's findings rest on its own comments alone. The package lies outside a module.
package debugs

// settled holds a //go:debug line after the package clause, where it sets nothing.
func settled() string {
	//go:debug panicnil=1
	return "settled"
}

// Summary calls settled and a function of the other file, and holds no directive itself.
func Summary() string {
	return settled() + unsettled()
}
