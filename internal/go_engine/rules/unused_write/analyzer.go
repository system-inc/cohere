// Package unused_write is x/tools' unusedwrite pass, a write to a field of a struct or an element of an array that
// nothing reads again, ported onto staticcheck's intermediate representation, which staticcheck's checks already
// build for every package, so the run no longer builds x/tools' single assignment form for this pass.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/unusedwrite/unusedwrite.go, whose
// logic it keeps and whose messages it reports word for word. Where staticcheck's form is built differently from
// x/tools', the code says how, and how the translation still finds what the original finds. The registry runs the
// port in the original's place, and the original stays the oracle: the parity tests beside this file run both over
// the same packages.
//
// The two differ only where adjudicated, each case in testdata/src/differences and held by
// TestPortDiffersFromOriginalOnlyWhereItIsRight. The port finds writes the original misses, rightly: staticcheck lifts
// the reads of a struct that come before its first use needing its address, so such a read, or one on a path the
// write never reaches, no longer keeps a later write alive; it builds no load of an array ranged over by index alone,
// which the Go specification never evaluates; and buildir's functions include the closures of package-level
// initializers, which buildssa's leave out. It misses nothing the original finds: the writes staticcheck's form
// erases when it lifts away the allocation of a pointer to a literal, the element of an array literal of one element
// and the given fields of a struct literal, are found from the function's syntax (reportLiftedArrayLiterals) and from
// the literal's value it leaves (liftedStructLiteralWrites). Where both report a write at an index the function
// computes, the message names the index by the form's registers, as the original's does, and the forms number their
// registers differently, so the text differs in those names alone, an artifact of the single assignment form.
package unused_write

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/ir"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// Name is the original's, which the port takes, so the rule, its settings and every `//cohere:ignore` naming it read
// the same as before the port took the original's place.
const Name = "unusedwrite"

// Doc is the rule's documentation.
const Doc = "Flags a write to a field of a struct or an element of an array that is never read again: x/tools' " +
	"unusedwrite, ported onto staticcheck's intermediate representation."

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{Function: Check}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener.
var Analyzer = dispatch.RuleAnalyzer(rule.Namespace+Name, Name, Doc, &Listener)

// notAField is a write's field when the write is to an array element.
const notAField = -1

// write is one write the pass judges: a store through a field or index address, which both forms build for
// `value.field = x` and `values[index] = x`, or one field of a struct literal staticcheck stores whole into the
// allocation the literal makes, where x/tools stores it field by field, or one field of such a literal whose allocation
// and store staticcheck lifted away.
type write struct {
	// store is the instruction that writes, and nil for a field of a literal lifted away.
	store *ir.Store
	// address is the field or index address store writes through, and nil for a field of a literal stored whole.
	address ir.Instruction
	// object is the struct or array written into, and nil for a field of a literal lifted away.
	object ir.Value
	// field is the field written when object is a struct, and notAField when it is an array.
	field int
	// literalField is the field of a literal stored whole that this write is, and nil for a write through an address.
	literalField *types.Var
	// literalPosition is where the original reports a field of a literal, the colon of a keyed element and the start
	// of a positional one.
	literalPosition token.Pos
	// arrayLiteral is the array literal of one element stored whole whose element this write is, and nil otherwise.
	arrayLiteral *ast.CompositeLit
}

