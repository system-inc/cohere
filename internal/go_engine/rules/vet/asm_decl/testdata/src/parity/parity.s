// parity.s names no architecture, in its name or in a build line, so each target judges it as its own, the one
// go/build's default context names. Its instructions are a mix of every architecture's, so every architecture's
// sizes and instruction table are read against the same references.

// Commented-out code is ignored:
//	TEXT ·undeclaredInAComment(SB),0,$0
//	MOVB	x+9(FP), AX

TEXT ·bytes(SB),0,$0-3
	MOVB	x+0(FP), R1
	MOVBU	y+1(FP), R1
	MOVBZ	z+2(FP), R1
	MOVH	x+0(FP), R1
	MOVHU	y+1(FP), R1
	MOVHZ	z+2(FP), R1
	MOVW	x+0(FP), R1
	MOVWU	y+1(FP), R1
	MOVWZ	z+2(FP), R1
	MOVL	x+0(FP), R1
	MOVD	y+1(FP), R1
	MOVQ	z+2(FP), AX
	MOVV	x+0(FP), R1
	MOVF	y+1(FP), F1
	FMOVS	z+2(FP), F1
	FMOVD	x+0(FP), F1
	MOVBZU	y+1(FP), R1
	MOVHBR	z+2(FP), R1
	MOVWBR	x+0(FP), R1
	MOVDU	y+1(FP), R1
	SETEQ	z+2(FP)
	MOVSS	x+0(FP), X0
	MOVSD	y+1(FP), X0
	PINSRD	$0, z+2(FP), X0
	PEXTRD	$0, X0, x+0(FP)
	FMOVLP	F0, y+1(FP)
	FMOVLP	z+2(FP), F0
	FMOVDP	F0, x+0(FP)
	FMOVFP	F0, y+1(FP)
	FXCHD	z+2(FP)
	MOVO	x+0(FP), X0
	MOVOU	y+1(FP), X0
	LEAQ	z+2(FP), AX
	LEAL	x+0(FP), AX
	MOVQ	$y+1(FP), AX
	MOVB	$z+2(FP), AX
	ADD	x+0(FP), R1
	ADDB	y+1(FP), R1
	ANDW	z+2(FP), R1
	XORL	x+0(FP), R1
	MOVB	x+1(FP), R1
	MOVB	y+0(FP), R1
	MOVB	z+3(FP), R1
	MOVB	R1, x+0(FP); MOVW y+1(FP), R2
loop:	MOVW	z+2(FP), R1
	JMP	loop
	RET

TEXT ·words(SB),NOSPLIT,$0
	MOVH	x+0(FP), R1
	MOVW	y+2(FP), R1
	MOVQ	x+0(FP), AX
	MOVH	x+2(FP), R1
	RET

TEXT ·longs(SB),7,$0-12
	MOVW	x+0(FP), R1
	MOVL	y+4(FP), R1
	FMOVS	f+8(FP), F1
	MOVSS	f+8(FP), X0
	MOVSD	f+8(FP), X0
	MOVD	f+8(FP), F1
	MOVF	f+8(FP), F1
	MOVW	f+4(FP), R1
	RET

TEXT ·quads(SB),7,$0-8
	MOVD	x+0(FP), R1
	MOVQ	y+8(FP), AX
	MOVV	d+16(FP), R1
	MOVW	x+0(FP), R1
	MOVW	x_lo+0(FP), R1
	MOVW	x_hi+4(FP), R1
	MOVW	x_lo+4(FP), R1
	MOVW	y_hi+8(FP), R1
	MOVW	y_hi+12(FP), R1
	MOVL	d_lo+16(FP), R1
	MOVL	d+16(FP), R1
	MOVQ	p+24(FP), AX
	MOVW	p+24(FP), R1
	MOVW	p+20(FP), R1
	RET

TEXT ·machine(SB),NOSPLIT,$0-16
	MOVQ	x+0(FP), AX
	MOVW	x+0(FP), R1
	MOVD	y+8(FP), R1
	MOVW	y+4(FP), R1
	MOVW	x_lo+0(FP), R1
	RET

TEXT ·pointers(SB),0,$0-40
	MOVQ	p+0(FP), AX
	MOVW	p+0(FP), R1
	MOVD	c+8(FP), R1
	MOVW	c+4(FP), R1
	MOVV	m+16(FP), R1
	MOVW	m+8(FP), R1
	MOVQ	f+24(FP), AX
	MOVW	f+12(FP), R1
	MOVD	u+32(FP), R1
	MOVW	u+16(FP), R1
	MOVB	u+16(FP), R1
	RET

