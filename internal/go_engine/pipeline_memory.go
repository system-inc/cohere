package go_engine

import "github.com/system-inc/cohere/internal/go_engine/contract"

// memoryRecord is the types record's memory, nil in a test, which decided nothing.
func (engine *pipeline) memoryRecord() *contract.Memory {
	if engine.memory == nil {
		return nil
	}
	return &engine.memory.record
}

// armLateRaise is a load's late raise, armed before the load starts, or nil when lateLimit says the run raises
// nothing, as in a test, which decided nothing. It sets the limit back to the one Main decided first, the cap, since
// the last load's raise may have lifted it, and every load runs under the cap.
func (engine *pipeline) armLateRaise() *lateRaise {
	if engine.memory == nil {
		return nil
	}
	limit, raises := lateLimit(*engine.memory, engine.request.switches["single-threaded"])
	if !raises {
		return nil
	}
	engine.memory.setMemoryLimit(engine.memory.record.LimitBytes)
	return newLateRaise(limit, engine.memory.setMemoryLimit, engine.memory.collecting)
}
