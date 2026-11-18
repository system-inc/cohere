package build_reads

// PartUse is one use a part of a declaration made, packed: its entry in its package's record, which the record reads
// (FunctionUses.Used), and every kind of use the part made of it. A part holds each entry once.
type PartUse uint32

// partUseKindBits are the low bits a PartUse holds its kinds in, partUseSelected the bit that says its entry is a
// selection, and partUseEntryShift where its entry's index starts, which leaves an index 26 bits.
const (
	partUseKindBits   = 1<<5 - 1
	partUseSelected   = 1 << 5
	partUseEntryShift = 6
	partUseEntryLimit = 1 << (32 - partUseEntryShift)
)

// Kinds are every kind of use the part made of the use's entry.
func (use PartUse) Kinds() UseKinds {
	return UseKinds(use & partUseKindBits)
}
