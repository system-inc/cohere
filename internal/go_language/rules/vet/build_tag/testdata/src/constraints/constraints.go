// Package constraints is buildtag's fixture for the unit-isolation test (internal/go_language/check): build constraint
// lines misplaced after the package clause, beside functions that call one another across two files, so a variant
// that stubs some of the functions shows whether the findings rest on the file's own comments alone. Each misplaced
// line sits in a block comment between declarations, at the file's level: gofmt moves a build line written as a line
// comment, inside a function or out, to the file's header, and indents a block comment's lines inside a function,
// where buildtag no longer reads them as build lines. The package lies outside a module.
package constraints

/*
//go:build linux
*/

// misplacedGoBuild follows a //go:build line in a block comment after the package clause, where it constrains
// nothing.
func misplacedGoBuild() int {
	return 1
}

/*
// +build linux
*/

// misplacedPlusBuild follows a +build line in a block comment after the package clause, where it constrains nothing.
func misplacedPlusBuild() int {
	return 2
}

// Total calls both of the above and a function of the other file.
func Total() int {
	return misplacedGoBuild() + misplacedPlusBuild() + fromOther()
}
