package go_engine

import "github.com/system-inc/cohere/internal/go_engine/contract"

// memoryRecord is the types record's memory, nil in a test, which decided nothing.
func (engine *pipeline) memoryRecord() *contract.Memory {
	if engine.memory == nil {
		return nil
	}
	return &engine.memory.record
}