TEXT ·text(SB),0,$0-24
	MOVQ	s+0(FP), AX
	MOVW	s+0(FP), R1
	MOVL	s+0(FP), R1
	MOVD	s_base+0(FP), R1
	MOVW	s_base+0(FP), R1
	MOVQ	s_len+8(FP), AX
	MOVW	s_len+4(FP), R1
	MOVQ	s+8(FP), AX
	MOVW	s+4(FP), R1
	MOVQ	s_cap+16(FP), AX
	MOVQ	AX, n+16(FP)
	MOVW	R1, n+8(FP)
	LEAQ	s+0(FP), AX
	MOVQ	$s_len+8(FP), AX
	RET

TEXT ·slices(SB),0,$0-48
	MOVQ	s+0(FP), AX
	MOVW	s+0(FP), R1
	MOVQ	s_base+0(FP), AX
	MOVQ	s_len+8(FP), AX
	MOVW	s_len+4(FP), R1
	MOVQ	s_cap+16(FP), AX
	MOVW	s_cap+8(FP), R1
	MOVQ	s+8(FP), AX
	MOVQ	t+24(FP), AX
	MOVW	t+12(FP), R1
	MOVQ	t_cap+40(FP), AX
	MOVW	t_cap+20(FP), R1
	MOVQ	t_len+24(FP), AX
	RET

TEXT ·interfaces(SB),0,$0-48
	MOVQ	e+0(FP), AX
	MOVW	e+0(FP), R1
	MOVQ	e_type+0(FP), AX
	MOVQ	e_data+8(FP), AX
	MOVW	e_data+4(FP), R1
	MOVQ	e_itable+0(FP), AX
	MOVQ	e+8(FP), AX
	MOVQ	r+16(FP), AX
	MOVW	r+8(FP), R1
	MOVQ	r_itable+16(FP), AX
	MOVQ	r_type+16(FP), AX
	MOVQ	r_data+24(FP), AX
	MOVW	r_data+12(FP), R1
	MOVQ	i+32(FP), AX
	MOVQ	i_type+32(FP), AX
	MOVW	i_data+20(FP), R1
	RET

TEXT ·complexes(SB),0,$0-24
	MOVSS	c+0(FP), X0
	MOVSD	c+0(FP), X0
	MOVW	c+0(FP), R1
	MOVD	c+0(FP), F1
	FMOVS	c_real+0(FP), F1
	FMOVD	c_real+0(FP), F1
	MOVF	c_imag+4(FP), F1
	MOVSS	c_imag+8(FP), X0
	MOVO	d+8(FP), X0
	MOVOU	c+0(FP), X0
	MOVSD	d+8(FP), X0
	MOVSD	d_real+8(FP), X0
	MOVD	d_imag+16(FP), F1
	MOVSS	d_imag+16(FP), X0
	MOVSD	d+16(FP), X0
	MOVQ	d+8(FP), AX
	RET

TEXT ·structs(SB),0,$0-56
	MOVQ	p+0(FP), AX
	MOVB	p_low+0(FP), R1
	MOVW	p_low+0(FP), R1
	MOVQ	p_wide+8(FP), AX
	MOVW	p_wide+4(FP), R1
	MOVW	p_wide_lo+4(FP), R1
	MOVQ	p_low+8(FP), AX
	MOVQ	n+16(FP), AX
	MOVQ	n_name+16(FP), AX
	MOVQ	n_name_base+16(FP), AX
	MOVW	n_name_len+20(FP), R1
	MOVQ	n_name_len+24(FP), AX
	MOVQ	n_name+24(FP), AX
	MOVB	n_inner_low+32(FP), R1
	MOVB	n_inner_low+24(FP), R1
	MOVQ	n_inner_wide+40(FP), AX
	MOVQ	n_inner+32(FP), AX
	MOVB	n_flags_0+48(FP), R1
	MOVB	n_flags_2+50(FP), R1
	MOVW	n_flags_1+49(FP), R1
	MOVB	n_flags+48(FP), R1
	MOVB	n_flags_3+51(FP), R1
	RET

TEXT ·arrays(SB),0,$0-16
	MOVH	a+0(FP), R1
	MOVH	a_0+0(FP), R1
	MOVH	a_1+2(FP), R1
	MOVW	a_1+2(FP), R1
	MOVH	a_1+4(FP), R1
	MOVB	b_0_0+4(FP), R1
	MOVB	b_1_2+9(FP), R1
	MOVW	b_1_2+9(FP), R1
	MOVB	b_1+7(FP), R1
	MOVB	b+4(FP), R1
	MOVQ	e+16(FP), AX
	MOVB	after+16(FP), R1
	MOVB	after+10(FP), R1
	MOVB	after+12(FP), R1
	RET

