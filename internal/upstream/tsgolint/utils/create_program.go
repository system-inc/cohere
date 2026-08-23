package utils

import (
	"context"
	"fmt"

	"github.com/microsoft/TypeScript/tsc/shim/bundled"
	"github.com/microsoft/TypeScript/tsc/shim/compiler"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/tsoptions"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/microsoft/TypeScript/tsc/shim/vfs"
)

func CreateCompilerHost(cwd string, fs vfs.FS) compiler.CompilerHost {
	defaultLibraryPath := bundled.LibPath()
	// Three arguments added upstream in the move to `microsoft/TypeScript`: an extended-config cache,
	// a trace sink, and a content mapper. All three are optional and nil is the one-shot answer.
	return compiler.NewCompilerHost(cwd, fs, defaultLibraryPath, nil, nil, nil)
}

func CreateProgram(singleThreaded bool, fs vfs.FS, cwd string, tsconfigPath string, host compiler.CompilerHost) (*compiler.Program, error) {
	resolvedConfigPath := tspath.ResolvePath(cwd, tsconfigPath)
	if !fs.FileExists(resolvedConfigPath) {
		return nil, fmt.Errorf("couldn't read tsconfig at %v", resolvedConfigPath)
	}

	configParseResult, _ := tsoptions.GetParsedCommandLineOfConfigFile(tsconfigPath, &core.CompilerOptions{}, nil, host, nil)

	opts := compiler.ProgramOptions{
		Config:         configParseResult,
		SingleThreaded: core.TSTrue,
		Host:           host,
	}
	if !singleThreaded {
		opts.SingleThreaded = core.TSFalse
	}
	program := compiler.NewProgram(opts)
	if program == nil {
		return nil, fmt.Errorf("couldn't create program")
	}

	diagnostics := program.GetSyntacticDiagnostics(context.Background(), nil)
	if len(diagnostics) != 0 {
		return nil, fmt.Errorf("found %v syntactic errors. Try running \"tsgo --noEmit\" first\n", len(diagnostics))
	}

	program.BindSourceFiles()

	// program.CreateCheckers()

	return program, nil
}
