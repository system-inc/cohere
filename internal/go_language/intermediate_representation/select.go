package intermediate_representation

// The Select instruction tests whether (or blocks until) one
// of the specified sent or received states is entered.
//
// Let n be the number of States for which Direction==`RECV` and Tᵢ (0 ≤ i < n)
// be the element type of each such state's Channel.
// Select returns an n+2-tuple
//
//	(index int, recvOk bool, r₀ T₀, ... rₙ-1 Tₙ-1)
//
// The tuple's components, described below, must be accessed via the
// Extract instruction.
//
// If Blocking, select waits until exactly one state holds, i.e. a
// channel becomes ready for the designated operation of sending or
// receiving; select chooses one among the ready states
// pseudorandomly, performs the send or receive operation, and sets
// 'index' to the index of the chosen channel.
//
// If !Blocking, select doesn't block if no states hold; instead it
// returns immediately with index equal to -1.
//
// If the chosen channel was used for a receive, the rᵢ component is
// set to the received value, where i is the index of that state among
// all n receive states; otherwise rᵢ has the zero value of type Tᵢ.
// Note that the receive index i is not the same as the state
// index index.
//
// The second component of the triple, recvOk, is a boolean whose value
// is true iff the selected operation was a receive and the receive
// successfully yielded a value.
//
// Pos() returns the ast.SelectStmt.Select.
//
// Example printed form:
//
//	t3 = select nonblocking [<-t0, t1<-t2]
//	t4 = select blocking []
type Select struct {
	register
	States   []*SelectState
	Blocking bool
}

func (instruction *Select) Operands(rands []*Value) []*Value {
	operands := rands
	for index := range instruction.States {
		operands = append(operands, &instruction.States[index].Channel, &instruction.States[index].Send)
	}
	return operands
}