TEXT ·named(SB),0,$0-12
	MOVSD	c+0(FP), X0
	MOVSS	c+0(FP), X0
	FMOVD	c+0(FP), F1
	MOVW	c_lo+0(FP), R1
	MOVL	a+8(FP), R1
	MOVW	a+8(FP), R1
	MOVQ	a+8(FP), AX
	RET

TEXT ·variadic(SB),0,$0-40
	MOVQ	prefix+0(FP), AX
	MOVQ	rest+16(FP), AX
	MOVQ	rest_base+16(FP), AX
	MOVQ	rest_len+24(FP), AX
	MOVW	rest_cap+32(FP), R1
	MOVQ	rest_len+16(FP), AX
	MOVW	rest+8(FP), R1
	RET

TEXT ·unnamed(SB)
	MOVB	arg+0(FP), R1
	MOVQ	arg1+8(FP), AX
	MOVW	arg1+4(FP), R1
	MOVL	ret+16(FP), R1
	MOVQ	ret1+24(FP), AX
	MOVW	ret1+12(FP), R1
	MOVW	ret1_len+16(FP), R1
	MOVB	arg2+0(FP), R1
	RET

TEXT ·unnamedResult(SB),0,$0-16
	MOVQ	x+0(FP), AX
	CMPQ	AX, $0
	JEQ	zero
	RET
zero:
	RET	·nothing(SB)
	RET

TEXT ·unnamedResult(SB),0,$0-16
	MOVQ	AX, ret+8(FP)
	RET

TEXT ·unnamedResult(SB),0,$0-16
	MOVW	R1, ret+4(FP)
	RET

TEXT ·unnamedResults(SB),0,$0-24
	MOVQ	AX, ret1+16(FP)
	RET

TEXT ·unnamedResults(SB),0,$0-24
	MOVQ	AX, ret+8(FP)
	RET

TEXT ·namedResults(SB),0,$0-17
	MOVQ	x+0(FP), AX
	RET

TEXT ·namedResults(SB),0,$0-17
	MOVQ	AX, sum+8(FP)
	MOVB	R1, ok+16(FP)
	MOVW	R1, ok+8(FP)
	MOVL	R1, ok+12(FP)
	RET

TEXT ·returnsString(SB),0,$0-16
	MOVQ	AX, ret_len+8(FP)
	RET

TEXT ·returnsString(SB),0,$0-16
	MOVQ	ret_base+0(FP), AX
	RET

TEXT ·returnsString(SB),0,$0-16
	MOVW	R1, ret_len+4(FP)
	RET

TEXT ·returnsStruct(SB),0,$0-16
	MOVB	R1, ret_low+0(FP)
	RET

TEXT ·returnsStruct(SB),0,$0-16
	MOVQ	AX, ret_wide+8(FP)
	RET

TEXT ·returnsStruct(SB),0,$0-16
	RET

TEXT ·returnsComplex(SB),0,$0-16
	MOVSD	X0, ret_imag+8(FP)
	RET

TEXT ·returnsComplex(SB),0,$0-16
	MOVO	X0, ret+0(FP)
	RET

TEXT ·returnsComplex(SB),0,$0-16
	RET
	RET

TEXT ·nothing(SB),0,$0-0
	RET

TEXT ·nothing(SB),0,$0-8
	RET

TEXT ·nothing(SB),0,$8
	RET

TEXT ·nothing(SB),NOSPLIT,$0-4
	RET

TEXT ·nothing(SB),7,$0-4
	RET

TEXT ·nothing(SB),NOSPLIT|NOFRAME,$0
	RET

TEXT ·nothing(SB),DUPOK|NOSPLIT,$-4-0
	RET

TEXT ·nothing(SB),$0-99999999999999999999999
	RET

TEXT ·noFrame(SB),NOFRAME,$0-8
	MOVW	x+0(FP), R1
	MOVW	R1, ret+4(FP)
	RET

TEXT ·noFrame(SB),NOFRAME|NOSPLIT,$0-8
	RET

TEXT ·colliding(SB),0,$0-16
	MOVQ	ret+0(FP), AX
	RET

TEXT ·colliding(SB),0,$0-16
	MOVQ	ret+8(FP), AX
	MOVQ	ret_lo+0(FP), AX
	RET

