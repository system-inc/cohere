// Mutating place visitors: the write-side mirror of visitor.go.
//
// `EachPlace` and friends hand out a Place by value, which is right for every pass that only reads.
// Renaming needs to WRITE one back, and a value copy cannot. So this file mirrors the same variant
// switches with `*Place` instead of `Place`.
//
// # Why this is a mirror rather than a rewrite of visitor.go to use pointers
//
// A pointer-only visitor would serve both, and the reading callers would dereference. It was
// rejected because the reading form is the common one and handing a mutable pointer to a pass that
// only wants to look is how a read-only pass acquires a write it did not intend. The cost is that
// the two switches must stay in step, and that cost is paid by `TestMutatingVisitorCoversEveryValue`
// in ssa_test.go, which walks the instruction set and fails when a variant one visitor handles and
// the other does not.
//
// The roles are visitor.go's and mean the same thing here.
package high_level_intermediate_representation

import "github.com/system-inc/cohere/static_single_assignment"

// EachPlacePointer calls visit for every place the instruction value touches, with the role it
// plays, handing each one out by pointer so the caller can rename it in place.
//
// The instruction's own LValue is NOT included; see EachInstructionPlacePointer.
func EachPlacePointer(value InstructionValue, visit func(place *Place, role PlaceRole)) {
	switch typed := value.(type) {
	case *LoadLocal:
		visit(&typed.Place, PlaceRoleUse)
	case *LoadContext:
		visit(&typed.Place, PlaceRoleUse)
	case *DeclareLocal:
		visit(&typed.LValue, PlaceRoleDefine)
	case *DeclareContext:
		visit(&typed.LValue, PlaceRoleDefine)
	case *StoreLocal:
		visit(&typed.LValue, PlaceRoleDefine)
		visit(&typed.Value, PlaceRoleUse)
	case *StoreContext:
		visit(&typed.LValue, PlaceRoleDefine)
		visit(&typed.Value, PlaceRoleUse)
	case *Destructure:
		eachPatternPlacePointer(typed.LValue, visit)
		visit(&typed.Value, PlaceRoleUse)
	case *LoadGlobal:
		// No places: the name is resolved statically.
	case *StoreGlobal:
		visit(&typed.Value, PlaceRoleUse)
	case *PropertyLoad:
		visit(&typed.Object, PlaceRoleReceiver)
	case *PropertyStore:
		visit(&typed.Object, PlaceRoleReceiver)
		visit(&typed.Value, PlaceRoleUse)
	case *PropertyDelete:
		visit(&typed.Object, PlaceRoleReceiver)
	case *ComputedLoad:
		visit(&typed.Object, PlaceRoleReceiver)
		visit(&typed.Property, PlaceRoleUse)
	case *ComputedStore:
		visit(&typed.Object, PlaceRoleReceiver)
		visit(&typed.Property, PlaceRoleUse)
		visit(&typed.Value, PlaceRoleUse)
	case *ComputedDelete:
		visit(&typed.Object, PlaceRoleReceiver)
		visit(&typed.Property, PlaceRoleUse)
	case *CallExpression:
		visit(&typed.Callee, PlaceRoleUse)
		eachArgumentPlacePointer(typed.Args, visit)
	case *MethodCall:
		visit(&typed.Receiver, PlaceRoleReceiver)
		visit(&typed.Property, PlaceRoleUse)
		eachArgumentPlacePointer(typed.Args, visit)
	case *NewExpression:
		visit(&typed.Callee, PlaceRoleUse)
		eachArgumentPlacePointer(typed.Args, visit)
	case *BinaryExpression:
		visit(&typed.Left, PlaceRoleUse)
		visit(&typed.Right, PlaceRoleUse)
	case *UnaryExpression:
		visit(&typed.Value, PlaceRoleUse)
	case *PrefixUpdate:
		visit(&typed.LValue, PlaceRoleDefine)
		visit(&typed.Value, PlaceRoleUse)
	case *PostfixUpdate:
		visit(&typed.LValue, PlaceRoleDefine)
		visit(&typed.Value, PlaceRoleUse)
	case *Primitive:
		// No places.
	case *RegExpLiteral:
		// No places.
	case *TemplateLiteral:
		for index := range typed.Subexprs {
			visit(&typed.Subexprs[index], PlaceRoleUse)
		}
	case *TaggedTemplateExpression:
		visit(&typed.Tag, PlaceRoleUse)
		for index := range typed.Subexprs {
			visit(&typed.Subexprs[index], PlaceRoleUse)
		}
	case *TypeCastExpression:
		visit(&typed.Value, PlaceRoleUse)
	case *MetaProperty:
		// No places.
	case *ObjectExpression:
		for index := range typed.Properties {
			if typed.Properties[index].ComputedKey != nil {
				visit(typed.Properties[index].ComputedKey, PlaceRoleUse)
			}
			visit(&typed.Properties[index].Value, PlaceRoleUse)
		}
	case *ObjectMethod:
		// The body is a nested function, reached through Function.Functions.
	case *ArrayExpression:
		for index := range typed.Elements {
			if typed.Elements[index].Hole {
				continue
			}
			visit(&typed.Elements[index].Place, PlaceRoleUse)
		}
	case *FunctionExpression:
		for index := range typed.Captures {
			visit(&typed.Captures[index], PlaceRoleUse)
		}
	case *Await:
		visit(&typed.Value, PlaceRoleUse)
	case *GetIterator:
		visit(&typed.Value, PlaceRoleUse)
	case *IteratorNext:
		visit(&typed.Iterator, PlaceRoleReceiver)
		visit(&typed.Collection, PlaceRoleUse)
	case *NextPropertyOf:
		visit(&typed.Value, PlaceRoleUse)
	case *JsxExpression:
		if typed.Tag.Place != nil {
			visit(typed.Tag.Place, PlaceRoleUse)
		}
		for index := range typed.Props {
			visit(&typed.Props[index].Value, PlaceRoleUse)
		}
		for index := range typed.Children {
			visit(&typed.Children[index], PlaceRoleUse)
		}
	case *JsxFragment:
		for index := range typed.Children {
			visit(&typed.Children[index], PlaceRoleUse)
		}
	case *JsxText:
		// No places.
	case *StartMemoize:
		for index := range typed.Deps {
			// See the matching note in `visitor.go`: a global root has no place to rewrite.
			if typed.Deps[index].Root.IsGlobal {
				continue
			}
			visit(&typed.Deps[index].Root.Place, PlaceRoleUse)
		}
	case *FinishMemoize:
		visit(&typed.Value, PlaceRoleUse)
	case *Debugger:
		// No places.
	case *UnsupportedNode:
		// No places: the node is opaque.
	}
}

