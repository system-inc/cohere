package intermediate_representation

// IsTrivial reports whether function does no real work: every instruction it holds is a panic, a return, a jump, a
// MakeInterface, an allocation, a load from a global, or a static call to another trivial function. It is
// staticcheck's irutil.IsTrivial (honnef.co/go/tools@v0.8.1 go/ir/irutil/stub.go), translated onto our IR, so a
// reader moving from go/ir judges the same functions trivial through it.
func IsTrivial(function *Function) bool {
	return isTrivial(function, nil)
}

// isTrivial is IsTrivial for a function reached through the calls of those in seen, which it takes as nontrivial
// when met again, since mutual recursion is work.
func isTrivial(function *Function, seen map[*Function]struct{}) bool {
	if function.Source() == nil {
		// A function with no syntax, as an external one is, may do anything, so it is taken as nontrivial.
		return false
	}
	if _, isSeen := seen[function]; isSeen {
		return false
	}
	// The function joins seen only when it makes a call, so a function that makes none allocates no map.
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			switch typed := instruction.(type) {
			case *Panic:
				// What it panics with is a constant, a parameter, or a value the other cases allow.
			case *Return:
				// What it returns is a constant, a parameter, or a value the other cases allow.
			case *Jump:
				// With no other work in the function, a jump only reaches the exit block, or another trivial one.
			case *MakeInterface:
				// It can only wrap what the other cases allow.
			case *Allocation:
				// Allocating is trivial when all the function does with it is return it, maybe in an interface.
			case *Load:
				if _, isGlobal := typed.X.(*Global); !isGlobal {
					return false
				}
			case *Call:
				if seen == nil {
					seen = make(map[*Function]struct{})
				}
				seen[function] = struct{}{}
				callee := typed.Call.StaticCallee()
				if callee == nil || !isTrivial(callee, seen) {
					return false
				}
			default:
				// Every other instruction is taken to do real work.
				return false
			}
		}
	}
	return true
}
