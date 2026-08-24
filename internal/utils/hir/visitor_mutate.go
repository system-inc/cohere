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
package hir

// EachPlacePointer calls visit for every place the instruction value touches, with the role it
// plays, handing each one out by pointer so the caller can rename it in place.
//
// The instruction's own LValue is NOT included; see EachInstructionPlacePointer.
func EachPlacePointer(value InstructionValue, visit func(place *Place, role PlaceRole)) {
	switch v := value.(type) {
	case *LoadLocal:
		visit(&v.Place, PlaceRoleUse)
	case *LoadContext:
		visit(&v.Place, PlaceRoleUse)
	case *DeclareLocal:
		visit(&v.LValue, PlaceRoleDefine)
	case *DeclareContext:
		visit(&v.LValue, PlaceRoleDefine)
	case *StoreLocal:
		visit(&v.LValue, PlaceRoleDefine)
		visit(&v.Value, PlaceRoleUse)
	case *StoreContext:
		visit(&v.LValue, PlaceRoleDefine)
		visit(&v.Value, PlaceRoleUse)
	case *Destructure:
		eachPatternPlacePointer(v.LValue, visit)
		visit(&v.Value, PlaceRoleUse)
	case *LoadGlobal:
		// No places: the name is resolved statically.
	case *StoreGlobal:
		visit(&v.Value, PlaceRoleUse)
	case *PropertyLoad:
		visit(&v.Object, PlaceRoleReceiver)
	case *PropertyStore:
		visit(&v.Object, PlaceRoleReceiver)
		visit(&v.Value, PlaceRoleUse)
	case *PropertyDelete:
		visit(&v.Object, PlaceRoleReceiver)
	case *ComputedLoad:
		visit(&v.Object, PlaceRoleReceiver)
		visit(&v.Property, PlaceRoleUse)
	case *ComputedStore:
		visit(&v.Object, PlaceRoleReceiver)
		visit(&v.Property, PlaceRoleUse)
		visit(&v.Value, PlaceRoleUse)
	case *ComputedDelete:
		visit(&v.Object, PlaceRoleReceiver)
		visit(&v.Property, PlaceRoleUse)
	case *CallExpression:
		visit(&v.Callee, PlaceRoleUse)
		eachArgumentPlacePointer(v.Args, visit)
	case *MethodCall:
		visit(&v.Receiver, PlaceRoleReceiver)
		visit(&v.Property, PlaceRoleUse)
		eachArgumentPlacePointer(v.Args, visit)
	case *NewExpression:
		visit(&v.Callee, PlaceRoleUse)
		eachArgumentPlacePointer(v.Args, visit)
	case *BinaryExpression:
		visit(&v.Left, PlaceRoleUse)
		visit(&v.Right, PlaceRoleUse)
	case *UnaryExpression:
		visit(&v.Value, PlaceRoleUse)
	case *PrefixUpdate:
		visit(&v.LValue, PlaceRoleDefine)
		visit(&v.Value, PlaceRoleUse)
	case *PostfixUpdate:
		visit(&v.LValue, PlaceRoleDefine)
		visit(&v.Value, PlaceRoleUse)
	case *Primitive:
		// No places.
	case *RegExpLiteral:
		// No places.
	case *TemplateLiteral:
		for index := range v.Subexprs {
			visit(&v.Subexprs[index], PlaceRoleUse)
		}
	case *TaggedTemplateExpression:
		visit(&v.Tag, PlaceRoleUse)
		for index := range v.Subexprs {
			visit(&v.Subexprs[index], PlaceRoleUse)
		}
	case *TypeCastExpression:
		visit(&v.Value, PlaceRoleUse)
	case *MetaProperty:
		// No places.
	case *ObjectExpression:
		for index := range v.Properties {
			if v.Properties[index].ComputedKey != nil {
				visit(v.Properties[index].ComputedKey, PlaceRoleUse)
			}
			visit(&v.Properties[index].Value, PlaceRoleUse)
		}
	case *ObjectMethod:
		// The body is a nested function, reached through Function.Functions.
	case *ArrayExpression:
		for index := range v.Elements {
			if v.Elements[index].Hole {
				continue
			}
			visit(&v.Elements[index].Place, PlaceRoleUse)
		}
	case *FunctionExpression:
		for index := range v.Captures {
			visit(&v.Captures[index], PlaceRoleUse)
		}
	case *Await:
		visit(&v.Value, PlaceRoleUse)
	case *GetIterator:
		visit(&v.Value, PlaceRoleUse)
	case *IteratorNext:
		visit(&v.Iterator, PlaceRoleReceiver)
		visit(&v.Collection, PlaceRoleUse)
	case *NextPropertyOf:
		visit(&v.Value, PlaceRoleUse)
	case *JsxExpression:
		if v.Tag.Place != nil {
			visit(v.Tag.Place, PlaceRoleUse)
		}
		for index := range v.Props {
			visit(&v.Props[index].Value, PlaceRoleUse)
		}
		for index := range v.Children {
			visit(&v.Children[index], PlaceRoleUse)
		}
	case *JsxFragment:
		for index := range v.Children {
			visit(&v.Children[index], PlaceRoleUse)
		}
	case *JsxText:
		// No places.
	case *StartMemoize:
		for index := range v.Deps {
			visit(&v.Deps[index], PlaceRoleUse)
		}
	case *FinishMemoize:
		visit(&v.Value, PlaceRoleUse)
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
	switch t := terminal.(type) {
	case *Return:
		visit(&t.Value, PlaceRoleUse)
	case *Throw:
		visit(&t.Value, PlaceRoleUse)
	case *If:
		visit(&t.Test, PlaceRoleUse)
	case *Branch:
		visit(&t.Test, PlaceRoleUse)
	case *Switch:
		visit(&t.Test, PlaceRoleUse)
		for index := range t.Cases {
			if t.Cases[index].Test != nil {
				visit(t.Cases[index].Test, PlaceRoleUse)
			}
		}
	case *Try:
		if t.HandlerBinding != nil {
			visit(t.HandlerBinding, PlaceRoleDefine)
		}
	}
}

func eachArgumentPlacePointer(args []Argument, visit func(place *Place, role PlaceRole)) {
	for index := range args {
		visit(&args[index].Place, PlaceRoleUse)
	}
}

func eachPatternPlacePointer(pattern Pattern, visit func(place *Place, role PlaceRole)) {
	switch p := pattern.(type) {
	case *PlacePattern:
		visit(&p.Place, PlaceRoleDefine)
	case *ObjectPattern:
		for index := range p.Properties {
			if p.Properties[index].ComputedKey != nil {
				visit(p.Properties[index].ComputedKey, PlaceRoleUse)
			}
			if p.Properties[index].Default != nil {
				visit(p.Properties[index].Default, PlaceRoleUse)
			}
			eachPatternPlacePointer(p.Properties[index].Value, visit)
		}
		if p.Rest != nil {
			visit(p.Rest, PlaceRoleDefine)
		}
	case *ArrayPattern:
		for index := range p.Elements {
			if p.Elements[index].Value == nil {
				continue
			}
			if p.Elements[index].Default != nil {
				visit(p.Elements[index].Default, PlaceRoleUse)
			}
			eachPatternPlacePointer(p.Elements[index].Value, visit)
		}
		if p.Rest != nil {
			visit(p.Rest, PlaceRoleDefine)
		}
	}
}