// EachInstructionPlacePointer calls visit for the instruction's LValue and every place its value
// touches, by pointer.
func EachInstructionPlacePointer(instruction *Instruction, visit func(place *Place, role PlaceRole)) {
	visit(&instruction.LValue, PlaceRoleDefine)
	EachPlacePointer(instruction.Value, visit)
}

// EachTerminalPlacePointer calls visit for every place a terminal reads, by pointer.
//
// A terminal never defines a place except for a catch binding, which mirrors EachTerminalPlace.
func EachTerminalPlacePointer(terminal Terminal, visit func(place *Place, role PlaceRole)) {
	switch typed := terminal.(type) {
	case *Return:
		visit(&typed.Value, PlaceRoleUse)
	case *Throw:
		visit(&typed.Value, PlaceRoleUse)
	case *If:
		visit(&typed.Test, PlaceRoleUse)
	case *Branch:
		visit(&typed.Test, PlaceRoleUse)
	case *Switch:
		visit(&typed.Test, PlaceRoleUse)
		for index := range typed.Cases {
			if typed.Cases[index].Test != nil {
				visit(typed.Cases[index].Test, PlaceRoleUse)
			}
		}
	case *Try:
		if typed.HandlerBinding != nil {
			visit(typed.HandlerBinding, PlaceRoleDefine)
		}
	}
}

func eachArgumentPlacePointer(args []Argument, visit func(place *Place, role PlaceRole)) {
	for index := range args {
		visit(&args[index].Place, PlaceRoleUse)
	}
}

