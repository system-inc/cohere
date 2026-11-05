// Visitor plumbing: the generic walks over an instruction's places, a terminal's successors, and a
// function's blocks.
//
// # Why these are functions and not an interface
//
// The obvious shape for a visitor in Go is an interface with one method per node type, which a pass
// embeds a default implementation of. That was rejected. A pass over this IR almost always cares
// about two or three of the 43 instruction values and wants to treat the rest uniformly - "every
// place this instruction reads" rather than "the second operand of a binary expression". An
// interface makes the uniform case the expensive one, since a pass must implement or inherit 43
// methods to express it.
//
// So the primitive is `EachPlace`, which yields every place an instruction touches without the
// caller knowing which variant it is, and a pass that needs a specific variant type-switches for
// it directly. That is the same division upstream's `visitors.rs` lands on after 1,147 lines.
//
// # Exhaustiveness
//
// Go cannot check a type switch for completeness, so `EachPlace` and `EachSuccessor` are the single
// place where all 43 instructions and all 20 terminals are enumerated. `TestEachPlaceCoversEvery
// InstructionValue` reflects over the package's variants and fails when one is missing a case, so a
// variant added without updating these goes red rather than silently contributing no places.
package high_level_intermediate_representation

import "github.com/system-inc/cohere/static_single_assignment"

// PlaceRole is why an instruction touches a place.
//
// A pass computing effects needs to distinguish the value being stored from the object being stored
// into, and a pass computing liveness needs to know which places are defined here rather than used.
// Both questions are answerable from the variant, but every pass asking them would re-derive the
// same mapping, so it is stated once here.
type PlaceRole uint8

const (
	// PlaceRoleUse is a place whose value is read.
	PlaceRoleUse PlaceRole = iota
	// PlaceRoleDefine is a place this instruction assigns to, including the instruction's LValue.
	PlaceRoleDefine
	// PlaceRoleReceiver is the object of a member access, method call, or property store.
	//
	// Separate from Use because it is the place a mutation attaches to: `o.f()` may mutate `o`, and
	// an effect pass wants the receiver distinguishable without re-matching the variant.
	PlaceRoleReceiver
)

