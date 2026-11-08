package parity

// functionFirst is the first declaration of a file with no imports, a function with a doc comment, so the import of
// fmt the first fix adds goes before the comment.
func functionFirst(number int) string {
	return string(number)
}
