// The arm rules: MOVD, MOVW, MOVH and MOVB by their own sizes, 4-byte words with halves low first, and the stack
// pointer R13 with a link register saved above a frame.

TEXT ·quads(SB),0,$0-28
	MOVW	x_lo+0(FP), R1
	MOVW	x_hi+4(FP), R1
	MOVW	x+0(FP), R1
	MOVD	x+0(FP), F1
	MOVD	d+16(FP), F1
	MOVH	y_lo+8(FP), R1
	MOVHU	y_hi+12(FP), R1
	MOVBU	p+24(FP), R1
	MOVB	p+24(FP), R1
	MOVW	p+24(FP), R1
	MOVW	8(R13), R1
	MOVW	4(R13), R1
	MOVW	12(R13), R1
	MOVW	36(R13), R1
	RET

TEXT ·quads(SB),NOFRAME,$0-28
	MOVW	4(R13), R1
	MOVW	8(R13), R1
	RET

TEXT ·text(SB),0,$8-12
	MOVW	s+0(FP), R1
	MOVW	s_len+4(FP), R1
	MOVW	R1, n+8(FP)
	MOVW	16(R13), R1
	MOVW	24(R13), R1
	RET

TEXT ·abiInternal<ABIInternal>(SB),NOSPLIT,$0
	MOVW	$1, R0
	RET
