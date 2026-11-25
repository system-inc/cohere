package flagged

// Unlike a marker variant beside its interface, a no-op whose empty method takes a parameter performs an operation as
// nothing, which is behavior, so this file serves noopTracer and is named for its interface instead.
type Tracer interface {
	Trace(event string)
}

type noopTracer struct{} // want "`noopTracer` is the one type .* in `tracer.go`, .* `noop_tracer.go`.$"

func (noopTracer) Trace(event string) {}
