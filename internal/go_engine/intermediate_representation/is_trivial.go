package intermediate_representation

import (
	"slices"
)

// IsTrivial reports whether function does no real work: every instruction it holds is a panic, a return, a jump, a
// MakeInterface, an allocation, a load from a global, or a static call to another trivial function. It is
// staticcheck's irutil.IsTrivial (honnef.co/go/tools@v0.8.1 go/ir/irutil/stub.go), translated onto our IR, so a
// reader moving from go/ir judges the same functions trivial through it.
func IsTrivial(function *Function) bool {
	return isTrivial(function, nil, nil)
}

// IsTrivialEntering is IsTrivial's answer for function, with every other function whose body the walk read to give
// it, each once, in the order the walk first entered them: each static callee it followed, and each below those. A
// callee the walk refuses at once, one with no syntax or one met again, is not among them for that meeting, since the
// walk reads nothing of its body then. It is IsTrivial's own walk, which only drops the list, so the answer rests on
// the bodies of function and of these, and on no other body.
func IsTrivialEntering(function *Function) (bool, []*Function) {
	var entered []*Function
	trivial := isTrivial(function, nil, func(read *Function) {
		if read != function && !slices.Contains(entered, read) {
			entered = append(entered, read)
		}
	})
	return trivial, entered
}

// isTrivial is IsTrivial for a function reached through the calls of those in seen, which it takes as nontrivial
// when met again, since mutual recursion is work. enter, when it is not nil, hears each function as the walk enters
// its body, past the two refusals, as often as the walk enters it.
func isTrivial(function *Function, seen map[*Function]struct{}, enter func(read *Function)) bool {
	if function.Source() == nil {
		// A function with no syntax, as an external one is, may do anything, so it is taken as nontrivial.
		return false
	}
	if _, isSeen := seen[function]; isSeen {
		return false
	}
	if enter != nil {
		enter(function)
	}
	// Our IR builds a function when it is first read, so a callee no one has read yet has no blocks, and with none
	// it would read as trivial. Build is idempotent, so a function already built costs nothing here. buildir builds
	// every function up front, so irutil never meets this.
	function.Build()
	// The function joins seen only when it makes a call, so a function that makes none allocates no map. visited is
	// seen once that map exists, shared by every call this function makes and the calls below them, as irutil's is.
	visited := seen
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
				if visited == nil {
					visited = make(map[*Function]struct{})
				}
				visited[function] = struct{}{}
				callee := typed.Call.StaticCallee()
				if callee == nil || !isTrivial(callee, visited, enter) {
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
