package asm_decl

import (
	"fmt"
	"go/build"
	"go/token"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// The patterns a line of assembly is read with, each byte for byte the original's.
var (
	// plusBuildPattern finds a `// +build` line and its terms. A `//go:build` line is not read, as the original
	// reads none. x/tools' asmdecl asmPlusBuild.
	plusBuildPattern = regexp.MustCompile(`//\s+\+build\s+([^\n]+)`)
	// textPattern finds a `TEXT` line of a function Go can call: its package, its name, its flags, and its frame's
	// size and its arguments' size. x/tools' asmdecl asmTEXT.
	textPattern = regexp.MustCompile(
		`\bTEXT\b(.*)·([^\(]+)\(SB\)(?:\s*,\s*([0-9A-Z|+()]+))?(?:\s*,\s*\$(-?[0-9]+)(?:-([0-9]+))?)?`,
	)
	// dataPattern finds a `DATA` or `GLOBL` line. x/tools' asmdecl asmDATA.
	dataPattern = regexp.MustCompile(`\b(DATA|GLOBL)\b`)
	// namedFramePointerPattern finds a reference to an argument by name, `x+8(FP)`, or to its address, `$x+8(FP)`.
	// x/tools' asmdecl asmNamedFP.
	namedFramePointerPattern = regexp.MustCompile(`\$?([a-zA-Z0-9_\xFF-\x{10FFFF}]+)(?:\+([0-9]+))\(FP\)`)
	// unnamedFramePointerPattern finds a reference to an argument by offset alone, `8(FP)`. x/tools' asmdecl
	// asmUnnamedFP.
	unnamedFramePointerPattern = regexp.MustCompile(`[^+\-0-9](([0-9]+)\(FP\))`)
	// stackPointerPattern finds a reference through a register by offset, `8(SP)`. x/tools' asmdecl asmSP.
	stackPointerPattern = regexp.MustCompile(`[^+\-0-9](([0-9]+)\(([A-Z0-9]+)\))`)
	// abiSelectorPattern finds a function name's `ABI` selector, `name<ABIInternal>`. x/tools' asmdecl abiSuff.
	abiSelectorPattern = regexp.MustCompile(`^(.+)<(ABI.+)>$`)
)

// assemblyFile is one assembly file being judged, line by line, and what its lines so far have said: the
// architecture, and of the function whose `TEXT` line came last, its declaration's frame and what its body has
// done. x/tools' asmdecl run's locals for one file.
type assemblyFile struct {
	pass *analysis.Pass
	file *token.File
	// frames is every Go declaration without a body, by its name, then by architecture's name.
	frames map[string]map[string]*argumentFrame
	// unmeasured is the names of the declarations whose frame go/types cannot measure, whose functions are not judged.
	unmeasured map[string]bool
	// architectureName is the file's architecture, and judged its description: from the file's name, a build line,
	// or go/build's default, empty and nil until one says.
	architectureName string
	judged           *architecture
	// frame is the declaration of the function being judged, nil when it has none or a `DATA` line came after it.
	frame *argumentFrame
	// functionName is the function being judged, empty outside one or in a function of another package.
	functionName string
	// abi is the function's `ABI` selector, `ABIInternal` in `·name<ABIInternal>(SB)`, empty when it has none.
	abi string
	// localSize is the offset from the stack pointer at which the arguments start, and argumentSize their size as
	// the `TEXT` line states them.
	localSize    int
	argumentSize int
	// wroteStackPointer is whether the function has moved its stack pointer, after which an offset from it no longer
	// says what it addresses.
	wroteStackPointer bool
	// noFrame is whether the `TEXT` line says `NOFRAME`.
	noFrame bool
	// wroteResult is whether the function has written its result, or done something taken as writing it.
	wroteResult bool
	// returnLines are the lines of the function's `RET` instructions, judged when the function ends.
	returnLines []int
}

// newAssemblyFile starts judging the assembly file named filename, read into file, against frames, leaving the
// functions unmeasured names unjudged. Its architecture is the one its name ends in, `_amd64.s`, when it ends in one.
func newAssemblyFile(
	pass *analysis.Pass,
	file *token.File,
	filename string,
	frames map[string]map[string]*argumentFrame,
	unmeasured map[string]bool,
) *assemblyFile {
	walk := &assemblyFile{pass: pass, file: file, frames: frames, unmeasured: unmeasured}
	for _, judged := range architectures {
		if strings.HasSuffix(filename, "_"+judged.name+".s") {
			walk.architectureName = judged.name
			walk.judged = judged
			break
		}
	}
	return walk
}

// judge reports what is wrong in content, the file's text.
func (walk *assemblyFile) judge(content string) {
	for index, line := range strings.SplitAfter(content, "\n") {
		if !walk.judgeLine(index+1, line) {
			// The file's architecture could not be told at its first `TEXT` line, so none of it is judged, as the
			// original leaves its file.
			return
		}
	}
	walk.endFunction()
}

// judgeLine judges the line at lineNumber, and returns whether to read on.
func (walk *assemblyFile) judgeLine(lineNumber int, line string) bool {
	if walk.architectureName == "" {
		walk.readBuildLine(line)
	}
	// Everything from `//` on is a comment, a commented-out instruction among them.
	code, _, _ := strings.Cut(line, "//")

	if text := textPattern.FindStringSubmatch(code); text != nil {
		return walk.startFunction(lineNumber, text)
	}
	if strings.Contains(code, "TEXT") && strings.Contains(code, "SB") {
		// A function Go cannot see, which textPattern does not match, so nothing is judged until the next one.
		walk.endFunction()
		walk.frame = nil
		walk.functionName = ""
		walk.abi = ""
		return true
	}

	// `RET f(SB)` is a tail call, which may leave the results to the function it calls.
	if strings.Contains(code, "RET") && !strings.Contains(code, "(SB)") {
		walk.returnLines = append(walk.returnLines, lineNumber)
	}
	if walk.functionName == "" {
		return true
	}
	// The function's data follows it, and nothing more of the function is judged against its declaration.
	if dataPattern.MatchString(code) {
		walk.frame = nil
	}
	if walk.judged == nil {
		return true
	}

	if walk.writesStackPointer(code) {
		walk.wroteStackPointer = true
		return true
	}
	walk.readResultWrites(code)
	walk.judgeStackPointerReferences(lineNumber, code)
	if walk.frame == nil {
		return true
	}
	walk.judgeFramePointerReferences(lineNumber, code)
	return true
}

// readBuildLine takes the file's architecture from line when it is a `// +build` line naming one. A line may name
// several, so the one go/build's default context names wins, and otherwise the first. x/tools' asmdecl run's reading
// of asmPlusBuild.
func (walk *assemblyFile) readBuildLine(line string) {
	buildLine := plusBuildPattern.FindStringSubmatch(line)
	if buildLine == nil {
		return
	}
	candidates := []*architecture{}
	for term := range strings.FieldsSeq(buildLine[1]) {
		for _, judged := range architectures {
			if judged.name == term {
				candidates = append(candidates, judged)
			}
		}
	}
	for _, candidate := range candidates {
		if candidate.name == defaultArchitectureName() {
			candidates = []*architecture{candidate}
			break
		}
	}
	if len(candidates) > 0 {
		walk.architectureName = candidates[0].name
		walk.judged = candidates[0]
	}
}

// startFunction begins judging the function whose `TEXT` line is at lineNumber, matched by textPattern as text, and
// returns whether to read on: not when the file's architecture cannot be told.
func (walk *assemblyFile) startFunction(lineNumber int, text []string) bool {
	walk.endFunction()
	if walk.architectureName == "" {
		// Neither the file's name nor a build line said, so the architecture is the one the build is for.
		fallback := architectureNamed(defaultArchitectureName())
		if fallback == nil {
			// The original logs that it cannot tell the file's architecture, and judges none of it. The port judges
			// none of it either, and does not log: a rule speaks only through its findings. Only a go/build default
			// architecture the table lacks reaches here, and the table holds every architecture Go 1.27 builds for.
			return false
		}
		walk.architectureName = fallback.name
		walk.judged = fallback
	}
	walk.functionName = text[2]
	if packagePath := strings.TrimSpace(text[1]); packagePath != "" {
		// The assembler writes a package path's slashes as Unicode division slashes inside a symbol's name.
		if strings.ReplaceAll(packagePath, "∕", "/") != walk.pass.Pkg.Path() {
			// Another package's function, whose declaration this package does not hold.
			walk.frame = nil
			walk.functionName = ""
			walk.abi = ""
			return true
		}
	}
	walk.functionName, walk.abi = withoutAbiSelector(walk.functionName)
	if walk.unmeasured[walk.functionName] {
		// A declaration whose frame go/types cannot measure, where the original crashes. Nothing of the function is
		// judged, as nothing of another package's is.
		walk.frame = nil
		walk.functionName = ""
		walk.abi = ""
		return true
	}
	flags := text[3]
	walk.frame = walk.frames[walk.functionName][walk.architectureName]
	if walk.frame != nil {
		stated := numberOrZero(text[5])
		// A `NOSPLIT` function, flags 7 in the old numeric spelling, may leave its arguments' size unstated.
		if stated != walk.frame.size && (flags != "7" && !strings.Contains(flags, "NOSPLIT") || stated != 0) {
			walk.report(lineNumber, fmt.Sprintf("wrong argument size %d; expected $...-%d", stated, walk.frame.size))
		}
	}
	walk.noFrame = strings.Contains(flags, "NOFRAME")
	// The arguments sit above the function's frame and the return address, and above the saved link register too
	// where the architecture has one and the function has a frame to save it in.
	walk.localSize = numberOrZero(text[4]) + walk.judged.integerSize
	if walk.judged.linkRegister && !walk.noFrame {
		walk.localSize += walk.judged.integerSize
	}
	walk.argumentSize = numberOrZero(text[5])
	// A static function, `name<>`, is the file's own, and a `NOFRAME` one needs no declaration either.
	if walk.frame == nil && !strings.Contains(walk.functionName, "<>") && !walk.noFrame {
		walk.report(lineNumber, fmt.Sprintf("function %s missing Go declaration", walk.functionName))
	}
	walk.wroteStackPointer = false
	walk.wroteResult = false
	return true
}

// endFunction reports each `RET` of the function being judged when its declaration returns an unnamed result and
// nothing in the function wrote it, and forgets the function's `RET` lines. x/tools' asmdecl run's flushRet.
func (walk *assemblyFile) endFunction() {
	if walk.frame != nil && walk.frame.variables["ret"] != nil && !walk.wroteResult && len(walk.returnLines) > 0 {
		result := walk.frame.variables["ret"]
		described := fmt.Sprintf("%d-byte ret+%d(FP)", result.size, result.offset)
		if walk.abi == "ABIInternal" {
			described = "result register"
		}
		for _, returnLine := range walk.returnLines {
			walk.report(returnLine, "RET without writing to "+described)
		}
	}
	walk.returnLines = nil
}

// writesStackPointer is whether code moves the architecture's stack pointer: names it as the last operand, or as the
// operand of `NOP`, which the assembler uses to say a function adjusted it.
func (walk *assemblyFile) writesStackPointer(code string) bool {
	return strings.Contains(code, ", "+walk.judged.stackPointer) ||
		strings.Contains(code, ",\t"+walk.judged.stackPointer) ||
		strings.Contains(code, "NOP "+walk.judged.stackPointer) ||
		strings.Contains(code, "NOP\t"+walk.judged.stackPointer)
}

// readResultWrites notes when code does what is taken as writing the function's result without naming it: a wasm
// `CallImport`, a call out that may write it, and in a function of the register `ABI` an instruction that writes a
// result register or a mention of one.
func (walk *assemblyFile) readResultWrites(code string) {
	if walk.architectureName == "wasm" && strings.Contains(code, "CallImport") {
		walk.wroteResult = true
	}
	if walk.abi != "ABIInternal" || walk.wroteResult {
		return
	}
	for _, instruction := range walk.judged.resultWritingInstructions {
		if strings.Contains(code, instruction) {
			walk.wroteResult = true
			break
		}
	}
	for _, register := range walk.judged.resultRegisters {
		if strings.Contains(code, register) {
			walk.wroteResult = true
			break
		}
	}
}

// judgeStackPointerReferences reports each reference in code through the stack pointer, `8(SP)`, that reaches the
// arguments, which assembly names through the frame pointer instead, unless the function has moved its stack
// pointer or has no frame.
func (walk *assemblyFile) judgeStackPointerReferences(lineNumber int, code string) {
	for _, reference := range stackPointerPattern.FindAllStringSubmatch(code, -1) {
		if reference[3] != walk.judged.stackPointer || walk.wroteStackPointer || walk.noFrame {
			continue
		}
		offset := 0
		// The pattern requires the offset, so this always holds. Kept as the original keeps it.
		if reference[1] != "" {
			offset = numberOrZero(reference[2])
		}
		if offset < walk.localSize {
			continue
		}
		if walk.frame != nil {
			if named := walk.frame.variablesByOffset[offset-walk.localSize]; named != nil {
				walk.report(lineNumber,
					fmt.Sprintf("%s should be %s+%d(FP)", reference[1], named.name, offset-walk.localSize))
				continue
			}
		}
		if offset >= walk.localSize+walk.argumentSize {
			walk.report(lineNumber, fmt.Sprintf("use of %s points beyond argument frame", reference[1]))
			continue
		}
		walk.report(lineNumber, fmt.Sprintf("use of %s to access argument frame", reference[1]))
	}
}

// judgeFramePointerReferences reports each reference in code through the frame pointer that names no argument, or
// names one wrongly, and notes a write to a result.
func (walk *assemblyFile) judgeFramePointerReferences(lineNumber int, code string) {
	for _, reference := range unnamedFramePointerPattern.FindAllStringSubmatch(code, -1) {
		offset := numberOrZero(reference[2])
		if named := walk.frame.variablesByOffset[offset]; named != nil {
			walk.report(lineNumber, fmt.Sprintf("use of unnamed argument %s; offset %d is %s+%d(FP)", reference[1],
				offset, named.name, named.offset))
			continue
		}
		walk.report(lineNumber, fmt.Sprintf("use of unnamed argument %s", reference[1]))
	}

	for _, reference := range namedFramePointerPattern.FindAllStringSubmatch(code, -1) {
		name := reference[1]
		offset := 0
		// The pattern requires the offset, so this always holds. Kept as the original keeps it.
		if reference[2] != "" {
			offset = numberOrZero(reference[2])
		}
		if name == "ret" || strings.HasPrefix(name, "ret_") {
			walk.wroteResult = true
		}
		referenced := walk.frame.variables[name]
		if referenced != nil {
			checkVariableReference(func(message string) {
				walk.report(lineNumber, message)
			}, walk.frame, code, reference[0], offset, referenced, walk.judged)
			continue
		}
		// `argframe+0(FP)` names the whole argument frame.
		if name == "argframe" && offset == 0 {
			continue
		}
		if covering := walk.frame.variablesByOffset[offset]; covering != nil {
			walk.report(lineNumber, fmt.Sprintf("unknown variable %s; offset %d is %s+%d(FP)", name, offset,
				covering.name, covering.offset))
			continue
		}
		walk.report(lineNumber, fmt.Sprintf("unknown variable %s", name))
	}
}

// report reports message at the start of the line at lineNumber, under the file's architecture and the function
// being judged, `[amd64] name: message`.
func (walk *assemblyFile) report(lineNumber int, message string) {
	walk.pass.Reportf(walk.file.LineStart(lineNumber), "[%s] %s: %s", walk.architectureName, walk.functionName,
		message)
}

// withoutAbiSelector is name without its `ABI` selector, and the selector, `ABIInternal` from `name<ABIInternal>`, or
// name and nothing when it has none. x/tools' asmdecl run's trimABI.
func withoutAbiSelector(name string) (string, string) {
	if selected := abiSelectorPattern.FindStringSubmatch(name); selected != nil {
		return selected[1], selected[2]
	}
	return name, ""
}

// numberOrZero is the decimal number text spells. The patterns capture only digits, with a leading minus at most,
// or nothing at all, which reads as zero, as the original reads it; a number too large for an int reads as the
// largest, which is what strconv.Atoi returns beside its range error, and the original ignores the error the same.
func numberOrZero(text string) int {
	// Ignored: the error says only that text is empty or too large, and either way the number Atoi returns is the
	// reading; the original drops the error too.
	number, _ := strconv.Atoi(text)
	return number
}

// defaultArchitectureName is the architecture the build is for, from go/build's default context, which the original
// reads too, so the two agree on a file whose name and build lines name none.
func defaultArchitectureName() string {
	return build.Default.GOARCH
}
