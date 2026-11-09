// Package ineffective_field_assignment is staticcheck's SA4005, a write to a field of a method's value receiver that
// nothing reads afterwards, lost when the method returns, ported onto the engine's own intermediate representation.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4005/sa4005.go, whose logic it keeps and
// whose message it reports word for word, from the start to the end of the store's syntax, where staticcheck's report
// puts it. The original walks every source function of a package in one run, but keeps nothing from one function to
// the next: the reads and writes it gathers belong to one method's receiver. So the port judges one function at a
// time and finds what the original finds. The original gathers the writes in a map keyed by field and ranges over it,
// in an order Go leaves to chance; the port ranges over the fields in order, which changes only the order findings
// are reported in. The registry runs the port in the original's place, under its name, and the original stays only as
// the oracle: the parity tests beside this file run both over the same packages, and they differ nowhere. The places
// the port could differ are those where the original panics: a store with no syntax, which the port does not report,
// and a receiver whose type has no name, which the port passes over; a method's receiver always has a named type, and
// staticcheck builds every store to a field from its assignment.
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

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// Listener is what the port hears of each package from the dispatcher over the engine's own intermediate
// representation: every function, each judged on its own.
var Listener = rule.Listener{IntermediateRepresentation: &rule.IntermediateRepresentationListener{Function: Check}}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4005",
	"SA4005",
	"Flags an assignment to a field of a value receiver that is never observed: staticcheck's SA4005, ported onto "+
		"the engine's own intermediate representation.",
	&Listener,
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
func Check(context *rule.Context, function *intermediate_representation.Function) {
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
	referrers := *function.Parameters[0].Referrers()
	if len(referrers) != 1 {
		return
	}
	copied, isStore := referrers[0].(*intermediate_representation.Store)
	if !isStore {
		return
	}
	local, isAllocation := copied.Address.(*intermediate_representation.Allocation)
	if !isAllocation || local.Heap {
		return
	}
	reads, writes, isJudged := accessesOf(local, copied, structure)
	if !isJudged {
		return
	}
	for field := range structure.NumFields() {
		for _, write := range writes[field] {
			isSeen := slices.ContainsFunc(reads[field], func(read intermediate_representation.Instruction) bool {
				return sees(read, write)
			})
			if isSeen {
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
	local *intermediate_representation.Allocation,
	copied *intermediate_representation.Store,
	structure *types.Struct,
) (map[int][]intermediate_representation.Instruction, map[int][]intermediate_representation.Instruction, bool) {
	reads := map[int][]intermediate_representation.Instruction{}
	writes := map[int][]intermediate_representation.Instruction{}
	for _, referrer := range *local.Referrers() {
		switch referrer := referrer.(type) {
		case *intermediate_representation.FieldAddress:
			for _, access := range *referrer.Referrers() {
				switch access.(type) {
				case *intermediate_representation.Store:
					writes[referrer.Field] = append(writes[referrer.Field], access)
				case *intermediate_representation.Load:
					reads[referrer.Field] = append(reads[referrer.Field], access)
				}
			}
		case *intermediate_representation.Store:
			if referrer != copied {
				return nil, nil, false
			}
		case *intermediate_representation.Load:
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
func sees(read intermediate_representation.Instruction, write intermediate_representation.Instruction) bool {
	if read.Block() == write.Block() {
		return offset(read) > offset(write)
	}
	return intermediate_representation.Reachable(write.Block(), read.Block())
}

// offset is the index of instruction in its block's instructions, or -1 should it not be there, where the original
// panics; an instruction is always among its block's.
func offset(instruction intermediate_representation.Instruction) int {
	return slices.Index(instruction.Block().Instructions, instruction)
}
