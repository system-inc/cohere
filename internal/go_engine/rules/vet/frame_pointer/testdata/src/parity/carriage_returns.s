// Lines ending in a carriage return and a line feed, judged on every architecture.

TEXT ·carriageReturns(SB), 0, $0
	MOVQ	AX, BP
	MOVD	R1, R29
	RET
