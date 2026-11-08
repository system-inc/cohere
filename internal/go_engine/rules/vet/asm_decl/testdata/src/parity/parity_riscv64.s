// The riscv64 rules: no table of instructions, X10 and F10 as result registers and ECALL as writing one, and the
// stack pointer SP with a link register saved above a frame.

TEXT ·quads(SB),0,$0-32
	MOV	x+0(FP), X5
	MOVW	x+0(FP), X5
	MOVD	d+16(FP), F1
	MOV	16(SP), X5
	MOV	24(SP), X5
	MOV	48(SP), X5
	MOV	56(SP), X5
	RET

TEXT ·abiInternal<ABIInternal>(SB),NOSPLIT,$0
	MOV	$1, X10
	RET

TEXT ·abiInternalMissing<ABIInternal>(SB),NOSPLIT,$0
	MOV	$1, X11
	RET

TEXT ·abiInternalInstruction<ABIInternal>(SB),NOSPLIT,$0
	ECALL
	RET

TEXT ·abiInternalInstruction<ABIInternal>(SB),NOSPLIT,$0
	FMOVD	$1.0, F10
	RET
