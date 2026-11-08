#define LATE_BUILD_LINE 1

// +build arm mipsle

// A build line after the first line of code is no constraint to go/build, which loads the file for every target, but
// the pass reads it anywhere before the first `TEXT` line: the architecture the build is for wins when it names it,
// and arm, the first, otherwise. A build line after that one is not read.

// +build s390x

TEXT ·quads(SB),0,$0-28
	MOVW	x_lo+0(FP), R1
	MOVW	x_hi+0(FP), R1
	MOVH	x+0(FP), R1
	MOVW	8(R13), R1
	MOVW	8(R29), R1
	RET