func eachPatternPlacePointer(pattern Pattern, visit func(place *Place, role PlaceRole)) {
	switch typed := pattern.(type) {
	case *PlacePattern:
		visit(&typed.Place, PlaceRoleDefine)
	case *ObjectPattern:
		for index := range typed.Properties {
			if typed.Properties[index].ComputedKey != nil {
				visit(typed.Properties[index].ComputedKey, PlaceRoleUse)
			}
			if typed.Properties[index].Default != nil {
				visit(typed.Properties[index].Default, PlaceRoleUse)
			}
			eachPatternPlacePointer(typed.Properties[index].Value, visit)
		}
		if typed.Rest != nil {
			visit(typed.Rest, PlaceRoleDefine)
		}
	case *ArrayPattern:
		for index := range typed.Elements {
			if typed.Elements[index].Value == nil {
				continue
			}
			if typed.Elements[index].Default != nil {
				visit(typed.Elements[index].Default, PlaceRoleUse)
			}
			eachPatternPlacePointer(typed.Elements[index].Value, visit)
		}
		if typed.Rest != nil {
			visit(typed.Rest, PlaceRoleDefine)
		}
	}
}

// EachBlockReferencePointer visits every block id a terminal names, by pointer so it can be rewritten.
//
// The read-only pair is `EachSuccessor` plus `Fallthrough`, and this is deliberately BOTH of them at
// once rather than a mutating mirror of either. A pass that renames blocks has to rewrite every
// reference a terminal holds, and a fallthrough is a reference even where it is not an edge: a
// `Switch` with a default reaches its fallthrough only through a case, and `EachSuccessor` correctly
// omits it, but a rename that skipped it would leave the terminal pointing at a block that no longer
// exists.
//
// # Why this is not the same shape as `EachSuccessor`
//
// `EachSuccessor` filters through `HasBlock`, because an unset block id means "no such edge" to a
// consumer walking the graph. This does not filter: an unset reference is not a reference to rewrite,
// and passing it through would let a caller write a real id into a slot that means absence.
//
// `TestEachBlockReferencePointerCoversEveryTerminal` guards the arm set the same way its read-only
// siblings are guarded, by scanning this switch against the declared terminals.
func EachBlockReferencePointer(terminal Terminal, visit func(block *static_single_assignment.BlockId)) {
	visitReal := func(block *static_single_assignment.BlockId) {
		if block != nil && HasBlock(*block) {
			visit(block)
		}
	}
	switch typed := terminal.(type) {
	case *Return, *Throw, *Unreachable, *Unsupported:
		// Control leaves the function; no block is named.
	case *Goto:
		visitReal(&typed.Block)
	case *If:
		visitReal(&typed.Consequent)
		visitReal(&typed.Alternate)
		visitReal(&typed.Fallthrough)
	case *Branch:
		visitReal(&typed.Consequent)
		visitReal(&typed.Alternate)
		visitReal(&typed.Fallthrough)
	case *Switch:
		for index := range typed.Cases {
			visitReal(&typed.Cases[index].Block)
		}
		visitReal(&typed.Fallthrough)
	case *While:
		visitReal(&typed.Test)
		visitReal(&typed.Loop)
		visitReal(&typed.Fallthrough)
	case *DoWhile:
		visitReal(&typed.Loop)
		visitReal(&typed.Test)
		visitReal(&typed.Fallthrough)
	case *For:
		visitReal(&typed.Init)
		visitReal(&typed.Test)
		visitReal(&typed.Loop)
		visitReal(&typed.Update)
		visitReal(&typed.Fallthrough)
	case *ForOf:
		visitReal(&typed.Init)
		visitReal(&typed.Test)
		visitReal(&typed.Loop)
		visitReal(&typed.Fallthrough)
	case *ForIn:
		visitReal(&typed.Init)
		visitReal(&typed.Loop)
		visitReal(&typed.Fallthrough)
	case *Logical:
		visitReal(&typed.Test)
		visitReal(&typed.Fallthrough)
	case *Ternary:
		visitReal(&typed.Test)
		visitReal(&typed.Fallthrough)
	case *Optional:
		visitReal(&typed.Test)
		visitReal(&typed.Fallthrough)
	case *Sequence:
		visitReal(&typed.Block)
		visitReal(&typed.Fallthrough)
	case *Label:
		visitReal(&typed.Block)
		visitReal(&typed.Fallthrough)
	case *Try:
		visitReal(&typed.Block)
		visitReal(&typed.Handler)
		visitReal(&typed.Fallthrough)
	case *MaybeThrow:
		visitReal(&typed.Continuation)
		visitReal(&typed.Handler)
	case *Scope:
		visitReal(&typed.Block)
		visitReal(&typed.Fallthrough)
	}
}
