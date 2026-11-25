// Loaded on every architecture, so each judges these lines by its own rules, and finds them on different lines.

TEXT ·acrossArchitectures(SB), 0, $0
	MOVQ	AX, BP
	MOVD	R1, R29
	RET

// B is arm64's branch and nothing on amd64.
TEXT ·branchAcrossArchitectures(SB), 0, $0
	B	skip
	MOVQ	AX, BP
	MOVD	R1, R29
skip:
	RET

// RETFL starts with RET, which stops amd64, and is not RET, which arm64 reads exactly.
TEXT ·longerReturnAcrossArchitectures(SB), 0, $0
	RETFL
	MOVD	$0, R29
	MOVQ	$0, BP
	RET

// A no-break space parts B from its label, which arm64 reads as a space, and leads the amd64 write.
TEXT ·noBreakSpace(SB), 0, $0
	B skip
 MOVQ	$0, BP
	MOVD	$0, R29
skip:
	RET

// A no-break space before BP is no space to amd64's pattern, which reads it as a read.
TEXT ·noBreakSpaceBeforeOperand(SB), 0, $0
	MOVQ	AX, BP
	MOVQ	BX, BP
	RET
