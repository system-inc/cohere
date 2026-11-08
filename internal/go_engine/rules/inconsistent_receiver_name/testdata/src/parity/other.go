package parity

// Methods declared in another file than their type are counted with the type.
func (value declaredElsewhere) here()  {}
func (other declaredElsewhere) there() {}

// A type declared after the methods that use it is judged all the same.
func (value laterType) early() {}
func (other laterType) late()  {}

type laterType struct{}
