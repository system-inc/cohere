package asm_decl

import "go/types"

// architecture is how one `GOARCH` lays out a function's arguments and names the registers the judgment reads.
// x/tools' asmdecl asmArch.
type architecture struct {
	// name is the `GOARCH` value, as a file name's suffix and a build line spell it.
	name string
	// bigEndian puts a 64-bit value's high half first when a 32-bit architecture splits it in two.
	bigEndian bool
	// stackPointer is the register a function's assembly addresses its own frame through.
	stackPointer string
	// linkRegister is whether a call leaves the return address in a register, which a function with a frame saves on
	// its stack, so the arguments sit one word further from the stack pointer.
	linkRegister bool
	// resultRegisters are the first integer and the first floating-point register a function of the register `ABI`
	// (`ABIInternal`) returns its results in. Only whether a result is written is judged, so naming either counts as
	// writing one.
	resultRegisters []string
	// resultWritingInstructions are the instructions that write a result register without naming it.
	resultWritingInstructions []string
	// sizes is the compiler's sizes for the architecture, and the three below are read from it once, at start.
	sizes            types.Sizes
	integerSize      int
	pointerSize      int
	maximumAlignment int
}

// The architectures, entry for entry the original's, in its order, which is the order a file name's suffix and a
// build line's candidates are tried in. x/tools' asmdecl asmArch386 through asmArchLoong64.
var (
	architecture386 = architecture{
		name:         "386",
		bigEndian:    false,
		stackPointer: "SP",
		linkRegister: false,
	}
	architectureArm = architecture{
		name:         "arm",
		bigEndian:    false,
		stackPointer: "R13",
		linkRegister: true,
	}
	architectureArm64 = architecture{
		name:                      "arm64",
		bigEndian:                 false,
		stackPointer:              "RSP",
		linkRegister:              true,
		resultRegisters:           []string{"R0", "F0"},
		resultWritingInstructions: []string{"SVC"},
	}
	architectureAmd64 = architecture{
		name:                      "amd64",
		bigEndian:                 false,
		stackPointer:              "SP",
		linkRegister:              false,
		resultRegisters:           []string{"AX", "X0"},
		resultWritingInstructions: []string{"SYSCALL"},
	}
	architectureMips = architecture{
		name:         "mips",
		bigEndian:    true,
		stackPointer: "R29",
		linkRegister: true,
	}
	architectureMipsLe = architecture{
		name:         "mipsle",
		bigEndian:    false,
		stackPointer: "R29",
		linkRegister: true,
	}
	architectureMips64 = architecture{
		name:         "mips64",
		bigEndian:    true,
		stackPointer: "R29",
		linkRegister: true,
	}
	architectureMips64Le = architecture{
		name:         "mips64le",
		bigEndian:    false,
		stackPointer: "R29",
		linkRegister: true,
	}
	architecturePpc64 = architecture{
		name:                      "ppc64",
		bigEndian:                 true,
		stackPointer:              "R1",
		linkRegister:              true,
		resultRegisters:           []string{"R3", "F1"},
		resultWritingInstructions: []string{"SYSCALL"},
	}
	architecturePpc64Le = architecture{
		name:                      "ppc64le",
		bigEndian:                 false,
		stackPointer:              "R1",
		linkRegister:              true,
		resultRegisters:           []string{"R3", "F1"},
		resultWritingInstructions: []string{"SYSCALL"},
	}
	architectureRiscv64 = architecture{
		name:                      "riscv64",
		bigEndian:                 false,
		stackPointer:              "SP",
		linkRegister:              true,
		resultRegisters:           []string{"X10", "F10"},
		resultWritingInstructions: []string{"ECALL"},
	}
	architectureS390x = architecture{
		name:         "s390x",
		bigEndian:    true,
		stackPointer: "R15",
		linkRegister: true,
	}
	architectureWasm = architecture{
		name:         "wasm",
		bigEndian:    false,
		stackPointer: "SP",
		linkRegister: false,
	}
	architectureLoong64 = architecture{
		name:                      "loong64",
		bigEndian:                 false,
		stackPointer:              "R3",
		linkRegister:              true,
		resultRegisters:           []string{"R4", "F0"},
		resultWritingInstructions: []string{"SYSCALL"},
	}
)

// architectures is every architecture the pass judges, in the original's order. x/tools' asmdecl arches.
var architectures = []*architecture{
	&architecture386,
	&architectureArm,
	&architectureArm64,
	&architectureAmd64,
	&architectureMips,
	&architectureMipsLe,
	&architectureMips64,
	&architectureMips64Le,
	&architecturePpc64,
	&architecturePpc64Le,
	&architectureRiscv64,
	&architectureS390x,
	&architectureWasm,
	&architectureLoong64,
}

func init() {
	for _, judged := range architectures {
		judged.sizes = types.SizesFor("gc", judged.name)
		if judged.sizes == nil {
			// Unreachable at Go 1.27, whose go/types knows the gc sizes of every architecture above. Kept as the
			// original keeps it, which assumes amd64's sizes for an architecture go/types does not know, and logs
			// a line saying so. The port does not log, since a rule speaks only through its findings.
			judged.sizes = types.SizesFor("gc", "amd64")
		}
		judged.integerSize = int(judged.sizes.Sizeof(types.Typ[types.Int]))
		judged.pointerSize = int(judged.sizes.Sizeof(types.Typ[types.UnsafePointer]))
		judged.maximumAlignment = int(judged.sizes.Alignof(types.Typ[types.Int64]))
	}
}

// architectureNamed is the architecture named name, or nil when the pass judges none by that name.
func architectureNamed(name string) *architecture {
	for _, judged := range architectures {
		if judged.name == name {
			return judged
		}
	}
	return nil
}