// Check judges one function: each write to a field or an element of a local struct or array, reported when nothing
// after it can read what it wrote.
func Check(context *rule.Context, function *ir.Function) {
	for _, imported := range context.Package.Imports() {
		if imported.Path() == "unsafe" {
			// See golang/go#67684, or testdata/src/importsunsafe: the unusedwrite analyzer may have false positives
			// when used with unsafe.
			return
		}
	}
	reportLiftedArrayLiterals(context, function)
	for _, dead := range append(deadWrites(function), liftedStructLiteralWrites(context, function)...) {
		switch address := dead.address.(type) {
		case *ir.FieldAddr:
			field, isField := fieldOf(address)
			if !isField {
				continue
			}
			context.Report(analysis.Diagnostic{
				Pos:     address.Pos(),
				Message: fmt.Sprintf("unused write to field %s", field.Name()),
			})
		case *ir.IndexAddr:
			context.Report(analysis.Diagnostic{
				Pos:     indexPosition(dead.store, address),
				Message: fmt.Sprintf("unused write to array index %s", address.Index),
			})
		case nil:
			if dead.arrayLiteral != nil {
				position, index, isReported := arrayLiteralElement(context, dead.arrayLiteral)
				if isReported {
					context.Report(analysis.Diagnostic{Pos: position, Message: "unused write to array index " + index})
				}
				continue
			}
			context.Report(analysis.Diagnostic{
				Pos:     dead.literalPosition,
				Message: fmt.Sprintf("unused write to field %s", dead.literalField.Name()),
			})
		}
	}
}

// deadWrites is the writes in function whose address is written to but never used.
func deadWrites(function *ir.Function) []write {
	var reports []write
	candidates := []write{}
	// Visit each block. No need to visit function.Recover.
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			// Identify writes.
			store, isStore := instruction.(*ir.Store)
			if !isStore {
				continue
			}
			// Consider field and index writes to an object whose elements are copied and not shared. MapUpdate is
			// excluded since only the reference of the map is copied.
			candidates = appendWrites(candidates[:0], store)
			for _, candidate := range candidates {
				if isDeadWrite(candidate) {
					reports = append(reports, candidate)
				}
			}
		}
	}
	return reports
}

// appendWrites appends to writes the writes store makes that the original would judge.
func appendWrites(writes []write, store *ir.Store) []write {
	switch address := store.Addr.(type) {
	case *ir.FieldAddr:
		return append(writes, write{store: store, address: address, object: address.X, field: address.Field})
	case *ir.IndexAddr:
		if isLiteralElementOfVariable(address) {
			// x/tools builds this literal in a temporary it then copies whole into the variable, and the copy reads
			// the temporary, so the original never reports one of its elements.
			return writes
		}
		return append(writes, write{store: store, address: address, object: address.X, field: notAField})
	case *ir.Alloc:
		if element, isElement := arrayLiteralElementWrite(store, address); isElement {
			return append(writes, element)
		}
		return append(writes, literalFieldWrites(store, address)...)
	}
	return writes
}

// literalFieldWrites is a write for each field given in a struct literal store writes whole into the allocation the
// literal makes, `&T{field: x}`, where x/tools writes each given field through its own field address. An array
// literal's element is arrayLiteralElementWrite's.
func literalFieldWrites(store *ir.Store, allocation *ir.Alloc) []write {
	literal, value, isLiteral := literalStoredWhole(store)
	if !isLiteral {
		return nil
	}
	pointer, isPointer := typeutil.CoreType(allocation.Type()).(*types.Pointer)
	if !isPointer {
		return nil
	}
	structure, isStruct := typeutil.CoreType(pointer.Elem()).(*types.Struct)
	if !isStruct {
		return nil
	}
	writes := givenFieldWrites(literal, value, structure)
	for index := range writes {
		writes[index].store = store
		writes[index].object = allocation
	}
	return writes
}

// givenFieldWrites is a write for each field literal gives, as value holds it, of structure, the literal's struct,
// with its field and where the original reports it, and no store or object.
func givenFieldWrites(literal *ast.CompositeLit, value *ir.CompositeValue, structure *types.Struct) []write {
	writes := []write{}
	for index, element := range literal.Elts {
		field := index
		position := element.Pos()
		if keyed, isKeyed := element.(*ast.KeyValueExpr); isKeyed {
			key, isIdentifier := keyed.Key.(*ast.Ident)
			if !isIdentifier {
				continue
			}
			field = fieldIndex(structure, key.Name)
			position = keyed.Colon
		}
		if field < 0 || field >= structure.NumFields() || value.Bitmap.Bit(field) == 0 {
			continue
		}
		writes = append(writes, write{field: field, literalField: structure.Field(field), literalPosition: position})
	}
	return writes
}

