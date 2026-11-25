// The amd64 rules: LEAQ, MOVO and the SSE suffixes, AX and X0 as result registers and SYSCALL as writing one,
// and the stack pointer SP with no link register.

TEXT ·quads(SB),0,$0-32
	MOVQ	x+0(FP), AX
	MOVL	x_lo+0(FP), AX
	LEAQ	d+16(FP), AX
	LEAQ	y+9(FP), AX
	MOVO	x+0(FP), X0
	MOVSD	d+16(FP), X0
	MOVSS	d+16(FP), X0
	PEXTRD	$0, X0, x+0(FP)
	SETNE	y+8(FP)
	MOVQ	8(SP), AX
	MOVQ	16(SP), AX
	MOVQ	40(SP), AX
	MOVQ	48(SP), AX
	RET

TEXT ·abiInternal<ABIInternal>(SB),NOSPLIT,$0
	MOVQ	$1, AX
	RET

TEXT ·abiInternalMissing<ABIInternal>(SB),NOSPLIT,$0
	MOVQ	$1, BX
	RET
	RET

TEXT ·abiInternalInstruction<ABIInternal>(SB),NOSPLIT,$0
	MOVQ	$1, CX
	PXOR	X0, X0
	RET

TEXT ·abiInternalInstruction<ABIInternal>(SB),NOSPLIT,$0
	MOVQ	$1, CX
	SYSCALL
	RET

TEXT ·unnamedResult<ABIInternal>(SB),NOSPLIT,$0-16
	MOVQ	$1, CX
	RET