TEXT ·dataFollows(SB),0,$0-16
	MOVQ	x+0(FP), AX
	MOVQ	x+8(FP), AX
DATA	·table+0(SB)/8, $1
GLOBL	·table(SB), RODATA, $8
	MOVQ	x+8(FP), AX
	MOVQ	16(FP), AX
	RET

TEXT ·abiInternal<ABIInternal>(SB),NOSPLIT,$0
	MOVD	$1, R9
	RET

TEXT ·abiInternalMissing<ABIInternal>(SB),NOSPLIT,$0-8
	MOVQ	$1, CX
	RET

TEXT ·abiInternalInstruction<ABIInternal>(SB),NOSPLIT,$0
	MOVQ	$1, CX
	SYSCALL
	RET

TEXT ·abiZero<ABI0>(SB),NOSPLIT,$0-16
	MOVQ	x+0(FP), AX
	RET

TEXT ·abiZero<ABISomethingNotYetInvented>(SB),NOSPLIT,$0-16
	MOVQ	x+0(FP), AX
	RET

TEXT ·abiZero<ABIInternal>(SB),NOSPLIT,$0-16
	MOVQ	x+0(FP), AX
	MOVQ	ret+8(FP), AX
	RET

TEXT ·unicode(SB),0,$0-16
	MOVQ	名前+0(FP), AX
	MOVW	名前+0(FP), R1
	MOVQ	héllo+8(FP), AX
	RET

TEXT ·method(SB),0,$0-8
	MOVQ	x+0(FP), AX
	MOVW	x+0(FP), R1
	RET

TEXT ·receiver·method(SB),0,$0-99
	MOVW	x+0(FP), R1
	RET

TEXT ·generic(SB),0,$0-24
	MOVQ	x+0(FP), AX
	MOVW	x+0(FP), R1
	MOVQ	s_base+8(FP), AX
	MOVQ	s_len+16(FP), AX
	MOVQ	box+0(FP), AX
	RET

TEXT ·measured(SB),0,$0-56
	MOVQ	p+0(FP), AX
	MOVW	p+0(FP), R1
	MOVQ	l_len+16(FP), AX
	MOVW	l_cap+12(FP), R1
	MOVQ	m+32(FP), AX
	MOVQ	f+44(FP), AX
	MOVQ	c+48(FP), AX
	RET

TEXT ·undeclared(SB),0,$0-8
	MOVQ	x+0(FP), AX
	MOVQ	8(SP), AX
	RET

TEXT ·undeclaredFrameless(SB),NOFRAME,$0
	RET

TEXT ·static<>(SB),0,$0-8
	MOVQ	x+0(FP), AX
	MOVQ	16(SP), AX
	MOVQ	64(SP), AX
	RET

TEXT other·foreign(SB),0,$0-8
	MOVQ	x+0(FP), AX
	MOVQ	16(SP), AX
	RET

TEXT parity·bytes(SB),0,$0-4
	MOVW	x+0(FP), R1
	RET

TEXT invisible(SB),0,$0-8
	MOVQ	x+0(FP), AX
	MOVQ	8(SP), AX
	RET

TEXT ·words(SB),0,$0-4
	callmacro(x+0(FP))
	CALLMACRO(y+2(FP))
	$x+0(FP)
	RET