// EachPlace calls visit for every place the instruction value touches, with the role it plays.
//
// The instruction's own LValue is not included; it is on the Instruction rather than the value, and
// `EachInstructionPlace` is the version that includes it.
//
// Places inside a nested function are not visited. A FunctionExpression yields its captures, which
// are places in this function, and the nested body is reached through Function.Functions.
func EachPlace(value InstructionValue, visit func(place Place, role PlaceRole)) {
	switch typed := value.(type) {
	case *LoadLocal:
		visit(typed.Place, PlaceRoleUse)
	case *LoadContext:
		visit(typed.Place, PlaceRoleUse)
	case *DeclareLocal:
		visit(typed.LValue, PlaceRoleDefine)
	case *DeclareContext:
		visit(typed.LValue, PlaceRoleDefine)
	case *StoreLocal:
		visit(typed.LValue, PlaceRoleDefine)
		visit(typed.Value, PlaceRoleUse)
	case *StoreContext:
		visit(typed.LValue, PlaceRoleDefine)
		visit(typed.Value, PlaceRoleUse)
	case *Destructure:
		eachPatternPlace(typed.LValue, visit)
		visit(typed.Value, PlaceRoleUse)
	case *LoadGlobal:
		// No places: the name is resolved statically.
	case *StoreGlobal:
		visit(typed.Value, PlaceRoleUse)
	case *PropertyLoad:
		visit(typed.Object, PlaceRoleReceiver)
	case *PropertyStore:
		visit(typed.Object, PlaceRoleReceiver)
		visit(typed.Value, PlaceRoleUse)
	case *PropertyDelete:
		visit(typed.Object, PlaceRoleReceiver)
	case *ComputedLoad:
		visit(typed.Object, PlaceRoleReceiver)
		visit(typed.Property, PlaceRoleUse)
	case *ComputedStore:
		visit(typed.Object, PlaceRoleReceiver)
		visit(typed.Property, PlaceRoleUse)
		visit(typed.Value, PlaceRoleUse)
	case *ComputedDelete:
		visit(typed.Object, PlaceRoleReceiver)
		visit(typed.Property, PlaceRoleUse)
	case *CallExpression:
		visit(typed.Callee, PlaceRoleUse)
		eachArgumentPlace(typed.Args, visit)
	case *MethodCall:
		visit(typed.Receiver, PlaceRoleReceiver)
		visit(typed.Property, PlaceRoleUse)
		eachArgumentPlace(typed.Args, visit)
	case *NewExpression:
		visit(typed.Callee, PlaceRoleUse)
		eachArgumentPlace(typed.Args, visit)
	case *BinaryExpression:
		visit(typed.Left, PlaceRoleUse)
		visit(typed.Right, PlaceRoleUse)
	case *UnaryExpression:
		visit(typed.Value, PlaceRoleUse)
	case *PrefixUpdate:
		visit(typed.LValue, PlaceRoleDefine)
		visit(typed.Value, PlaceRoleUse)
	case *PostfixUpdate:
		visit(typed.LValue, PlaceRoleDefine)
		visit(typed.Value, PlaceRoleUse)
	case *Primitive:
		// No places.
	case *RegExpLiteral:
		// No places.
	case *TemplateLiteral:
		for _, subexpr := range typed.Subexprs {
			visit(subexpr, PlaceRoleUse)
		}
	case *TaggedTemplateExpression:
		visit(typed.Tag, PlaceRoleUse)
		for _, subexpr := range typed.Subexprs {
			visit(subexpr, PlaceRoleUse)
		}
	case *TypeCastExpression:
		visit(typed.Value, PlaceRoleUse)
	case *MetaProperty:
		// No places.
	case *ObjectExpression:
		for _, property := range typed.Properties {
			if property.ComputedKey != nil {
				visit(*property.ComputedKey, PlaceRoleUse)
			}
			visit(property.Value, PlaceRoleUse)
		}
	case *ObjectMethod:
		// The body is a nested function, reached through Function.Functions.
	case *ArrayExpression:
		for _, element := range typed.Elements {
			if element.Hole {
				continue
			}
			visit(element.Place, PlaceRoleUse)
		}
	case *FunctionExpression:
		for _, capture := range typed.Captures {
			visit(capture, PlaceRoleUse)
		}
	case *Await:
		visit(typed.Value, PlaceRoleUse)
	case *GetIterator:
		visit(typed.Value, PlaceRoleUse)
	case *IteratorNext:
		visit(typed.Iterator, PlaceRoleReceiver)
		visit(typed.Collection, PlaceRoleUse)
	case *NextPropertyOf:
		visit(typed.Value, PlaceRoleUse)
	case *JsxExpression:
		if typed.Tag.Place != nil {
			visit(*typed.Tag.Place, PlaceRoleUse)
		}
		for _, property := range typed.Props {
			visit(property.Value, PlaceRoleUse)
		}
		for _, child := range typed.Children {
			visit(child, PlaceRoleUse)
		}
	case *JsxFragment:
		for _, child := range typed.Children {
			visit(child, PlaceRoleUse)
		}
	case *JsxText:
		// No places.
	case *StartMemoize:
		for _, dep := range typed.Deps {
			// A global-rooted dependency carries a name rather than a place, so it holds a zero
			// `Place` that names no identifier. Visiting it would hand every operand consumer an
			// identifier id of zero, which single-assignment renaming would then treat as a real
			// binding.
			if dep.Root.IsGlobal {
				continue
			}
			visit(dep.Root.Place, PlaceRoleUse)
		}
	case *FinishMemoize:
		visit(typed.Value, PlaceRoleUse)
	case *Debugger:
		// No places.
	case *UnsupportedNode:
		// No places: the node is opaque.
	}
}

