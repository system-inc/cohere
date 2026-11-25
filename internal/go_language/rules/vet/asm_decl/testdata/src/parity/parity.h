// A header is among the package's other files, and is never judged.
TEXT ·undeclaredInAHeader(SB),0,$0-8
	MOVW	x+0(FP), R1
	RET
