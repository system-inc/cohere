package go_engine

import (
	"runtime/debug"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// memoryRecord is the types record's memory, nil in a test, which decided nothing.
func (engine *pipeline) memoryRecord() *contract.Memory {
	if engine.memory == nil {
		return nil
	}
	return &engine.memory.record
}

// armLateRaise is the run's late raise, armed before its load, or nil when lateLimit says it raises nothing, as in a
// test, which decided nothing.
func (engine *pipeline) armLateRaise() *lateRaise {
	if engine.memory == nil {
		return nil
	}
	limit, raises := lateLimit(*engine.memory, engine.request.switches["single-threaded"])
	if !raises {
		return nil
	}
	return newLateRaise(limit, debug.SetMemoryLimit, collectorMarking)
}
