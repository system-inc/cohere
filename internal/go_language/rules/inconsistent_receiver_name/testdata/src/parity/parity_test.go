package parity

// A test file's method names testedType's receiver a second way, so the test variant finds it.
func (other testedType) inTest() {}

// A type a test file declares is a member of the package's test variant, judged there.
type testType struct{}

func (value testType) first()  {}
func (other testType) second() {}
