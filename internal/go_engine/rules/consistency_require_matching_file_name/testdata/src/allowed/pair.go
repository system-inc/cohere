package allowed

// Two types with methods in one file are the one type per file rule's finding, and which one the file should be named
// for is a guess until the split, so neither is judged here.
type Left struct{ value int }

func (left Left) Value() int {
	return left.value
}

type Right struct{ value int }

func (right Right) Value() int {
	return right.value
}
