package build_reads

// channelSlots are walks' own slots, when no load's are given (see NewWalks): a channel of as many as may run at once.
type channelSlots chan struct{}

// Acquire takes a slot, waiting until one is free.
func (slots channelSlots) Acquire() {
	slots <- struct{}{}
}

// Release gives a slot back.
func (slots channelSlots) Release() {
	<-slots
}
