// Package modified_writer_buffer is staticcheck's SA1023, a Write method of io.Writer's signature that writes into
// the buffer it was handed, by an element store or by an append onto it, which io.Writer's contract forbids even for a
// moment, ported onto staticcheck's intermediate representation as a rule.FunctionCheck.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1023/sa1023.go, whose logic it keeps and
// whose message it reports word for word, from the start to the end of the store or the call, where staticcheck's
// report puts it. It reads io.Writer's Write signature from honnef.co/go/tools/knowledge, a table of data rather than
// framework, allowed in this wave and to be ported with the rest of knowledge later, and asks whether a call is to the
// builtin append through honnef.co/go/tools/go/ir/irutil, as the original does. The original walks every source
// function of a package in one run, but keeps nothing from one function to the next: each Write method is judged by
// its own instructions. So the port judges one function at a time and finds what the original finds. The original
// stays the oracle: the parity tests beside this file run both over the same packages, and they differ nowhere. The
// one place the port could differ is a store or a call with no syntax, where staticcheck's report panics and the port
// reports nothing; staticcheck builds every element store and every append from syntax, so none reaches either.
package modified_writer_buffer

import (
	"go/types"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/ir"
	"honnef.co/go/tools/go/ir/irutil"
	"honnef.co/go/tools/knowledge"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{Function: Check}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1023",
	"SA1023",
	"Flags an io.Writer's Write method that modifies the buffer it is given: staticcheck's SA1023, ported onto its "+
		"intermediate representation.",
	&Listener,
)

// message is the original's, word for word.
const message = "io.Writer.Write must not modify the provided buffer, not even temporarily"

// Check judges one function: when it is a method named Write of io.Writer's signature, each store into an element of
// its buffer and each append onto its buffer, the parameter as it was passed, is reported. A slicing of the buffer, or
// a buffer a function literal captures into memory, is another value, and is not judged.
func Check(context *rule.Context, function *ir.Function) {
	if function.Name() != "Write" || function.Signature.Recv() == nil {
		return
	}
	// The signature compared without its receiver, so aliases of byte, of a byte slice, of int or of error read as
	// what they name.
	if !types.Identical(function.Signature, knowledge.Signatures["(io.Writer).Write"]) {
		return
	}
	// The receiver is the first parameter of a method's function, so the buffer is the second.
	buffer := function.Params[1]
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			switch instruction := instruction.(type) {
			case *ir.Store:
				address, isElement := instruction.Addr.(*ir.IndexAddr)
				if !isElement || address.X != buffer {
					continue
				}
			case *ir.Call:
				if !irutil.IsCallTo(instruction.Common(), "append") || instruction.Common().Args[0] != buffer {
					continue
				}
			default:
				continue
			}
			source := instruction.Source()
			if source == nil {
				// An instruction with no syntax has no place to report; staticcheck's report panics on one.
				continue
			}
			context.Report(analysis.Diagnostic{Pos: source.Pos(), End: source.End(), Message: message})
		}
	}
}