// isDeadWrite determines whether a field or index write to an object is dead.
func isDeadWrite(candidate write) bool {
	// Consider only struct or array objects.
	if !hasStructOrArrayType(candidate.object) {
		return false
	}
	// Check liveness: if the value is used later, then don't report the write.
	for _, referrer := range *candidate.object.Referrers() {
		if referrer == candidate.store || candidate.address != nil && referrer == candidate.address {
			continue
		}
		switch instruction := referrer.(type) {
		case ir.CallInstruction:
			return false
		case *ir.FieldAddr:
			// Check whether the same field is used.
			if instruction.X == candidate.object && candidate.field != notAField &&
				candidate.field == instruction.Field {
				return false
			}
			// Otherwise another field is used, and this usage doesn't count.
			continue
		case *ir.IndexAddr:
			if isLiteralElementOfVariable(instruction) {
				// Where staticcheck writes this literal's element, x/tools copies a whole temporary into the
				// variable, a store of another value that the original passes over.
				continue
			}
			if instruction.X == candidate.object {
				return false
			}
			continue // Otherwise another object is used
		// x/tools has one Lookup instruction for a map or a string, where staticcheck has these two. Neither can
		// read a struct or an array, so like the original's case they only ever continue.
		case *ir.MapLookup:
			if instruction.X == candidate.object {
				return false
			}
			continue // Otherwise another object is used
		case *ir.StringLookup:
			if instruction.X == candidate.object {
				return false
			}
			continue // Otherwise another object is used
		case *ir.BlankStore:
			// staticcheck records `_ = value` as an instruction that does nothing, where x/tools records nothing, so
			// the original never sees a use here.
			continue
		case *ir.Store:
			if instruction.Val == candidate.object {
				return false
			}
			if literalUsesWrittenElement(instruction, candidate) {
				return false
			}
			continue // Otherwise other object is stored
		default: // consider live if the object is used in any other instruction
			return false
		}
	}
	return true
}

// literalUsesWrittenElement is whether store writes whole a literal into the allocation it makes where x/tools would
// write it element by element through addresses that the original counts as uses of what candidate writes: the
// field address of the same field, or for an array, any element's address.
func literalUsesWrittenElement(store *ir.Store, candidate write) bool {
	_, value, isLiteral := literalStoredWhole(store)
	if !isLiteral || store.Addr != candidate.object {
		return false
	}
	switch typeutil.CoreType(value.Type()).(type) {
	case *types.Struct:
		return candidate.field != notAField && value.Bitmap.Bit(candidate.field) == 1
	case *types.Array:
		return value.NumSet > 0
	}
	return false
}

// literalStoredWhole is the composite literal store writes whole into the allocation the literal makes, as
// staticcheck builds `&T{...}`: the allocation, the value and the store all come from the literal. It is false for a
// literal staticcheck stores into a variable, `value := T{...}`, which x/tools builds in a temporary and copies, and
// for the copy staticcheck makes when it lifts the reads before the literal's first use that needs its address into
// a register: that copy's allocation and value come from the literal too, but the reads lifted before it have seen
// the literal's fields, so it is not the literal's write.
func literalStoredWhole(store *ir.Store) (*ast.CompositeLit, *ir.CompositeValue, bool) {
	value, isComposite := store.Val.(*ir.CompositeValue)
	allocation, isAllocation := store.Addr.(*ir.Alloc)
	if !isComposite || !isAllocation {
		return nil, nil, false
	}
	literal, isLiteral := allocation.Source().(*ast.CompositeLit)
	if !isLiteral || value.Source() != ast.Node(literal) || store.Source() != ast.Node(literal) {
		return nil, nil, false
	}
	return literal, value, true
}