// EachInstructionPlace calls visit for the instruction's LValue and every place its value touches.
func EachInstructionPlace(instruction *Instruction, visit func(place Place, role PlaceRole)) {
	visit(instruction.LValue, PlaceRoleDefine)
	EachPlace(instruction.Value, visit)
}

func eachArgumentPlace(args []Argument, visit func(place Place, role PlaceRole)) {
	for _, arg := range args {
		visit(arg.Place, PlaceRoleUse)
	}
}

func eachPatternPlace(pattern Pattern, visit func(place Place, role PlaceRole)) {
	switch typed := pattern.(type) {
	case *PlacePattern:
		visit(typed.Place, PlaceRoleDefine)
	case *ObjectPattern:
		for _, property := range typed.Properties {
			if property.ComputedKey != nil {
				visit(*property.ComputedKey, PlaceRoleUse)
			}
			if property.Default != nil {
				visit(*property.Default, PlaceRoleUse)
			}
			eachPatternPlace(property.Value, visit)
		}
		if typed.Rest != nil {
			visit(*typed.Rest, PlaceRoleDefine)
		}
	case *ArrayPattern:
		for _, element := range typed.Elements {
			if element.Value == nil {
				continue
			}
			if element.Default != nil {
				visit(*element.Default, PlaceRoleUse)
			}
			eachPatternPlace(element.Value, visit)
		}
		if typed.Rest != nil {
			visit(*typed.Rest, PlaceRoleDefine)
		}
	}
}

// EachTerminalPlace calls visit for every place a terminal reads.
//
// A terminal never defines a place, so every role here is a use.
func EachTerminalPlace(terminal Terminal, visit func(place Place, role PlaceRole)) {
	switch typed := terminal.(type) {
	case *Return:
		visit(typed.Value, PlaceRoleUse)
	case *Throw:
		visit(typed.Value, PlaceRoleUse)
	case *If:
		visit(typed.Test, PlaceRoleUse)
	case *Branch:
		visit(typed.Test, PlaceRoleUse)
	case *Switch:
		visit(typed.Test, PlaceRoleUse)
		for _, kase := range typed.Cases {
			if kase.Test != nil {
				visit(*kase.Test, PlaceRoleUse)
			}
		}
	case *Try:
		if typed.HandlerBinding != nil {
			visit(*typed.HandlerBinding, PlaceRoleDefine)
		}
	}
}

// EachSuccessor calls visit for every block control can reach directly from this terminal.
//
// Fallthrough is not yielded. It is not an edge: control reaches it through the construct's arms,
// not from the terminal. `EachSuccessorAndFallthrough` is the version for a caller that wants the
// structural link as well, such as a printer or a pass rebuilding syntax.
//
// The distinction is load-bearing. A dataflow analysis given the fallthrough as an edge propagates
// values along a path that does not exist, and the error is silent because the fallthrough is
// genuinely reachable by other means.
func EachSuccessor(terminal Terminal, visit func(block static_single_assignment.BlockId)) {
	visitReal := func(block static_single_assignment.BlockId) {
		if HasBlock(block) {
			visit(block)
		}
	}
	switch typed := terminal.(type) {
	case *Return, *Throw, *Unreachable, *Unsupported:
		// Control leaves the function.
	case *Goto:
		visitReal(typed.Block)
	case *If:
		visitReal(typed.Consequent)
		visitReal(typed.Alternate)
	case *Branch:
		visitReal(typed.Consequent)
		visitReal(typed.Alternate)
	case *Switch:
		for _, kase := range typed.Cases {
			visitReal(kase.Block)
		}
		// A switch with no default can fall past every case, so the fallthrough is an edge here.
		if !hasDefaultCase(typed.Cases) {
			visitReal(typed.Fallthrough)
		}
	case *While:
		visitReal(typed.Test)
	case *DoWhile:
		visitReal(typed.Loop)
	case *For:
		visitReal(typed.Init)
	case *ForOf:
		visitReal(typed.Init)
	case *ForIn:
		visitReal(typed.Init)
	case *Logical:
		visitReal(typed.Test)
	case *Ternary:
		visitReal(typed.Test)
	case *Optional:
		visitReal(typed.Test)
	case *Sequence:
		visitReal(typed.Block)
	case *Label:
		visitReal(typed.Block)
	case *Try:
		visitReal(typed.Block)
		visitReal(typed.Handler)
	case *MaybeThrow:
		visitReal(typed.Continuation)
		visitReal(typed.Handler)
	case *Scope:
		// The scope body is a real edge. The fallthrough is not, exactly as for every other
		// structured terminal: control reaches it by leaving the body, not from here.
		visitReal(typed.Block)
	}
}

