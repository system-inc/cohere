package lookalike

// Piston is the one type this file serves. Bolt is declared in bolt.go and only marked a Part here, and a receiver
// whose every method in the package is a marker serves nothing, as its declaration does not.
type Part interface {
	partValue()
}

type Piston struct {
	strokes int
}

func (piston *Piston) Fire() {
	piston.strokes++
}

func (*Piston) partValue() {}

func (Bolt) partValue() {}
