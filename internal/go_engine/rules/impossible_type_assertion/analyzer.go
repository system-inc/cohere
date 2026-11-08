// Package impossible_type_assertion is staticcheck's SA5010, an assertion from one interface to another that can
// never hold, since both have a method of one name with signatures that contradict each other, ported onto
// staticcheck's intermediate representation as a rule.FunctionCheck.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa5010/sa5010.go, whose logic it keeps and
// whose message it reports word for word, from the start to the end of the assertion's syntax, where staticcheck's
// report puts it. The original walks every source function of a package in one run, but keeps nothing from one
// function to the next: each assertion is judged alone, by the method sets of its two types. It reads those sets from
// the program's method set cache, which it reaches through staticcheck's buildir result; the port reaches the same
// cache through the function's own program, the one buildir built, and a cache only spares work, never changes a
// set. So the port judges one function at a time and finds what the original finds. The original stays the oracle:
// the parity tests beside this file run both over the same packages, and they differ nowhere. The places the port
// could differ are an assertion with no syntax, where staticcheck's report panics and the port reports nothing, and a
// method set entry that is no function, where the original's unchecked assertion panics and the port passes over the
// method; staticcheck builds every assertion from its syntax, and a method set holds only functions, so neither
// reaches either.
package impossible_type_assertion

import (
	"fmt"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{Function: Check}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA5010",
	"SA5010",
	"Flags an assertion from one interface to another whose methods contradict each other: staticcheck's SA5010, "+
		"ported onto its intermediate representation.",
	&Listener,
)

// contradiction is one method both types of an assertion have, with signatures that cannot be one.
type contradiction struct {
	// have is the method of the asserted value's type.
	have *types.Func
	// want is the method of the asserted interface.
	want *types.Func
}

// Check judges one function: each assertion to an interface whose methods contradict those of the value's type.
func Check(context *rule.Context, function *ir.Function) {
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			assertion, isAssertion := instruction.(*ir.TypeAssert)
			if !isAssertion {
				continue
			}
			asserted, isInterface := assertion.AssertedType.Underlying().(*types.Interface)
			if !isInterface {
				// The compiler already refuses an impossible assertion to a concrete type, so only an assertion
				// between interfaces is judged.
				continue
			}
			contradictions, isJudged := contradictionsOf(function.Prog, assertion.X.Type(), asserted)
			if !isJudged || len(contradictions) == 0 {
				continue
			}
			source := assertion.Source()
			if source == nil {
				// An assertion with no syntax has no place to report; staticcheck's report panics on one.
				continue
			}
			context.Report(analysis.Diagnostic{
				Pos:     source.Pos(),
				End:     source.End(),
				Message: messageOf(context.Package, assertion, contradictions),
			})
		}
	}
}

// contradictionsOf is every method of asserted that the method set of from has under the same name with a signature
// not assignable to it, in asserted's order, and whether the assertion could be judged at all. One generic method on
// either side gives up the whole assertion, as the original does, since go/types exports no unification to compare
// a generic signature by.
func contradictionsOf(program *ir.Program, from types.Type, asserted *types.Interface) ([]contradiction, bool) {
	methods := program.MethodSets.MethodSet(from)
	contradictions := []contradiction{}
	for wanted := range asserted.Methods() {
		selection := methods.Lookup(wanted.Pkg(), wanted.Name())
		if selection == nil {
			continue
		}
		had, isFunction := selection.Obj().(*types.Func)
		if !isFunction {
			// A method set holds methods alone, so no selection reaches here.
			continue
		}
		if had.Origin() != had || wanted.Origin() != wanted {
			// A method of an instantiated generic type is not its own origin, and it drops the contradictions found
			// before it too.
			return nil, false
		}
		if types.AssignableTo(had.Type(), wanted.Type()) {
			continue
		}
		contradictions = append(contradictions, contradiction{have: had, want: wanted})
	}
	return contradictions, true
}

// messageOf is the original's message, word for word: the two types, written relative to the package, then each
// contradicting method with both signatures, which the original writes with their packages in full.
func messageOf(typesPackage *types.Package, assertion *ir.TypeAssert, contradictions []contradiction) string {
	var message strings.Builder
	fmt.Fprintf(&message, "impossible type assertion; %s and %s contradict each other:",
		types.TypeString(assertion.X.Type(), types.RelativeTo(typesPackage)),
		types.TypeString(assertion.AssertedType, types.RelativeTo(typesPackage)))
	for _, contradicting := range contradictions {
		fmt.Fprintf(&message, "\n\twrong type for %s method", contradicting.have.Name())
		fmt.Fprintf(&message, "\n\t\thave %s", contradicting.have.Type())
		fmt.Fprintf(&message, "\n\t\twant %s", contradicting.want.Type())
	}
	return message.String()
}
