// +build ppc64 ppc64le mips64le

// A build line naming several architectures, which go/build reads as the constraint too, so the file is loaded only
// for them: the one the build is for wins, and the first otherwise.

TEXT ·quads(SB),0,$0-32
	MOVD	x+0(FP), R3
	MOVW	x+0(FP), R3
	MOVV	y+8(FP), R3
	MOVD	24(R1), R3
	MOVD	24(R29), R3
	RET
