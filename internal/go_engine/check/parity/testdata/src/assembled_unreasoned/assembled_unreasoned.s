// calls is assembly, where both find the call to shared and the port alone the call to gained.
TEXT ·calls(SB),0,$0-0
	CALL ·shared(SB)
	CALL ·gained(SB) // port finds: call to gained
	RET
