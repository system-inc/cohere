// A header beside the assembly is among the package's other files, and is never judged.
TEXT ·header(SB), 0, $0
	MOVQ	AX, BP
	MOVD	R1, R29
	RET
