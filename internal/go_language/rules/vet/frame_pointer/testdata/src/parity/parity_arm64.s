// Every branch of the arm64 judgment. Judged only where GOARCH is arm64.

// A write before any TEXT line is outside every function, and is not judged.
	MOVD	R1, R29

// A write as the last operand, spaced, unspaced, and of three operands, is reported.
TEXT ·writeLastOperand(SB), 0, $0
	MOVD	R1, R29
	RET
TEXT ·writeUnspaced(SB), 0, $0
	MOVD	R1,R29
	RET
TEXT ·writeThreeOperands(SB), NOSPLIT|NOFRAME, $0-8
	ADD	$8, R1, R29
	RET

// A last operand that only ends in R29 reads as a write, as the original's suffix test reads it.
TEXT ·writeLookalike(SB), 0, $0
	MOVD	R1, XR29
	RET

// Each instruction that loads a pair, writing R29 as either of the pair.
TEXT ·writePairLoad(SB), 0, $0
	LDP	16(RSP), (R29, R30)
	RET
TEXT ·writePairLoadWord(SB), 0, $0
	LDPW	16(RSP), (R26, R29)
	RET
TEXT ·writeAcquiringPairLoad(SB), 0, $0
	LDAXP	(R0), (R26, R29)
	RET
TEXT ·writeExclusivePairLoad(SB), 0, $0
	LDXP	(R0), (R29, R30)
	RET
TEXT ·writeCompareAndSwapPair(SB), 0, $0
	CASPD	(R2, R3), (R2), (R26, R29)
	RET

// A pair load that leaves R29 alone is no write, nor is one whose last parenthesis never closes, so the write after
// each is reported.
TEXT ·writeAfterPairLoad(SB), 0, $0
	LDP	(R0), (R1, R2)
	MOVD	$0, R29
	RET
TEXT ·writeAfterUnclosedPair(SB), 0, $0
	LDP	(R0), (R1
	MOVD	$0, R29
	RET

// A pair load from R29 into other registers reads it.
TEXT ·pairLoadFromFramePointer(SB), 0, $0
	LDP	(R29), (R1, R2)
	MOVD	$0, R29
	RET

// A pair store is not among the pair loads, so naming R29 reads it.
TEXT ·pairStoreFirst(SB), 0, $0
	STP	(R29, R30), -16(RSP)
	MOVD	$0, R29
	RET

// A line opening with its comma has no operand before it, so it is a read, not a write.
TEXT ·openingComma(SB), 0, $0
	,R29
	MOVD	$0, R29
	RET

// A read first, as a source or through memory, saves the frame pointer.
TEXT ·readFirst(SB), 0, $0
	MOVD	R29, R1
	MOVD	$0, R29
	RET
TEXT ·readThroughFramePointer(SB), 0, $0
	MOVD	8(R29), R1
	MOVD	$0, R29
	RET

// A register whose name only starts with R29 is neither a read nor a write.
TEXT ·writeAfterLookalike(SB), 0, $0
	MOVD	R290, R1
	MOVD	$0, R29
	RET

// An unconditional branch first, by its exact instruction, stops the judging: B, JMP, RET, and RET with an operand.
TEXT ·branchFirst(SB), 0, $0
	B	skip
	MOVD	$0, R29
skip:
	RET
TEXT ·jumpFirst(SB), 0, $0
	JMP	·readFirst(SB)
	MOVD	$0, R29
	RET
TEXT ·returnFirst(SB), 0, $0
	RET
	MOVD	$0, R29
TEXT ·returnThroughFirst(SB), 0, $0
	RET	(R30)
	MOVD	$0, R29

// A call, a conditional branch, and an instruction that only starts with B or RET do not stop it.
TEXT ·writeAfterCall(SB), 0, $0
	BL	·readFirst(SB)
	MOVD	$0, R29
	RET
TEXT ·writeAfterConditionalBranch(SB), 0, $0
	CBZ	R0, done
	MOVD	$0, R29
done:
	RET
TEXT ·writeAfterLongerReturn(SB), 0, $0
	RETAA
	MOVD	$0, R29
	RET

// A function with a frame is not judged.
TEXT ·framed(SB), 0, $16
	MOVD	$0, R29
	RET
TEXT ·framedWithNoArguments(SB), 0, $16-0
	MOVD	$0, R29
	RET

// Amd64's write is not arm64's, and BP is an ordinary name here.
TEXT ·amd64WriteOnArm64(SB), 0, $0
	MOVQ	AX, BP
	MOVD	$0, R29
	RET
