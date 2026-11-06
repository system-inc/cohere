package core

import "github.com/system-inc/cohere/internal/lint/ecmascript/control_flow_graph"

// deadStoreLiveness is this rule's lattice: a set of symbols, met by union, transferred by walking
// one block's events backward.
//
// # The meet, and why union is the conservative choice here
//
// Upstream distinguishes six edge kinds (Normal, Jump, Backedge, Error, Finalize, Join) and unions
// all but Unreachable into one of two sets, keeping the error set separate so a write bypassed by a
// throw is not reported. Our graph carries no edge kinds, so the same conservatism is obtained by a
// different route: the fork to a handler is a real successor edge, so the handler's live set is
// unioned in like any other, and a write followed by a throwing call stays live because the catch
// path reads the earlier value. Measured on
// `function f() { let v = 1; try { h(); } catch (e) { v = 2; } return v; }`, where the fork out of
// the `try` block reaches the catch and the join before it.
//
// # Bottom and Entry are both empty, and they mean different things
//
// Bottom is the identity for union, so it is the empty set. Entry is the claim that nothing is live
// where the code path leaves (no symbol declared inside this root can be read after it returns),
// and it is also the empty set. They coincide numerically and not in meaning, which is why they are
// written separately rather than sharing one constructor.
type deadStoreLiveness struct {
	// reachable is keyed by source position, and `applyBlockTransfer` consults it to decide whether
	// a write is judged at all. It is threaded through the lattice because the transfer needs it and
	// the framework hands the lattice nothing but the block.
	reachable map[int]bool
	// words is how long a live set this root needs, one bit per symbol its events name.
	words int
}

// Bottom is the empty set, nil, which costs nothing. Solve asks for it for every block on every
// round, and the map it used to be was the largest single allocation in this rule (#tmn9n27).
func (deadStoreLiveness) Bottom() liveSymbols { return nil }

func (deadStoreLiveness) Entry() liveSymbols { return nil }

func (liveness deadStoreLiveness) Meet(left, right liveSymbols) liveSymbols {
	// Neither argument is written: `Solve` holds one of them as a settled boundary value and
	// compares against it to decide whether anything moved. One that already holds the other is
	// returned as it is, which is most meets, since Solve folds every block's neighbours from
	// Bottom. Returning it shares it, and that is safe because nothing writes into a live set it did
	// not just make: Transfer and the reporting walk each copy before they write.
	if left.covers(right) {
		return left
	}
	if right.covers(left) {
		return right
	}
	merged := left.withWords(liveness.words)
	for index, bits := range right {
		merged[index] |= bits
	}
	return merged
}

func (liveness deadStoreLiveness) Transfer(
	block *control_flow_graph.Block[deadStoreEvent],
	incoming liveSymbols,
) liveSymbols {
	// A block with no event passes its set through unchanged, so it is handed back rather than
	// copied.
	if len(block.Events) == 0 {
		return incoming
	}
	outgoing := incoming.withWords(liveness.words)
	// nil observer: the fixed-point rounds must report nothing, because a block is transferred many
	// times before the sets settle. Reporting happens once afterwards, in a separate walk.
	applyBlockTransfer(block, outgoing, liveness.reachable, nil)
	return outgoing
}

func (deadStoreLiveness) Equal(left, right liveSymbols) bool {
	return left.covers(right) && right.covers(left)
}
