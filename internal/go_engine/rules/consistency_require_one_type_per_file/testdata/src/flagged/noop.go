package flagged

// A null object performs an operation as nothing, which is behavior rather than membership, so an empty method that
// takes a parameter is no marker. Each no-op here is a type with methods, named or blank parameter alike, and the
// file serves the first and two more.
type Event struct {
	name string
}

type noopTracer struct{}

func (noopTracer) Trace(event Event) {}

type noopMeter struct{} // want "already serves `noopTracer`, and `noopMeter` is a second type with methods"

func (noopMeter) Record(name string, value int) {}

type noopLogger struct{} // want "already serves `noopTracer`, and `noopLogger` is a second type with methods"

func (*noopLogger) Log(_ string) {}
