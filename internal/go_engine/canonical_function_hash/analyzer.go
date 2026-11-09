package canonical_function_hash

import (
	"go/ast"
	"reflect"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/duplicate_functions"
	"github.com/system-inc/cohere/internal/go_engine/copied"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
)

// PackageFunctions is one package's functions as the hash read them, the Analyzer's result, which the engine's
// findings cache keeps in the package's entry so a package it restores still has them.
type PackageFunctions struct {
	Functions []duplicate_functions.Function
	Refused   []duplicate_functions.Refusal
	Unread    []Unread
}

// Unread is one function never read, and why. Kept one by one rather than counted, since the engine reads every
// function of a package once more in its test variant and counts each once (see the engine's duplicates report).
type Unread struct {
	File   string
	Line   int
	Name   string
	Reason duplicate_functions.NotReadReason
}

// Analyzer hashes every source function of a package, reading the engine's own intermediate representation, the one
// build_intermediate_representation builds for the rules on it. It is no rule: it reports nothing, and the engine runs
// it only under `--duplicates`.
var Analyzer = &analysis.Analyzer{
	Name: "canonical_function_hash",
	Doc: "hashes every source function of the package canonically, for the duplicates report: equal digests mean " +
		"the functions compute the same thing",
	Requires:   []*analysis.Analyzer{build_intermediate_representation.Analyzer},
	ResultType: reflect.TypeFor[*PackageFunctions](),
	Run:        run,
}

func run(pass *analysis.Pass) (any, error) {
	built, readError := build_intermediate_representation.ResultOf(pass)
	if readError != nil {
		return nil, readError
	}
	generated := map[string]bool{}
	copies := map[string]bool{}
	for _, file := range pass.Files {
		name := pass.Fset.PositionFor(file.Package, false).Filename
		generated[name] = ast.IsGenerated(file)
		copies[name] = copied.IsCopied(file)
	}
	hasher := newPackageHasher()
	result := &PackageFunctions{}
	for function := range built.SourceFunctions() {
		// A wrapper, a thunk or the package's init is the compiler's, not a function anyone wrote.
		if function.Synthetic != "" {
			continue
		}
		position := pass.Fset.PositionFor(function.Pos(), false)
		name := function.RelativeString(pass.Pkg)
		unread := Unread{File: position.Filename, Line: position.Line, Name: name}
		switch {
		case generated[position.Filename]:
			unread.Reason = duplicate_functions.NotReadGenerated
		case copies[position.Filename]:
			unread.Reason = duplicate_functions.NotReadCopied
		case function.Blocks == nil:
			unread.Reason = duplicate_functions.NotReadBodyless
		}
		if unread.Reason != "" {
			result.Unread = append(result.Unread, unread)
			continue
		}
		digest, unsupported := hashFunction(hasher, function)
		if unsupported != "" {
			result.Refused = append(result.Refused, duplicate_functions.Refusal{
				File: position.Filename, Line: position.Line, Name: name,
				Reason: duplicate_functions.RefusalUnsupportedInstruction, Detail: unsupported,
			})
			continue
		}
		result.Functions = append(result.Functions, duplicate_functions.Function{
			File: position.Filename, Line: position.Line, Name: name, Size: functionSize(function), Digest: digest,
		})
	}
	return result, nil
}

// hashFunction is one function's digest, or the instruction it holds that the hash does not model. Any other panic
// is the analyzer's crash, which the engine's recovering run reports as one.
//
// Mutates: hasher forgets which closures were being hashed when one is refused, so a refusal deep in a closure never
// leaves a later function reading that closure by its name
func hashFunction(
	hasher *packageHasher,
	function *intermediate_representation.Function,
) (digest duplicate_functions.Digest, unsupported string) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		refused, isRefusal := recovered.(unsupportedInstruction)
		if !isRefusal {
			panic(recovered)
		}
		clear(hasher.inProgress)
		unsupported = refused.instruction
	}()
	return hasher.digest(function), ""
}
