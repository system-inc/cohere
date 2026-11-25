// The arm64 rules: no table of instructions, so only the suffix-free references, R0 and F0 as result registers and
// SVC as writing one, and the stack pointer RSP with a link register saved above a frame.

TEXT ·quads(SB),0,$0-32
	MOVD	x+0(FP), R1
	MOVW	x+0(FP), R1
	MOVD	y+4(FP), R1
	FMOVD	d+16(FP), F1
	MOVD	8(RSP), R1
	MOVD	16(RSP), R1
	MOVD	24(RSP), R1
	MOVD	48(RSP), R1
	MOVD	56(RSP), R1
	MOVD	8(SP), R1
	RET

TEXT ·abiInternal<ABIInternal>(SB),NOSPLIT,$0
	MOVD	$1, R0
	RET

TEXT ·abiInternalMissing<ABIInternal>(SB),NOSPLIT,$0
	MOVD	$1, R2
	RET

TEXT ·abiInternalInstruction<ABIInternal>(SB),NOSPLIT,$0
	MOVD	$1, R2
	SVC
	RET

TEXT ·abiInternalInstruction<ABIInternal>(SB),NOSPLIT,$0
	FMOVD	$1.0, F0
	RET
