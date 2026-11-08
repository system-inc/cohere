// Package ineffective_field_assignment is staticcheck's SA4005, a write to a field of a method's value receiver that
// nothing reads afterwards, lost when the method returns, ported onto staticcheck's intermediate representation as a
// rule.FunctionCheck.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4005/sa4005.go, whose logic it keeps and
// whose message it reports word for word, from the start to the end of the store's syntax, where staticcheck's report
// puts it. The original walks every source function of a package in one run, but keeps nothing from one function to
// the next: the reads and writes it gathers belong to one method's receiver. So the port judges one function at a
// time and finds what the original finds. The original gathers the writes in a map keyed by field and ranges over it,
// in an order Go leaves to chance; the port ranges over the fields in order, which changes only the order findings
// are reported in. The original stays the oracle: the parity tests beside this file run both over the same packages,
// and they differ nowhere. The places the port could differ are those where the original panics: a store with no
// syntax, which the port does not report, and a receiver whose type has no name, which the port passes over; a
// method's receiver always has a named type, and staticcheck builds every store to a field from its assignment.
//
// The port keeps one false positive the original makes, so as to find what it finds: a read earlier in the write's own
// block does not see the write, even when a loop brings control back to the read, so a loop's body that reads a field
// and then writes it is reported though the next turn reads the write (parity's loopBodyReadFirst). Fixing it is a
// difference from the original, to be adjudicated on its own.
package ineffective_field_assignment

import (
	"fmt"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/ir"
	"honnef.co/go/tools/go/ir/irutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_ir"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = staticcheck_ir.FunctionCheckAnalyzer(
	"SA4005",
	"Flags an assignment to a field of a value receiver that is never observed: staticcheck's SA4005, ported onto "+
		"its intermediate representation.",
	Check,
)

// namedType is a type with a name of its own, *types.Named or *types.Alias, the name the message gives the receiver.
type namedType interface {
	Obj() *types.TypeName
}

// Check judges one function: when it is a method on a struct whose receiver is copied into a local the function
// keeps to itself, each write to a first level field of that local that no later read can see.
//
// It considers only the receiver and its first level fields, not other parameters nor nested fields, and only writes
// to a field never read after the write: it does not flag `a.x = 1; a.x = 2; _ = a.x`.
func Check(context *rule.Context, function *ir.Function) {
	receiver := function.Signature.Recv()
	if receiver == nil {
		return
	}
	structure, isStruct := receiver.Type().Underlying().(*types.Struct)
	if !isStruct {
		return
	}
	named, isNamed := receiver.Type().(namedType)
	if !isNamed {
		return
	}
	// The receiver parameter's one reader is the store that copies it into a local of its own. Another reader, or a
	// local that escapes to the heap, could see the fields some other way.
	referrers := *function.Params[0].Referrers()
	if len(referrers) != 1 {
		return
	}
	copied, isStore := referrers[0].(*ir.Store)
	if !isStore {
		return
	}
	local, isAllocation := copied.Addr.(*ir.Alloc)
	if !isAllocation || local.Heap {
		return
	}
	reads, writes, isJudged := accessesOf(local, copied, structure)
	if !isJudged {
		return
	}
	for field := range structure.NumFields() {
		for _, write := range writes[field] {
			if slices.ContainsFunc(reads[field], func(read ir.Instruction) bool { return sees(read, write) }) {
				continue
			}
			source := write.Source()
			if source == nil {
				// A store with no syntax has no place to report; staticcheck's report panics on one.
				continue
			}
			context.Report(analysis.Diagnostic{
				Pos: source.Pos(),
				End: source.End(),
				Message: fmt.Sprintf("ineffective assignment to field %s.%s", named.Obj().Name(),
					structure.Field(field).Name()),
			})
		}
	}
}

// accessesOf is the reads and writes of each field of local, the receiver's copy, by field index, and whether the
// function is one to judge at all. A store through a field's address is a write of the field and a load through it is
// a read; any other use of the address is passed over, since an address that escapes would have put local on the
// heap. A load of local whole reads every field. It is false when the function stores into local whole again, since
// what a partial literal writes to the fields it leaves out is not decided here, or uses local any other way.
func accessesOf(
	local *ir.Alloc,
	copied *ir.Store,
	structure *types.Struct,
) (map[int][]ir.Instruction, map[int][]ir.Instruction, bool) {
	reads := map[int][]ir.Instruction{}
	writes := map[int][]ir.Instruction{}
	for _, referrer := range *local.Referrers() {
		switch referrer := referrer.(type) {
		case *ir.FieldAddr:
			for _, access := range *referrer.Referrers() {
				switch access.(type) {
				case *ir.Store:
					writes[referrer.Field] = append(writes[referrer.Field], access)
				case *ir.Load:
					reads[referrer.Field] = append(reads[referrer.Field], access)
				}
			}
		case *ir.Store:
			if referrer != copied {
				return nil, nil, false
			}
		case *ir.Load:
			for field := range structure.NumFields() {
				reads[field] = append(reads[field], referrer)
			}
		default:
			return nil, nil, false
		}
	}
	return reads, writes, true
}

// sees is whether read can observe write: later in the same block, or in a block reachable from the write's. A read
// earlier in the write's own block counts for nothing even when a loop brings control back to it, as in the original.
func sees(read ir.Instruction, write ir.Instruction) bool {
	if read.Block() == write.Block() {
		return offset(read) > offset(write)
	}
	return irutil.Reachable(write.Block(), read.Block())
}

// offset is the index of instruction in its block's instructions, or -1 should it not be there, where the original
// panics; an instruction is always among its block's.
func offset(instruction ir.Instruction) int {
	return slices.Index(instruction.Block().Instrs, instruction)
}
