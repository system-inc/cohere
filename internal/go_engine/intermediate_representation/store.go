package intermediate_representation

// The Store instruction stores Value at address Address.
// Stores can be of arbitrary types.
//
// Pos() returns the position of the source-level construct most closely
// associated with the memory store operation.
// Since implicit memory stores are numerous and varied and depend upon
// implementation choices, the details are not specified.
//
// Example printed form:
//
//	*x = y
type Store struct {
	anInstruction
	Address Value
	Value   Value
	comment string // what lifting made the Store for
}

func (store *Store) Comment() string { return store.comment }

func (store *Store) Operands(rands []*Value) []*Value {
	return append(rands, &store.Address, &store.Value)
}
