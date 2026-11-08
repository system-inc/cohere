// The ppc64le rules: the size letter under the standard suffixes, R3 and F1 as result registers and SYSCALL as
// writing one, and the stack pointer R1 with a link register saved above a frame.

TEXT ·quads(SB),0,$0-32
	MOVD	x+0(FP), R3
	MOVW	x+0(FP), R3
	MOVWZ	x+0(FP), R3
	MOVHZU	y+8(FP), R3
	MOVBZ	y+8(FP), R3
	MOVDBR	y+8(FP), R3
	MOVWBR	d+16(FP), R3
	FMOVD	d+16(FP), F1
	FMOVS	d+16(FP), F1
	MOVDU	p+24(FP), R3
	ADD	p+24(FP), R3
	MOVD	16(R1), R3
	MOVD	24(R1), R3
	MOVD	48(R1), R3
	MOVD	56(R1), R3
	RET

TEXT ·abiInternal<ABIInternal>(SB),NOSPLIT,$0
	MOVD	$1, R3
	RET

TEXT ·abiInternalMissing<ABIInternal>(SB),NOSPLIT,$0
	MOVD	$1, R4
	RET

TEXT ·abiInternalInstruction<ABIInternal>(SB),NOSPLIT,$0
	SYSCALL
	RET
