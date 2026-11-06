package main

// poolShape is the pool's tokens, and how many packages and threads each token's run may use.
type poolShape struct {
	tokens, packages, threads int
}

// widened is the shape of a run holding tokens of the pool's tokens: their cores together, split between
// packages and threads as evenly as they divide, packages the larger, so a lone long test binary gains threads
// as well as the run gaining binaries. Held to one token, it is the pool's own shape, unchanged.
func (shape poolShape) widened(tokens int) poolShape {
	if tokens <= 1 {
		return shape
	}
	cores := shape.packages * shape.threads * tokens
	threads := 1
	for candidate := 1; candidate*candidate <= cores; candidate++ {
		if cores%candidate == 0 {
			threads = candidate
		}
	}
	return poolShape{tokens: shape.tokens, packages: cores / threads, threads: threads}
}
