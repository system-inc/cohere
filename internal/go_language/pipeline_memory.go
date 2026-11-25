package go_language

import "github.com/system-inc/cohere/internal/go_language/contract"

// memoryRecord is the types record's memory, nil in a test, which decided nothing.
func (pipeline *pipeline) memoryRecord() *contract.Memory {
	if pipeline.memory == nil {
		return nil
	}
	return &pipeline.memory.record
}

// raisesRecord is the run's memory record, each limit its loads' raises set, in order, once every load has ended
// (#5shepvt). A run that raises nothing, as a test that decided nothing, writes it with none.
func (pipeline *pipeline) raisesRecord() contract.MemoryRecord {
	return contract.MemoryRecord{RaisedToBytes: pipeline.loads.raisedTo()}
}
