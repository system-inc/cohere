// The last line holds the write and no line feed ends it, judged on every architecture.

TEXT ·unterminated(SB), 0, $0
	MOVD	$0, R1
	MOVQ	AX, BP
	MOVD	R1, R29