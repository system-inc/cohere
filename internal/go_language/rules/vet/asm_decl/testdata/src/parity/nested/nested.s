// The package's own path, spelled with the division slash the assembler writes, and with a plain slash.
TEXT parity∕nested·judged(SB),0,$0-8
	MOVW	x+0(FP), R1
	MOVB	x+0(FP), R1
	RET

TEXT parity/nested·judged(SB),0,$0-8
	MOVB	x+0(FP), R1
	RET

// Another package's path, which is not judged.
TEXT parity∕other·judged(SB),0,$0-8
	MOVB	x+0(FP), R1
	RET

TEXT ·judged(SB),0,$0-4
	MOVQ	x+0(FP), AX
	RET
