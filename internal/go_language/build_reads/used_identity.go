package build_reads

// UsedIdentity is a used entry's identity (UsedEntry): its object's, and for a selection, its Index and its receiver's,
// the zero Identity when it starts from no named type. Any other use has neither.
type UsedIdentity struct {
	Object   Identity
	Receiver Identity
	Index    string
}

// Equal reports whether used and other are the same entry's identity: their objects Equal, their indexes the same,
// and their receivers Equal, or both the zero Identity of a selection from no named type. Never when either holds an
// unknown identity.
func (used UsedIdentity) Equal(other UsedIdentity) bool {
	return used.Object.Equal(other.Object) && used.Receiver.Equal(other.Receiver) && used.Index == other.Index
}
