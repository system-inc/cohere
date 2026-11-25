package allowed

import "testing"

// A test file is named for what it tests, so a helper type with methods inside it is not judged against the name.
type fixture struct {
	graph *Graph
}

func (fixture *fixture) connect(from string, to string) {
	fixture.graph.Connect(from, to)
}

func TestConnect(test *testing.T) {
	test.Parallel()
	subject := &fixture{graph: NewGraph()}
	subject.connect("a", "b")
}
