// Judged only where GOOS is linux and GOARCH is arm64.
TEXT ·linuxOnly(SB), 0, $0
	MOVD	$0, R29
	RET
