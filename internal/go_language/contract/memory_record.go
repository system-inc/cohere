package contract

// MemoryRecord is additive within contract 3, and the Go engine's alone: the memory limits the run raised its heap to
// (#5shepvt). Every load runs under the cap, and once a load ends with no collection in flight the engine may raise the
// limit once, until the next load starts (#44n0vap). The types record's memory is written after the first load, before
// any raise lands, so it says the limit the run started under, and this record, written after every load has ended,
// says each limit the run went above it to.
//
// It is written once, in every check run whose load ran, as the inputs record is, and just before it.
type MemoryRecord struct {
	// RaisedToBytes is each limit the run raised to, in the order raised, one per raise that landed. Written `[]` when
	// nothing raised (see MarshalJSON).
	RaisedToBytes []int64 `json:"raisedToBytes"`
}

func (MemoryRecord) kind() Kind { return KindMemory }

// MarshalJSON writes no raises as `[]`, never `null`, so a reader never has to tell "nothing raised" from "not
// recorded".
func (record MemoryRecord) MarshalJSON() ([]byte, error) {
	// withoutMarshaler has MemoryRecord's fields and not its methods, so encoding it does not recurse here.
	type withoutMarshaler MemoryRecord
	declared := withoutMarshaler(record)
	if declared.RaisedToBytes == nil {
		declared.RaisedToBytes = []int64{}
	}
	return encodeWithoutHtmlEscaping(declared)
}
