package fixture

// noopTracer's empty method takes a parameter, so it performs an operation as nothing and is no marker, and the file
// serves noopTracer.
type noopTracer struct{}

func (noopTracer) Trace(event string) {}
