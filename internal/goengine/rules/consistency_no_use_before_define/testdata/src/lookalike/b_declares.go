package lookalike

// Read by a function in a_reads.go, and reading one that file declares. Each file reads the other, so whichever the
// loader places first, one of them refers to a name at a later position, and a reference from another file is still
// not judged.
var declaredElsewhere = 5

const elsewhereCeiling = 6

func fromFirstFile() int {
	return declaredFirst
}
