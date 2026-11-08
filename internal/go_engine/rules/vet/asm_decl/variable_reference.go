package asm_decl

import (
	"fmt"
	"regexp"
	"strings"
)

// opcodePattern finds a line's instruction after an optional label, and its operands. x/tools' asmdecl asmOpcode.
var opcodePattern = regexp.MustCompile(`^\s*(?:[A-Z0-9a-z_]+:)?\s*([A-Z]+)\s*([^,]*)(?:,\s*(.*))?`)

// ppc64SuffixPattern finds the size letter of a ppc64 instruction under its standard suffixes. x/tools' asmdecl
// ppc64Suff.
var ppc64SuffixPattern = regexp.MustCompile(`([BHWD])(ZU|Z|U|BR)?$`)

// checkVariableReference reports, through report, what is wrong with expression, a reference on line to the
// variable referenced at offset in frame: an offset that is not the variable's, or an instruction that moves a
// different number of bytes than the variable holds. judged is the file's architecture. x/tools' asmdecl
// asmCheckVar.
func checkVariableReference(
	report func(message string),
	frame *argumentFrame,
	line string,
	expression string,
	offset int,
	referenced *variable,
	judged *architecture,
) {
	opcode := opcodePattern.FindStringSubmatch(line)
	if opcode == nil {
		// The line arrives with its comment cut off, so it never opens with `//` and the second condition always
		// holds. Kept as the original keeps it.
		if !strings.HasPrefix(strings.TrimSpace(line), "//") {
			report("cannot find assembly opcode")
		}
		return
	}
	instruction := opcode[1]
	sourceSize, destinationSize, isAddress := operandSizes(frame.architecture.name, instruction)
	// A reference written `$name+offset(FP)` is the variable's address, not its value.
	if strings.HasPrefix(expression, "$") {
		isAddress = true
	}
	if destinationSize == 0 {
		destinationSize = sourceSize
	}
	// Whether the reference is the instruction's first operand or a later one, judged by where it sits against the
	// line's first comma.
	kind := sourceSize
	if strings.Index(line, expression) > strings.Index(line, ",") {
		kind = destinationSize
	}

	judgedKind := referenced.kind
	judgedSize := referenced.size
	judgedTypeName := referenced.typeName
	switch referenced.kind {
	case interfaceKind, emptyInterfaceKind, stringKind, sliceKind:
		// A reference to the whole value may read its first word, the pointer.
		judgedKind = referenced.inner[0].kind
		judgedSize = referenced.inner[0].size
		judgedTypeName = referenced.inner[0].typeName
	case complexKind:
		// One instruction may load both parts of a complex number at once.
		if int(kind) == judgedSize {
			kind = complexKind
		}
	}
	if isAddress {
		judgedKind = variableKind(judged.pointerSize)
		judgedSize = judged.pointerSize
		judgedTypeName = "address"
	}

	if offset != referenced.offset {
		var alternatives strings.Builder
		for innerIndex, inner := range referenced.inner {
			if len(referenced.inner) > 1 {
				alternatives.WriteString(",")
			}
			alternatives.WriteString(" ")
			if innerIndex == len(referenced.inner)-1 {
				alternatives.WriteString("or ")
			}
			fmt.Fprintf(&alternatives, "%s+%d(FP)", inner.name, inner.offset)
		}
		report(fmt.Sprintf("invalid offset %s; expected %s+%d(FP)%s", expression, referenced.name, referenced.offset,
			alternatives.String()))
		return
	}
	if kind != 0 && kind != judgedKind {
		var contents strings.Builder
		if len(referenced.inner) > 0 {
			contents.WriteString(" containing")
			for innerIndex, inner := range referenced.inner {
				if innerIndex > 0 && len(referenced.inner) > 2 {
					contents.WriteString(",")
				}
				contents.WriteString(" ")
				if innerIndex > 0 && innerIndex == len(referenced.inner)-1 {
					contents.WriteString("and ")
				}
				fmt.Fprintf(&contents, "%s+%d(FP)", inner.name, inner.offset)
			}
		}
		report(fmt.Sprintf("invalid %s of %s; %s is %d-byte value%s", instruction, expression, judgedTypeName,
			judgedSize, contents.String()))
	}
}

