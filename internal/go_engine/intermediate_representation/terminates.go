package intermediate_representation

import "go/types"

// Terminates reports whether function is supposed to return: whether it has at least one theoretic path that
// returns from it. An explicit panic does not count as returning, nor does leaving a range over time.Tick's
// channel, which is never closed. A function with no blocks, as an external one has, is taken to terminate, the
// conservative choice. It is staticcheck's irutil.Terminates (honnef.co/go/tools@v0.8.1
// go/ir/irutil/terminates.go), translated, not copied, onto our IR, so a reader moving from go/ir gets the same
// answer through it.
func Terminates(function *Function) bool {
	if function.Blocks == nil {
		return true
	}

	for _, block := range function.Blocks {
		if _, isReturn := block.Control().(*Return); isReturn {
			if len(block.Predecessors) == 0 {
				return true
			}
			for _, predecessor := range block.Predecessors {
				switch control := predecessor.Control().(type) {
				case *Panic:
					// An explicit panic does not count as terminating.
				case *If:
					// Arriving here by receiving from a closed time.Tick channel cannot happen at run time, so
					// it is not termination; any other If is.
					//
					// irutil tests ok here, which is still its Return assertion's and so always true: this
					// branch never returns. Kept, so the translation is irutil's branch for branch.
					if !isReturn {
						return true
					}
					extract, isExtract := control.Condition.(*Extract)
					if !isExtract {
						return true
					}
					if extract.Index != 1 {
						return true
					}
					receive, isReceive := extract.Tuple.(*Receive)
					if !isReceive {
						return true
					}
					call, isCall := receive.Channel.(*Call)
					if !isCall {
						return true
					}
					callee, isFunction := call.Common().Value.(*Function)
					if !isFunction {
						return true
					}
					calleeObject, isFunc := callee.Object().(*types.Func)
					if !isFunc {
						return true
					}
					if calleeObject.FullName() != "time.Tick" {
						return true
					}
				default:
					// The exit block is reached some other way.
					return true
				}
			}
		}
	}
	return false
}
