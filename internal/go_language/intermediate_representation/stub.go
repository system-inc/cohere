package intermediate_representation

// IsStub is whether function does no work of its own: it holds no instruction but returns, panics, jumps,
// allocations and interfaces made of those, so all it can do is return or panic with a constant, a new allocation or
// what it was given. A function with no syntax, one built from types alone, is never a stub, since its work cannot be
// seen. It is staticcheck's irutil.IsStub
// (honnef.co/go/tools@v0.8.1 go/ir/irutil/stub.go), translated onto our IR, so a reader moving from go/ir judges the
// same functions stubs.
func IsStub(function *Function) bool {
	if function.Source() == nil {
		return false
	}
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			switch instruction.(type) {
			case *Panic, *Return, *Jump:
				// A panic or a return of constants is a stub's, and a jump with nothing disallowed around it only
				// reaches the exit.
			case *MakeInterface:
				// It can only be wrapping a constant or a heap allocation here, which is fine.
			case *Allocation:
				// Allocating is fine so long as all that is done with it is returning it.
			default:
				// Every other instruction is assumed to do actual work.
				return false
			}
		}
	}
	return true
}
