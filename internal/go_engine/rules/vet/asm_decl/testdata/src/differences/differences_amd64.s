// Each `port finds:` record is a finding the port reports on its line, every one of them and nothing else. The
// original crashes before it reads a line of this file, laying out the frames of the declarations go/types cannot
// measure, so nothing here is compared against it.

// The functions whose frames go/types cannot measure are not judged at all: no argument size, no reference, no
// stack pointer, no `RET`, and no missing declaration.
TEXT ·value(SB),0,$0-99
	MOVW	x+0(FP), R1
	MOVQ	y+8(FP), AX
	MOVQ	8(SP), AX
	MOVQ	16(FP), AX
	RET

TEXT ·array(SB),0,$0-99
	MOVB	x_0+0(FP), AX
	MOVQ	x+3(FP), AX
	RET

TEXT ·wrapped(SB),0,$0-99
	MOVB	x_second+8(FP), AX
	RET

TEXT ·result(SB),0,$0-99
	MOVQ	16(SP), AX
	RET

TEXT ·value<ABIInternal>(SB),NOSPLIT,$0
	RET

TEXT ·earlier(SB),0,$0-99
	MOVW	x+0(FP), R1
	RET

// The functions whose frames it can measure, in the same file and after the unjudged ones, are judged in full.
TEXT ·pointer(SB),0,$0-8 // port finds: [amd64] pointer: wrong argument size 8; expected $...-32
	MOVW	x+0(FP), R1 // port finds: [amd64] pointer: invalid MOVW of x+0(FP); *T is 8-byte value
	MOVQ	l_len+16(FP), AX
	MOVQ	l_cap+16(FP), AX // port finds: [amd64] pointer: invalid offset l_cap+16(FP); expected l_cap+24(FP)
	RET

TEXT ·plain(SB),0,$0-8 // port finds: [amd64] plain: wrong argument size 8; expected $...-16
	MOVW	x+0(FP), R1 // port finds: [amd64] plain: invalid MOVW of x+0(FP); int64 is 8-byte value
	RET // port finds: [amd64] plain: RET without writing to 8-byte ret+8(FP)

TEXT ·later(SB),0,$0-99 // port finds: [amd64] later: wrong argument size 99; expected $...-1
	MOVW	x+0(FP), R1 // port finds: [amd64] later: invalid MOVW of x+0(FP); int8 is 1-byte value
	RET

TEXT ·undeclared(SB),0,$0-8 // port finds: [amd64] undeclared: function undeclared missing Go declaration
	RET