// isLiteralElementOfVariable is whether address is where staticcheck writes an element of an array literal that does
// not give every element straight into the variable it initializes, `values := [3]int{1: x}`. Such an address comes
// from no index expression, and every use of it is a store from the element's own expression. x/tools builds the
// literal in a temporary and copies it whole into the variable instead.
func isLiteralElementOfVariable(address *ir.IndexAddr) bool {
	if _, isIndexExpression := address.Source().(*ast.IndexExpr); isIndexExpression {
		return false
	}
	allocation, isAllocation := address.X.(*ir.Alloc)
	if !isAllocation {
		return false
	}
	if _, isLiteral := allocation.Source().(*ast.CompositeLit); isLiteral {
		return false
	}
	referrers := *address.Referrers()
	if len(referrers) == 0 {
		return false
	}
	for _, referrer := range referrers {
		store, isStore := referrer.(*ir.Store)
		if !isStore || store.Addr != ir.Value(address) || store.Source() != address.Source() {
			return false
		}
	}
	return true
}

// isStructOrArray returns whether the underlying type is struct or array.
func isStructOrArray(typ types.Type) bool {
	switch typ.Underlying().(type) {
	case *types.Array:
		return true
	case *types.Struct:
		return true
	}
	return false
}

// hasStructOrArrayType returns whether a value is of struct or array type.
func hasStructOrArrayType(value ir.Value) bool {
	if allocation, isAllocation := value.(*ir.Alloc); isAllocation {
		// Check the element type of an allocated register (which always has pointer type), e.g., for
		//   func (t T) f() { ...}
		// the receiver object is of type *T:
		//   t0 = local T (t)   *T
		if pointer, isPointer := types.Unalias(allocation.Type()).(*types.Pointer); isPointer {
			return isStructOrArray(pointer.Elem())
		}
		return false
	}
	return isStructOrArray(value.Type())
}

// fieldOf is the struct field address selects. The original asserts the struct and panics on anything else; a field
// address always has one, so the port skips the write instead of crashing should that ever not hold.
func fieldOf(address *ir.FieldAddr) (*types.Var, bool) {
	pointer, isPointer := typeutil.CoreType(address.X.Type()).(*types.Pointer)
	if !isPointer {
		return nil, false
	}
	structure, isStruct := typeutil.CoreType(pointer.Elem()).(*types.Struct)
	if !isStruct || address.Field >= structure.NumFields() {
		return nil, false
	}
	return structure.Field(address.Field), true
}

// fieldIndex is the index of structure's field named name, or notAField.
func fieldIndex(structure *types.Struct, name string) int {
	for index := range structure.NumFields() {
		if structure.Field(index).Name() == name {
			return index
		}
	}
	return notAField
}

// indexPosition is where the original reports a write through address. For `values[index] = x` that is the left
// bracket, which x/tools gives the store, where staticcheck's store sits at the start of its statement and its index
// address holds the index expression. For an element of an array literal that does not give every element, which
// both forms write through an index address, it is the colon of a keyed element and the start of a positional one,
// where staticcheck's address holds the element's value.
func indexPosition(store *ir.Store, address *ir.IndexAddr) token.Pos {
	if indexExpression, isIndexExpression := address.Source().(*ast.IndexExpr); isIndexExpression {
		return indexExpression.Lbrack
	}
	if allocation, isAllocation := address.X.(*ir.Alloc); isAllocation {
		if literal, isLiteral := allocation.Source().(*ast.CompositeLit); isLiteral {
			for _, element := range literal.Elts {
				if keyed, isKeyed := element.(*ast.KeyValueExpr); isKeyed && ast.Node(keyed.Value) == address.Source() {
					return keyed.Colon
				}
				if ast.Node(element) == address.Source() {
					return element.Pos()
				}
			}
		}
	}
	return store.Pos()
}
