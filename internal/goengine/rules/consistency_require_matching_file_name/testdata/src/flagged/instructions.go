package flagged

// The variants serve nothing, so this file serves Machine alone, and is named for its interface instead.
type Operation interface {
	operationValue()
}

type Push struct {
	value int
}

func (Push) operationValue() {}

type Pop struct{}

func (*Pop) operationValue() {}

type Machine struct { // want "`Machine` is the one type .* Name it `machine.go`.$"
	operations []Operation
}

func (machine *Machine) Run(operation Operation) {
	machine.operations = append(machine.operations, operation)
}
