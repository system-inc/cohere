//go:build amd64 || arm64 || riscv64

// A `//go:build` line is a constraint to go/build but no build line to the pass, which leaves the file's
// architecture to go/build's default.

TEXT ·quads(SB),0,$0-28
	MOVQ	x+0(FP), AX
	MOVW	x+0(FP), R1
	RET
