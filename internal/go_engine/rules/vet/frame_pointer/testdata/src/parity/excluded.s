//go:build never

// Excluded by its build constraint, so the load leaves it out of the package's other files.
TEXT ·excluded(SB), 0, $0
	MOVQ	AX, BP
	MOVD	R1, R29
	RET
