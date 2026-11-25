// Every branch of the amd64 judgment. Judged only where GOARCH is amd64.

// A write before any TEXT line is outside every function, and is not judged.
	MOVQ	AX, BP

// A write, spaced, unspaced and spaced wider, is reported.
TEXT ·writeSpaced(SB), 0, $0
	MOVQ	AX, BP
	RET
TEXT ·writeUnspaced(SB), 0, $0
	MOVQ	AX,BP
	RET
TEXT ·writeSpacedWider(SB), 0, $0
	MOVQ	AX,	  BP
	RET

// Indented with spaces and followed by a comment, the write is the same write.
TEXT ·writeIndented(SB), 0, $0
        MOVQ    $0, BP    // a comment after the write
	RET

// Frameless with arguments, `$0-16`, and a frame of `$0x10`, which holds `$0` too, as the original reads it.
TEXT ·writeWithArguments(SB), NOSPLIT, $0-16
	MOVQ	AX, BP
	RET
TEXT ·writeHexadecimalFrame(SB), 0, $0x10
	MOVQ	AX, BP
	RET

// An indented TEXT line starts a function too.
	TEXT ·writeAfterIndentedText(SB), 0, $0
	MOVQ	AX, BP
	RET

// Only the first write of a function is reported.
TEXT ·writeTwice(SB), 0, $0
	MOVQ	AX, BP
	MOVQ	BX, BP
	RET

// Blank lines, commented-out instructions and labels are passed over.
TEXT ·writeAfterBlanks(SB), 0, $0

	// MOVQ	BP, AX
	// RET
loop:
	ADDQ	$1, BX
	JNE	loop
	MOVQ	BX, BP
	RET

// A conditional branch does not stop the judging.
TEXT ·writeAfterConditionalBranch(SB), 0, $0
	CMPQ	AX, BX
	JLT	done
	MOVQ	$0, BP
done:
	RET

// A register whose name only starts with BP is neither a read nor a write.
TEXT ·writeAfterLookalike(SB), 0, $0
	MOVQ	AX, BPX
	MOVQ	AX, BP
	RET

// The original's own note: a test of BP against itself reads as a write.
TEXT ·testOfTheFramePointer(SB), 0, $0
	TESTQ	BP, BP
	RET

// A read first, as a source, a push, and a memory operand, saves the frame pointer.
TEXT ·readFirst(SB), 0, $0
	MOVQ	BP, AX
	MOVQ	$0, BP
	RET
TEXT ·pushFirst(SB), 0, $0
	PUSHQ	BP
	MOVQ	SP, BP
	POPQ	BP
	RET
TEXT ·readThroughFramePointer(SB), 0, $0
	MOVQ	8(BP), AX
	MOVQ	AX, BP
	RET

// A block comment is not removed, so the write it follows reads as a read.
TEXT ·blockCommentAfterWrite(SB), 0, $0
	MOVQ	AX, BP /* a block comment */
	MOVQ	BX, BP
	RET

// An unconditional branch first, by its prefix as the original reads it, stops the judging.
TEXT ·jumpFirst(SB), 0, $0
	JMP	·readFirst(SB)
	MOVQ	AX, BP
	RET
TEXT ·returnFirst(SB), 0, $0
	RET
	MOVQ	AX, BP
TEXT ·longerReturnFirst(SB), 0, $0
	RETFL
	MOVQ	AX, BP
	RET

// A function with a frame is not judged, nor is one whose `$0` is only in its comment.
TEXT ·framed(SB), 0, $16
	MOVQ	AX, BP
	RET
TEXT ·framedWithNoArguments(SB), 0, $16-0
	MOVQ	AX, BP
	RET
TEXT ·framedCommented(SB), 0, $8 // not $0
	MOVQ	AX, BP
	RET

// A TEXT line with no `(SB)` does not start a function, so after a framed one nothing is judged.
TEXT ·framedBeforeUnnamed(SB), 0, $8
	RET
TEXT ·unnamed, 0, $0
	MOVQ	AX, BP
	RET

// The TEXT line itself is never judged, even when it ends in a write.
TEXT ·textLineWrite(SB), 0, $0; MOVQ AX, BP
	RET
	MOVQ	AX, BP

// A function after a reported one is judged anew.
TEXT ·reportedBefore(SB), 0, $0
	MOVQ	AX, BP
TEXT ·judgedAnew(SB), 0, $0
	MOVQ	BX, BP
	RET

// Arm64's write is not amd64's, and R29 is an ordinary name here.
TEXT ·arm64WriteOnAmd64(SB), 0, $0
	MOVD	R1, R29
	MOVQ	AX, BP
	RET
