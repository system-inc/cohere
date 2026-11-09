package static_single_assignment

// The strongly connected step on its own, for the external tests. Compiled into the package's tests only.

// EliminateRedundantComponentsOnceForTest runs the strongly connected step once over function, without the
// trivial rule or the fixed point EliminateRedundantPhis wraps it in, so a test can see what one pass collapses:
// a component whose outside operand an earlier component's collapse rewrote collapses in the same pass, which
// the fixed point would otherwise hide behind a second round.
func EliminateRedundantComponentsOnceForTest[G Graph[F, B, P], F any, B comparable, P any](
	graph G,
	function F,
) bool {
	return eliminateRedundantComponents(graph, function)
}
