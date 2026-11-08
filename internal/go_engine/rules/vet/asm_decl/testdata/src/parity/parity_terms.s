#define TERMS_BUILD_LINE 1

// +build linux,amd64 !windows

// A build line whose terms name no architecture alone leaves the file's architecture to go/build's default.

TEXT ·quads(SB),0,$0-28
	MOVQ	x+0(FP), AX
	MOVW	x+0(FP), R1
	RET
