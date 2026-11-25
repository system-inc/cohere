// The wasm rules: CallImport as writing the result, and the stack pointer SP with no link register.

TEXT ·wasmImport(SB),NOSPLIT,$0-8
	CallImport
	RET

TEXT ·wasmImport(SB),NOSPLIT,$0-8
	MOVD	$1, R0
	RET

TEXT ·wasmImport(SB),NOSPLIT|NOFRAME,$0-8
	MOVD	24(SP), R1
	RET

TEXT ·quads(SB),0,$0-32
	MOVD	x+0(FP), R1
	MOVW	x+0(FP), R1
	MOVD	8(SP), R1
	MOVD	16(SP), R1
	MOVD	40(SP), R1
	MOVD	48(SP), R1
	RET

TEXT ·abiInternal<ABIInternal>(SB),NOSPLIT,$0
	CallImport
	RET