func hasDefaultCase(cases []SwitchCase) bool {
	for _, kase := range cases {
		if kase.Test == nil {
			return true
		}
	}
	return false
}

// Fallthrough returns the block where the construct this terminal begins completes, if it has one.
func Fallthrough(terminal Terminal) (static_single_assignment.BlockId, bool) {
	var block static_single_assignment.BlockId
	switch typed := terminal.(type) {
	case *If:
		block = typed.Fallthrough
	case *Branch:
		block = typed.Fallthrough
	case *Switch:
		block = typed.Fallthrough
	case *While:
		block = typed.Fallthrough
	case *DoWhile:
		block = typed.Fallthrough
	case *For:
		block = typed.Fallthrough
	case *ForOf:
		block = typed.Fallthrough
	case *ForIn:
		block = typed.Fallthrough
	case *Logical:
		block = typed.Fallthrough
	case *Ternary:
		block = typed.Fallthrough
	case *Optional:
		block = typed.Fallthrough
	case *Sequence:
		block = typed.Fallthrough
	case *Label:
		block = typed.Fallthrough
	case *Try:
		block = typed.Fallthrough
	case *Scope:
		block = typed.Fallthrough
	default:
		return InvalidBlock, false
	}
	return block, HasBlock(block)
}

// EachSuccessorAndFallthrough calls visit for every successor and then the fallthrough.
//
// For a printer, a structural walk, or a reachability check that wants to reach every block a
// construct owns. Not for dataflow; see EachSuccessor.
func EachSuccessorAndFallthrough(terminal Terminal, visit func(block static_single_assignment.BlockId)) {
	seen := false
	fallthroughBlock, hasFallthrough := Fallthrough(terminal)
	EachSuccessor(terminal, func(block static_single_assignment.BlockId) {
		if hasFallthrough && block == fallthroughBlock {
			seen = true
		}
		visit(block)
	})
	if hasFallthrough && !seen {
		visit(fallthroughBlock)
	}
}

// TerminalOrder returns the terminal's position in evaluation order.
func TerminalOrder(terminal Terminal) static_single_assignment.EvaluationOrder {
	switch typed := terminal.(type) {
	case *Return:
		return typed.Order
	case *Throw:
		return typed.Order
	case *Unreachable:
		return typed.Order
	case *Unsupported:
		return typed.Order
	case *Goto:
		return typed.Order
	case *If:
		return typed.Order
	case *Branch:
		return typed.Order
	case *Switch:
		return typed.Order
	case *While:
		return typed.Order
	case *DoWhile:
		return typed.Order
	case *For:
		return typed.Order
	case *ForOf:
		return typed.Order
	case *ForIn:
		return typed.Order
	case *Logical:
		return typed.Order
	case *Ternary:
		return typed.Order
	case *Optional:
		return typed.Order
	case *Sequence:
		return typed.Order
	case *Label:
		return typed.Order
	case *Try:
		return typed.Order
	case *MaybeThrow:
		return typed.Order
	case *Scope:
		return typed.Order
	}
	return 0
}
