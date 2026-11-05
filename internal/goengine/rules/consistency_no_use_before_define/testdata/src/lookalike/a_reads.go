package lookalike

import "strconv"

// This file and b_declares.go each read a name the other declares, so whichever the loader places first, one of the
// two references comes before its declaration in the package, and a reference from another file is not judged.
func fromOtherFile() int {
	return declaredElsewhere + elsewhereCeiling
}

// A local, a parameter and a struct field share a later package name, and resolve to themselves.
type box struct{ limit int }

func shadowed(limit int) int {
	ceiling := limit * 2
	item := box{limit: ceiling}
	return item.limit + item.size()
}

// A method shares the name of a later const, and is no reference to it.
func (item box) size() int {
	return item.limit
}

// Another package's names live in its own scope.
func width() int {
	return strconv.IntSize
}

var declaredFirst = 7

var limit = 1

var ceiling = 2

const size = 3