// operandSizes is how many bytes instruction moves on the architecture named architectureName, from its source and
// to its destination, zero where it says nothing, and whether it takes its operand's address rather than reading
// it. The suffix usually says, with exceptions. A destination of zero is the source's. x/tools' asmdecl
// asmCheckVar's switch on the opcode.
func operandSizes(architectureName string, instruction string) (variableKind, variableKind, bool) {
	switch architectureName + "." + instruction {
	case "386.FMOVLP":
		return 8, 4, false
	case "arm.MOVD":
		return 8, 0, false
	case "arm.MOVW":
		return 4, 0, false
	case "arm.MOVH", "arm.MOVHU":
		return 2, 0, false
	case "arm.MOVB", "arm.MOVBU":
		return 1, 0, false
	// `LEA` instructions take their second operand's address and never read it.
	case "386.LEAL":
		return 0, 4, true
	case "amd64.LEAQ":
		return 0, 8, true
	}
	switch architectureName {
	case "386", "amd64":
		return x86SourceSize(instruction), 0, false
	case "ppc64", "ppc64le":
		// The standard suffixes stripped, the letter left says the size.
		suffix := ppc64SuffixPattern.FindStringSubmatch(instruction)
		if suffix == nil {
			return 0, 0, false
		}
		switch suffix[1] {
		case "B":
			return 1, 0, false
		case "H":
			return 2, 0, false
		case "W":
			return 4, 0, false
		case "D":
			return 8, 0, false
		}
	case "loong64", "mips", "mipsle", "mips64", "mips64le":
		switch instruction {
		case "MOVB", "MOVBU":
			return 1, 0, false
		case "MOVH", "MOVHU":
			return 2, 0, false
		case "MOVW", "MOVWU", "MOVF":
			return 4, 0, false
		case "MOVV", "MOVD":
			return 8, 0, false
		}
	case "s390x":
		switch instruction {
		case "MOVB", "MOVBZ":
			return 1, 0, false
		case "MOVH", "MOVHZ":
			return 2, 0, false
		case "MOVW", "MOVWZ", "FMOVS":
			return 4, 0, false
		case "MOVD", "FMOVD":
			return 8, 0, false
		}
	}
	return 0, 0, false
}

// x86SourceSize is how many bytes a 386 or amd64 instruction moves from its source, zero where it says nothing.
// The tests run in the original's order, since an instruction can match more than one. x/tools' asmdecl
// asmCheckVar's case for 386 and amd64.
func x86SourceSize(instruction string) variableKind {
	switch {
	// `FMOVDP`, `FXCHD` and the like.
	case strings.HasPrefix(instruction, "F") &&
		(strings.HasSuffix(instruction, "D") || strings.HasSuffix(instruction, "DP")):
		return 8
	// `PINSRD`, `PEXTRD` and the like.
	case strings.HasPrefix(instruction, "P") && strings.HasSuffix(instruction, "RD"):
		return 4
	// `FMOVFP`, `FXCHF` and the like.
	case strings.HasPrefix(instruction, "F") &&
		(strings.HasSuffix(instruction, "F") || strings.HasSuffix(instruction, "FP")):
		return 4
	// `MOVSD`, `SQRTSD` and the like.
	case strings.HasSuffix(instruction, "SD"):
		return 8
	// `MOVSS`, `SQRTSS` and the like.
	case strings.HasSuffix(instruction, "SS"):
		return 4
	case instruction == "MOVO" || instruction == "MOVOU":
		return 16
	// `SETEQ` and the like.
	case strings.HasPrefix(instruction, "SET"):
		return 1
	}
	// The opcode pattern matches one capital letter at least, so the instruction has a last letter.
	switch instruction[len(instruction)-1] {
	case 'B':
		return 1
	case 'W':
		return 2
	case 'L':
		return 4
	case 'D', 'Q':
		return 8
	}
	return 0
}
