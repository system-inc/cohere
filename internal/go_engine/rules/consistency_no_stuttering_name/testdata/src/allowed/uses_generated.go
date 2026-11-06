package allowed

// A variable a generated file declares is used by hand here, and a rename by hand would be undone on the next
// generation, so the stutter is not reported where it is read either.
func readsGeneratedVariable() int {
	return Value.Value
}
