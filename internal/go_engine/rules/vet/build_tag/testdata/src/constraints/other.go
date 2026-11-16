package constraints

/*
//go:build !windows
*/

// fromOther follows a misplaced //go:build line too, and calls back into the first file.
func fromOther() int {
	if misplacedPlusBuild() > 0 {
		return 3
	}
	return 0
}

// unconstrained calls back into the first file, and no build line is near it.
func unconstrained() int {
	return Total()
}
