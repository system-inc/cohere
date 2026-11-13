package layout

// NewHolder builds a Holder, a composite literal that touches its underlying: dependent.
func NewHolder() *Holder { return &Holder{} }

// Capacity reads the length of Holder's slots, which hangs on Size: dependent.
func (holder *Holder) Capacity() int { return len(holder.slots) }
