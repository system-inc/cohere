package odd_slice_argument

// evenElements marks a slice parameter its function needs of even length.
type evenElements struct{}

// AFact makes evenElements an analysis.Fact.
func (*evenElements) AFact() {
	// Nothing to do: analysis.Fact is a marker interface, and having the method is what it asks.
}

// String is the mark as the original's prints, which staticcheck's fixture expects word for word.
func (evenElements) String() string { return "needs even elements" }
