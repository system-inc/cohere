// The mips64le rules: MOVB, MOVH, MOVW, MOVF, MOVV and MOVD by their own sizes, 8-byte words, and the stack pointer
// R29 with a link register saved above a frame.

TEXT ·quads(SB),0,$0-32
	MOVV	x+0(FP), R1
	MOVW	x+0(FP), R1
	MOVWU	x_lo+0(FP), R1
	MOVD	d+16(FP), F1
	MOVF	d+16(FP), F1
	MOVV	p+24(FP), R1
	MOVH	y+8(FP), R1
	MOVV	16(R29), R1
	MOVV	24(R29), R1
	MOVV	48(R29), R1
	MOVV	56(R29), R1
	RET

TEXT ·text(SB),0,$0-24
	MOVV	s+0(FP), R1
	MOVW	s_len+8(FP), R1
	MOVV	R1, n+16(FP)
	RET
