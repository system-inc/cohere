// foo's Go function has a body, so the assembly implements no declaration.
TEXT ·foo(SB),0,$0-0
	RET

// bar's arguments are an int32, 4 bytes, so 8 is the wrong size.
TEXT ·bar(SB),0,$0-8
	RET
