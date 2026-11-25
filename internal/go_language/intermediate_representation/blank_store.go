package intermediate_representation

// The BlankStore instruction is emitted for assignments to the blank
// identifier.
//
// BlankStore is a pseudo-instruction: it has no dynamic effect.
//
// Pos() returns NoPos.
//
// Example printed form:
//
//	BlankStore t1
type BlankStore struct {
	anInstruction
	Value Value
}

func (store *BlankStore) Operands(rands []*Value) []*Value {
	return append(rands, &store.Value)
}
