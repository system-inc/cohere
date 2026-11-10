package go_engine

import "github.com/system-inc/cohere/internal/go_engine/contract"

// memoryRecord is the types record's memory, nil in a test, which decided nothing.
func (engine *pipeline) memoryRecord() *contract.Memory {
	if engine.memory == nil {
		return nil
	}
	return &engine.memory.record
}

// raisesRecord is the run's memory record, each limit its loads' raises set, in order, once every load has ended
// (#5shepvt). A run that raises nothing, as a test that decided nothing, writes it with none.
func (engine *pipeline) raisesRecord() contract.MemoryRecord {
	return contract.MemoryRecord{RaisedToBytes: engine.loads.raisedTo()}
}
