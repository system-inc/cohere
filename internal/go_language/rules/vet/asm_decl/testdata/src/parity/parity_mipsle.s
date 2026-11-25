// The mipsle rules: MOVB, MOVH, MOVW, MOVF and MOVD by their own sizes, 4-byte words with an 8-byte value's halves
// low first, and the stack pointer R29 with a link register saved above a frame.

TEXT ·quads(SB),0,$0-28
	MOVW	x_lo+0(FP), R1
	MOVW	x_hi+4(FP), R1
	MOVW	x_lo+4(FP), R1
	MOVW	x+0(FP), R1
	MOVD	x+0(FP), F1
	MOVD	d+16(FP), F1
	MOVF	d+16(FP), F1
	MOVF	d_lo+16(FP), F1
	MOVHU	y_lo+8(FP), R1
	MOVBU	p+24(FP), R1
	MOVWU	p+24(FP), R1
	MOVV	p+24(FP), R1
	MOVW	8(R29), R1
	MOVW	12(R29), R1
	MOVW	36(R29), R1
	MOVW	40(R29), R1
	RET

TEXT ·text(SB),0,$8-12
	MOVW	s_base+0(FP), R1
	MOVW	s_len+4(FP), R1
	MOVW	R1, n+8(FP)
	MOVW	16(R29), R1
	RET
