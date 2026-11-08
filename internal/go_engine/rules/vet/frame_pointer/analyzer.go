// Package frame_pointer is vet's framepointer pass, assembly that writes the frame pointer before saving it. A
// function whose `TEXT` line declares a frame of `$0` gets no frame of its own, so the frame pointer register still
// holds its caller's, and a write to it before anything has read it, to save it, loses the caller's frame for whatever
// walks the stack. From each such `TEXT` line the pass reads the function's lines in order and reports the first that
// writes the frame pointer, BP on amd64 and R29 on arm64, unless a line that reads it or branches unconditionally
// comes first, after which it judges nothing more until the next `TEXT` line. It judges only amd64 and arm64, only on
// linux and darwin, and never the runtime's own assembly.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/framepointer/framepointer.go,
// whose logic it keeps, its patterns among it, and whose message it reports word for word, at the same position, the
// start of the line. Like the original it reads the architecture and the operating system from go/build's default
// context, the `GOARCH` and `GOOS` the process runs under, and of the package only its assembly files and its import
// path, so it reads no type information. The registry runs the port in the original's place, and the original stays
// the oracle: the parity tests beside this file run both over the same packages, on each target the judgment
// differs by. The two agree everywhere, so there is no differences fixture.
package frame_pointer

import (
	"go/build"
	"go/token"
	"os"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = &analysis.Analyzer{
	Name: "framepointer",
	Doc:  "report assembly that clobbers the frame pointer before saving it",
	URL:  "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/framepointer",
	Run:  run,
}

// architecture is how one `GOARCH`'s assembly touches the frame pointer, each judged on a line with its comment and
// its surrounding space removed. x/tools' framepointer arch.
type architecture struct {
	writesFramePointer      func(line string) bool
	readsFramePointer       func(line string) bool
	branchesUnconditionally func(line string) bool
}

// architectures is the `GOARCH` values the pass judges. x/tools' framepointer arches.
var architectures = map[string]architecture{
	"amd64": {
		// The original's own note holds here too: `TESTQ BP, BP` reads as a write, which it does not do.
		writesFramePointer: regexp.MustCompile(`,\s*BP$`).MatchString,
		readsFramePointer:  regexp.MustCompile(`\bBP\b`).MatchString,
		branchesUnconditionally: func(line string) bool {
			return hasAnyPrefix(line, "JMP", "RET")
		},
	},
	"arm64": {
		writesFramePointer: writesArm64FramePointer,
		readsFramePointer:  regexp.MustCompile(`\bR29\b`).MatchString,
		branchesUnconditionally: func(line string) bool {
			// The instruction alone, so `B skip` branches and `BEQ skip` and `BL ·function(SB)` do not.
			instruction := line
			if spaceIndex := strings.IndexFunc(line, unicode.IsSpace); spaceIndex > 0 {
				instruction = line[:spaceIndex]
			}
			return instruction == "B" || instruction == "JMP" || instruction == "RET"
		},
	},
}

// writesArm64FramePointer is whether an arm64 line writes R29: as its last operand, `MOVD R1, R29`, or as one of
// the pair of registers an instruction that loads a pair writes, `LDP 8(R0), (R26, R29)` and
// `CASPD (R2, R3), (R2), (R26, R29)`.
func writesArm64FramePointer(line string) bool {
	if commaIndex := strings.LastIndex(line, ","); commaIndex > 0 && strings.HasSuffix(line[commaIndex:], "R29") {
		return true
	}
	if !hasAnyPrefix(line, "LDP", "LDAXP", "LDXP", "CASP") {
		return false
	}
	// The pair is the last parenthesized operand, so only the text from its opening parenthesis to its closing one is
	// judged, which names the pair's two registers.
	openingIndex := strings.LastIndex(line, "(")
	closingIndex := strings.LastIndex(line, ")")
	if openingIndex < 0 || openingIndex >= closingIndex {
		return false
	}
	pair := line[openingIndex:closingIndex]
	return strings.Contains(pair, ",") && strings.Contains(pair, "R29")
}

// hasAnyPrefix is whether line starts with any of prefixes. x/tools' framepointer hasAnyPrefix.
func hasAnyPrefix(line string, prefixes ...string) bool {
	return slices.ContainsFunc(prefixes, func(prefix string) bool {
		return strings.HasPrefix(line, prefix)
	})
}

func run(pass *analysis.Pass) (any, error) {
	judged, judgesArchitecture := architectures[build.Default.GOARCH]
	if !judgesArchitecture {
		return nil, nil
	}
	if build.Default.GOOS != "linux" && build.Default.GOOS != "darwin" {
		return nil, nil
	}
	// The runtime's assembly is never judged, so none of its files is read.
	if pass.Pkg.Path() == "runtime" {
		return nil, nil
	}
	for _, filename := range pass.OtherFiles {
		// OtherFiles holds C headers and other sources beside the assembly, and only Go assembly is judged.
		if !strings.HasSuffix(filename, ".s") {
			continue
		}
		content, file, readError := readFile(pass, filename)
		if readError != nil {
			// Unreachable from a load: every file the package lists is on disk and the driver allows reading it. Kept
			// as the original keeps it, ending the pass with the error.
			return nil, readError
		}
		judgeFile(pass, judged, string(content), file)
	}
	return nil, nil
}

// judgeFile reports each line of one assembly file that writes the frame pointer in a frameless function that has
// not saved it.
func judgeFile(pass *analysis.Pass, judged architecture, content string, file *token.File) {
	// judging is whether the lines read so far are a frameless function's that has not yet read, written or branched
	// past its frame pointer.
	judging := false
	for index, line := range strings.SplitAfter(content, "\n") {
		// Everything from `//` on is a comment, a commented-out instruction among them.
		code, _, _ := strings.Cut(line, "//")
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		// A `TEXT` line declaring a frame of `$0` starts a frameless function, and is itself never judged.
		if strings.HasPrefix(code, "TEXT") && strings.Contains(code, "(SB)") && strings.Contains(code, "$0") {
			judging = true
			continue
		}
		if !judging {
			continue
		}
		if judged.writesFramePointer(code) {
			pass.Reportf(file.LineStart(index+1), "frame pointer is clobbered before saving")
			judging = false
			continue
		}
		// A read is taken as saving the frame pointer, and past an unconditional branch the order of the lines is no
		// longer the order they run in.
		if judged.readsFramePointer(code) || judged.branchesUnconditionally(code) {
			judging = false
		}
	}
}

// readFile reads filename through the pass, falling back to the file system when the driver offers no reader, and
// adds it to the pass's file set so a finding can be placed in it. x/tools' analyzerutil.ReadFile.
func readFile(pass *analysis.Pass, filename string) ([]byte, *token.File, error) {
	reader := pass.ReadFile
	// The checker always offers a reader, so the fallback is kept only as the original's helper keeps it, for a
	// driver that offers none.
	if reader == nil {
		reader = os.ReadFile
	}
	content, readError := reader(filename)
	if readError != nil {
		return nil, nil, readError
	}
	file := pass.Fset.AddFile(filename, -1, len(content))
	file.SetLinesForContent(content)
	return content, file, nil
}
