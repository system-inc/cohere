package allowed

import "testing"

// One test helper with methods beside a plain struct and a test serves one type.
type fixture struct {
	graph *Graph
}

func (fixture *fixture) connect(from string, to string) {
	fixture.graph.Connect(from, to)
}

type edge struct {
	from string
	to   string
}

func TestConnect(test *testing.T) {
	test.Parallel()
	subject := &fixture{graph: NewGraph()}
	for _, connection := range []edge{{from: "a", to: "b"}} {
		subject.connect(connection.from, connection.to)
	}
}
