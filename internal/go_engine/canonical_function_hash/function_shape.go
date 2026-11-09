package canonical_function_hash

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// typeString names a type by its full package paths, so two packages' `Config` never read alike. Signatures and
// tuples drop their parameter and result names, and a type parameter is named by its index, since go/types prints
// the source names in all three and a name there is not meaning.
func typeString(typ types.Type) string {
	if typ == nil {
		return "<nil type>"
	}
	switch typed := typ.(type) {
	case *types.Signature:
		text := "func(" + typeString(typed.Params()) + ")"
		if typed.Variadic() {
			text += "variadic"
		}
		return text + " " + typeString(typed.Results())
	case *types.Tuple:
		parts := make([]string, typed.Len())
		for index := range parts {
			parts[index] = typeString(typed.At(index).Type())
		}
		return "(" + strings.Join(parts, ",") + ")"
	case *types.TypeParam:
		return fmt.Sprintf("typeparam#%d", typed.Index())
	case *types.Pointer:
		return "*" + typeString(typed.Elem())
	case *types.Slice:
		return "[]" + typeString(typed.Elem())
	case *types.Array:
		return fmt.Sprintf("[%d]%s", typed.Len(), typeString(typed.Elem()))
	case *types.Map:
		return "map[" + typeString(typed.Key()) + "]" + typeString(typed.Elem())
	case *types.Chan:
		return fmt.Sprintf("chan%d ", typed.Dir()) + typeString(typed.Elem())
	}
	return types.TypeString(typ, func(namedIn *types.Package) string { return namedIn.Path() })
}

// pureBuiltins are the builtins with no effect, whose calls float free like any other pure value.
var pureBuiltins = map[string]bool{
	"len": true, "cap": true, "real": true, "imag": true, "complex": true, "min": true, "max": true,
}

// isEffect says whether an instruction takes a place in its block's effect chain: it writes, reads state a write
// could change while advancing it, allocates an object whose identity matters, or leaves the function.
func isEffect(instruction intermediate_representation.Instruction) bool {
	switch typed := instruction.(type) {
	case *intermediate_representation.Call:
		builtin, isBuiltin := typed.Call.Value.(*intermediate_representation.Builtin)
		if isBuiltin && pureBuiltins[builtin.Name()] {
			return false
		}
		return true
	case *intermediate_representation.Go, *intermediate_representation.Defer, *intermediate_representation.Store,
		*intermediate_representation.MapUpdate, *intermediate_representation.Send, *intermediate_representation.Receive,
		*intermediate_representation.Select, *intermediate_representation.Next, *intermediate_representation.Range,
		*intermediate_representation.Allocation, *intermediate_representation.MakeSlice,
		*intermediate_representation.MakeMap, *intermediate_representation.MakeChannel,
		*intermediate_representation.Panic, *intermediate_representation.Return, *intermediate_representation.RunDefers:
		return true
	}
	return false
}

// readsMemory says whether a value reads memory a write could change; it hashes with the chain before it, so it
// floats among other reads but never across a write.
func readsMemory(value intermediate_representation.Value) bool {
	switch value.(type) {
	case *intermediate_representation.Load, *intermediate_representation.MapLookup:
		return true
	}
	return false
}

// isCommutative says whether a binary operator sorts its operand digests: equality always, and arithmetic and
// bitwise operators only on numbers, since string concatenation is not commutative.
func isCommutative(binary *intermediate_representation.BinaryOperation) bool {
	switch binary.Operator {
	case token.EQL, token.NEQ:
		return true
	case token.MUL, token.AND, token.OR, token.XOR, token.ADD:
		return isNumeric(binary.X.Type())
	}
	return false
}

func isNumeric(typ types.Type) bool {
	basic, isBasic := typ.Underlying().(*types.Basic)
	return isBasic && basic.Info()&types.IsNumeric != 0
}

// fieldName is a struct field's name: a name is meaning, beside the index the IR selects by.
func fieldName(typ types.Type, index int, throughPointer bool) string {
	underlying := typ.Underlying()
	if throughPointer {
		pointer, isPointer := underlying.(*types.Pointer)
		if !isPointer {
			return "?"
		}
		underlying = pointer.Elem().Underlying()
	}
	structure, isStruct := underlying.(*types.Struct)
	if !isStruct || index >= structure.NumFields() {
		return "?"
	}
	return structure.Field(index).Name()
}

// functionSize counts the instructions that compute something: every one but a Jump or a BlankStore, which only route
// control or mark a discard.
func functionSize(function *intermediate_representation.Function) int {
	size := 0
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			switch instruction.(type) {
			case *intermediate_representation.Jump, *intermediate_representation.BlankStore:
				continue
			}
			size++
		}
	}
	return size
}

// canonicalBlockOrder is a depth-first preorder from the entry, following successors in their semantic order (an
// If's true successor first), then the recover block, then anything unreached by index. Not the dominator tree's
// preorder: its children come in the order of the builder's block indices, which follow source layout.
func canonicalBlockOrder(function *intermediate_representation.Function) []*intermediate_representation.BasicBlock {
	visited := map[*intermediate_representation.BasicBlock]bool{}
	var order []*intermediate_representation.BasicBlock
	visit := func(block *intermediate_representation.BasicBlock) {
		stack := []*intermediate_representation.BasicBlock{block}
		for len(stack) > 0 {
			current := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if visited[current] {
				continue
			}
			visited[current] = true
			order = append(order, current)
			for index := len(current.Successors) - 1; index >= 0; index-- {
				if !visited[current.Successors[index]] {
					stack = append(stack, current.Successors[index])
				}
			}
		}
	}
	visit(function.Blocks[0])
	if function.Recover != nil {
		visit(function.Recover)
	}
	for _, block := range function.Blocks {
		if !visited[block] {
			visit(block)
		}
	}
	return order
}
