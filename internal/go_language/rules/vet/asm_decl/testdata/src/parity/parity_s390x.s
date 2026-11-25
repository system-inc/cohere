// The s390x rules: MOVB, MOVH, MOVW, MOVD and their zero-extending forms, FMOVS and FMOVD, big endian with
// 8-byte words, and the stack pointer R15 with a link register saved above a frame.

TEXT ·quads(SB),0,$0-32
	MOVD	x+0(FP), R1
	MOVW	x+0(FP), R1
	MOVWZ	x+0(FP), R1
	MOVHZ	y+8(FP), R1
	MOVBZ	y+8(FP), R1
	FMOVD	d+16(FP), F1
	FMOVS	d+16(FP), F1
	MOVD	p+24(FP), R1
	MOVD	16(R15), R1
	MOVD	24(R15), R1
	MOVD	48(R15), R1
	MOVD	56(R15), R1
	RET

TEXT ·abiInternal<ABIInternal>(SB),NOSPLIT,$0
	MOVD	$1, R2
	RET
