package check

// channelStringer is a chan that is a fmt.Stringer, so a fact's interface field can hold a value gob cannot carry.
type channelStringer chan int

// String names it.
func (channelStringer) String() string { return "a channel" }
