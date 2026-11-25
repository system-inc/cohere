package parity

// A method declared in another file than its type is judged with the type.
func (self declaredElsewhere) elsewhere() {}

// A type declared after the methods that use it is judged all the same.
func (this laterType) early() {}

type laterType struct{}