// Every architecture's stack pointer, at offsets below, at and past where the arguments start on each, before
// and after the stack pointer moves.
TEXT ·stackPointer(SB),0,$16-24
	MOVQ	0(SP), AX
	MOVQ	4(SP), AX
	MOVQ	8(SP), AX
	MOVQ	12(SP), AX
	MOVQ	16(SP), AX
	MOVQ	20(SP), AX
	MOVQ	24(SP), AX
	MOVQ	28(SP), AX
	MOVQ	32(SP), AX
	MOVQ	36(SP), AX
	MOVQ	40(SP), AX
	MOVQ	44(SP), AX
	MOVQ	48(SP), AX
	MOVQ	56(SP), AX
	MOVQ	64(SP), AX
	MOVQ	72(SP), AX
	MOVQ	0(R13), AX
	MOVQ	4(R13), AX
	MOVQ	8(R13), AX
	MOVQ	12(R13), AX
	MOVQ	16(R13), AX
	MOVQ	20(R13), AX
	MOVQ	24(R13), AX
	MOVQ	28(R13), AX
	MOVQ	32(R13), AX
	MOVQ	36(R13), AX
	MOVQ	40(R13), AX
	MOVQ	44(R13), AX
	MOVQ	48(R13), AX
	MOVQ	56(R13), AX
	MOVQ	64(R13), AX
	MOVQ	72(R13), AX
	MOVQ	0(RSP), AX
	MOVQ	4(RSP), AX
	MOVQ	8(RSP), AX
	MOVQ	12(RSP), AX
	MOVQ	16(RSP), AX
	MOVQ	20(RSP), AX
	MOVQ	24(RSP), AX
	MOVQ	28(RSP), AX
	MOVQ	32(RSP), AX
	MOVQ	36(RSP), AX
	MOVQ	40(RSP), AX
	MOVQ	44(RSP), AX
	MOVQ	48(RSP), AX
	MOVQ	56(RSP), AX
	MOVQ	64(RSP), AX
	MOVQ	72(RSP), AX
	MOVQ	0(R29), AX
	MOVQ	4(R29), AX
	MOVQ	8(R29), AX
	MOVQ	12(R29), AX
	MOVQ	16(R29), AX
	MOVQ	20(R29), AX
	MOVQ	24(R29), AX
	MOVQ	28(R29), AX
	MOVQ	32(R29), AX
	MOVQ	36(R29), AX
	MOVQ	40(R29), AX
	MOVQ	44(R29), AX
	MOVQ	48(R29), AX
	MOVQ	56(R29), AX
	MOVQ	64(R29), AX
	MOVQ	72(R29), AX
	MOVQ	0(R1), AX
	MOVQ	4(R1), AX
	MOVQ	8(R1), AX
	MOVQ	12(R1), AX
	MOVQ	16(R1), AX
	MOVQ	20(R1), AX
	MOVQ	24(R1), AX
	MOVQ	28(R1), AX
	MOVQ	32(R1), AX
	MOVQ	36(R1), AX
	MOVQ	40(R1), AX
	MOVQ	44(R1), AX
	MOVQ	48(R1), AX
	MOVQ	56(R1), AX
	MOVQ	64(R1), AX
	MOVQ	72(R1), AX
	MOVQ	0(R15), AX
	MOVQ	4(R15), AX
	MOVQ	8(R15), AX
	MOVQ	12(R15), AX
	MOVQ	16(R15), AX
	MOVQ	20(R15), AX
	MOVQ	24(R15), AX
	MOVQ	28(R15), AX
	MOVQ	32(R15), AX
	MOVQ	36(R15), AX
	MOVQ	40(R15), AX
	MOVQ	44(R15), AX
	MOVQ	48(R15), AX
	MOVQ	56(R15), AX
	MOVQ	64(R15), AX
	MOVQ	72(R15), AX
	MOVQ	0(R3), AX
	MOVQ	4(R3), AX
	MOVQ	8(R3), AX
	MOVQ	12(R3), AX
	MOVQ	16(R3), AX
	MOVQ	20(R3), AX
	MOVQ	24(R3), AX
	MOVQ	28(R3), AX
	MOVQ	32(R3), AX
	MOVQ	36(R3), AX
	MOVQ	40(R3), AX
	MOVQ	44(R3), AX
	MOVQ	48(R3), AX
	MOVQ	56(R3), AX
	MOVQ	64(R3), AX
	MOVQ	72(R3), AX
	MOVQ	x+0(FP), AX
	MOVQ	AX, ret+16(FP)
	MOVQ	$8+16(SP), AX
	MOVQ	-8(SP), AX
	RET

TEXT ·stackPointer(SB),NOFRAME,$0-24
	MOVQ	24(SP), AX
	MOVQ	24(R13), AX
	MOVQ	24(RSP), AX
	MOVQ	24(R29), AX
	MOVQ	24(R1), AX
	MOVQ	24(R15), AX
	MOVQ	24(R3), AX
	RET

TEXT ·stackPointer(SB),0,$0-24
	MOVQ	24(SP), AX
	MOVQ	24(R13), AX
	MOVQ	24(RSP), AX
	MOVQ	24(R29), AX
	MOVQ	24(R1), AX
	MOVQ	24(R15), AX
	MOVQ	24(R3), AX
	ADDQ	$8, SP
	SUB	$8,	R13
	NOP	RSP
	NOP R29
	MOVD	R2, R1
	ADD	$8, R15
	ADDV	$8, R3
	MOVQ	32(SP), AX
	MOVQ	32(R13), AX
	MOVQ	32(RSP), AX
	MOVQ	32(R29), AX
	MOVQ	32(R1), AX
	MOVQ	32(R15), AX
	MOVQ	32(R3), AX
	RET
