package parity

// A type a test file declares is a member of the package's test variant, judged there.
type testType struct{}

func (self testType) inTest() {}

// A method a test file declares on a type of the package is judged there too.
func (this valueType) inTestOnValueType() {}
