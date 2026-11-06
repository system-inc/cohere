package micromark

// storeInfo is upstream's Info: where events were when an attempt started, and how to go back. Upstream's
// restore is a closure over the saved state; here the state is the struct's own fields and restore a method,
// so taking one allocates only the copy of the stack it must keep.
type storeInfo struct {
	from int

	context          *TokenizeContext
	point            Point
	previous         Code
	currentConstruct *Construct
	stack            []*Token
}

// restore puts the tokenizer back where store found it.
func (info *storeInfo) restore() {
	context := info.context
	context.point = info.point
	context.Previous = info.previous
	context.CurrentConstruct = info.currentConstruct
	context.Events = context.Events[:info.from]
	// Copied into the stack's own array rather than taking the saved copy's: that copy has no room past
	// its length, so the next enter would reallocate it, after every attempt that fails (#vbjv3d6).
	context.stack = append(context.stack[:0], info.stack...)
	context.accountForPotentialSkip()
}
