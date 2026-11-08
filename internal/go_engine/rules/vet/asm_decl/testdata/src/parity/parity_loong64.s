// The loong64 rules: MOVB, MOVH, MOVW, MOVF, MOVV and MOVD by their own sizes, R4 and F0 as result registers and
// SYSCALL as writing one, and the stack pointer R3 with a link register saved above a frame.

TEXT ·quads(SB),0,$0-32
	MOVV	x+0(FP), R19
	MOVW	x+0(FP), R19
	MOVD	d+16(FP), F1
	MOVF	d+16(FP), F1
	MOVBU	y+8(FP), R19
	MOVV	16(R3), R19
	MOVV	24(R3), R19
	MOVV	48(R3), R19
	MOVV	56(R3), R19
	RET

TEXT ·abiInternal<ABIInternal>(SB),NOSPLIT,$0
	MOVV	$1, R4
	RET

TEXT ·abiInternalMissing<ABIInternal>(SB),NOSPLIT,$0
	MOVV	$1, R5
	RET

TEXT ·abiInternalInstruction<ABIInternal>(SB),NOSPLIT,$0
	SYSCALL
	RET
