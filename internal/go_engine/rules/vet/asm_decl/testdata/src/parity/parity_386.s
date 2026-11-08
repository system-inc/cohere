// The 386 rules: 4-byte words, an 8-byte value split into halves low first, FMOVLP and LEAL, and the stack
// pointer SP with no link register.

TEXT ·quads(SB),0,$0-28
	MOVL	x_lo+0(FP), AX
	MOVL	x_hi+4(FP), AX
	MOVL	x+0(FP), AX
	MOVQ	x+0(FP), AX
	MOVL	y_hi+12(FP), AX
	MOVL	y_hi+8(FP), AX
	FMOVLP	F0, d+16(FP)
	FMOVLP	d+16(FP), F0
	FMOVDP	F0, d+16(FP)
	LEAL	d+16(FP), AX
	MOVL	p+24(FP), AX
	MOVL	8(SP), AX
	MOVL	4(SP), AX
	MOVL	12(SP), AX
	MOVL	36(SP), AX
	RET

TEXT ·text(SB),0,$4-12
	MOVL	s+0(FP), AX
	MOVL	s_len+4(FP), AX
	MOVL	AX, n+8(FP)
	MOVL	12(SP), AX
	RET

TEXT ·complexes(SB),0,$0-24
	MOVSS	c_real+0(FP), X0
	MOVSD	c+0(FP), X0
	MOVSD	d_real+8(FP), X0
	RET

TEXT ·abiInternal<ABIInternal>(SB),NOSPLIT,$0
	MOVL	$1, AX
	RET
