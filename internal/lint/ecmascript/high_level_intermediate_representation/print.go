// Printing a function back out as text.
//
// This exists to make the IR reviewable by a human and testable by a diff. Every lowering test in
// this package asserts against printed output rather than against a structure literal, because a
// structure literal test asserts what the author believed lowering does and a printed one shows
// what it did. The difference matters most exactly where lowering is subtle, which is where the
// tests are needed.
//
// The format is this package's own, not upstream's. Matching upstream's printer would be a second
// fidelity obligation with no oracle behind it: no fixture compares printed `HIR`, so the only reader
// is us.
package high_level_intermediate_representation

import (
	"fmt"
	"sort"
	"strings"
)

// Print renders a function as text.
func Print(function *Function) string {
	var out strings.Builder
	printFunction(&out, function, 0)
	return out.String()
}

func printFunction(out *strings.Builder, function *Function, depth int) {
	indent := strings.Repeat("  ", depth)

	name := function.Name
	if name == "" {
		name = "<anonymous>"
	}
	fmt.Fprintf(out, "%sfunction %s(", indent, name)
	for index, parameter := range function.Params {
		if index > 0 {
			out.WriteString(", ")
		}
		out.WriteString(function.PlaceString(parameter))
	}
	out.WriteString(")")
	if function.Kind != FunctionKindOther {
		fmt.Fprintf(out, " [%s]", function.Kind)
	}
	out.WriteString("\n")

	for _, block := range function.Blocks {
		fmt.Fprintf(out, "%sbb%d (%s)", indent, block.Id, block.Kind)
		if len(block.Predecessors) > 0 {
			predecessors := make([]string, 0, len(block.Predecessors))
			for _, predecessor := range block.Predecessors {
				predecessors = append(predecessors, fmt.Sprintf("bb%d", predecessor))
			}
			sort.Strings(predecessors)
			fmt.Fprintf(out, " preds=[%s]", strings.Join(predecessors, " "))
		}
		out.WriteString(":\n")

		// Operands are read through PhiOperandsInOrder, ascending predecessor order, so the same
		// function renders the same way between runs, which TestLowerIsDeterministic exists to
		// catch.
		for _, phi := range block.Phis {
			operands := make([]string, 0, len(phi.Operands))
			for _, predecessorId := range PhiOperandsInOrder(phi) {
				operands = append(operands, fmt.Sprintf("bb%d: %s",
					predecessorId, function.PlaceString(phi.Operands.At(predecessorId))))
			}
			fmt.Fprintf(out, "%s  %s = phi(%s)\n",
				indent, function.PlaceString(phi.Place), strings.Join(operands, ", "))
		}
		for _, instructionId := range block.Instructions {
			instruction := function.Instructions[instructionId]
			fmt.Fprintf(
				out,
				"%s  %s = %s\n",
				indent,
				function.PlaceString(instruction.LValue),
				printValue(function, instruction.Value),
			)
		}
		fmt.Fprintf(out, "%s  %s\n", indent, printTerminal(function, block.Terminal))
	}

	for _, nested := range function.Functions {
		out.WriteString("\n")
		printFunction(out, nested, depth+1)
	}
}

func printValue(function *Function, value InstructionValue) string {
	place := function.PlaceString
	switch typed := value.(type) {
	case *LoadLocal:
		return fmt.Sprintf("LoadLocal %s", place(typed.Place))
	case *LoadContext:
		return fmt.Sprintf("LoadContext %s", place(typed.Place))
	case *DeclareLocal:
		return fmt.Sprintf("DeclareLocal %s %s", typed.Kind, place(typed.LValue))
	case *DeclareContext:
		return fmt.Sprintf("DeclareContext %s %s", typed.Kind, place(typed.LValue))
	case *StoreLocal:
		return fmt.Sprintf("StoreLocal %s %s = %s", typed.Kind, place(typed.LValue), place(typed.Value))
	case *StoreContext:
		return fmt.Sprintf("StoreContext %s %s = %s", typed.Kind, place(typed.LValue), place(typed.Value))
	case *Destructure:
		return fmt.Sprintf("Destructure %s %s = %s", typed.Kind, printPattern(function, typed.LValue), place(typed.Value))
	case *LoadGlobal:
		if typed.Source != "" {
			return fmt.Sprintf("LoadGlobal %s from %q", typed.Name, typed.Source)
		}
		return fmt.Sprintf("LoadGlobal %s", typed.Name)
	case *StoreGlobal:
		return fmt.Sprintf("StoreGlobal %s = %s", typed.Name, place(typed.Value))
	case *PropertyLoad:
		return fmt.Sprintf("PropertyLoad %s%s.%s", place(typed.Object), optionalMark(typed.Optional), typed.Property)
	case *PropertyStore:
		return fmt.Sprintf("PropertyStore %s.%s = %s", place(typed.Object), typed.Property, place(typed.Value))
	case *PropertyDelete:
		return fmt.Sprintf("PropertyDelete %s.%s", place(typed.Object), typed.Property)
	case *ComputedLoad:
		return fmt.Sprintf("ComputedLoad %s%s[%s]", place(typed.Object), optionalMark(typed.Optional), place(typed.Property))
	case *ComputedStore:
		return fmt.Sprintf("ComputedStore %s[%s] = %s", place(typed.Object), place(typed.Property), place(typed.Value))
	case *ComputedDelete:
		return fmt.Sprintf("ComputedDelete %s[%s]", place(typed.Object), place(typed.Property))
	case *CallExpression:
		return fmt.Sprintf("Call %s%s(%s)", place(typed.Callee), optionalMark(typed.Optional), printArgs(function, typed.Args))
	case *MethodCall:
		return fmt.Sprintf("MethodCall %s.%s(%s)", place(typed.Receiver), place(typed.Property), printArgs(function, typed.Args))
	case *NewExpression:
		return fmt.Sprintf("New %s(%s)", place(typed.Callee), printArgs(function, typed.Args))
	case *BinaryExpression:
		return fmt.Sprintf("Binary %s %s %s", place(typed.Left), typed.Operator, place(typed.Right))
	case *UnaryExpression:
		return fmt.Sprintf("Unary %s%s", typed.Operator, place(typed.Value))
	case *PrefixUpdate:
		return fmt.Sprintf("PrefixUpdate %s%s", typed.Operation, place(typed.Value))
	case *PostfixUpdate:
		return fmt.Sprintf("PostfixUpdate %s%s", place(typed.Value), typed.Operation)
	case *Primitive:
		return fmt.Sprintf("Primitive %v", typed.Value)
	case *RegExpLiteral:
		return fmt.Sprintf("RegExp /%s/%s", typed.Pattern, typed.Flags)
	case *TemplateLiteral:
		return fmt.Sprintf("Template %d quasis %d exprs", len(typed.Quasis), len(typed.Subexprs))
	case *TaggedTemplateExpression:
		return fmt.Sprintf("TaggedTemplate %s", place(typed.Tag))
	case *TypeCastExpression:
		return fmt.Sprintf("TypeCast %s", place(typed.Value))
	case *MetaProperty:
		return fmt.Sprintf("MetaProperty %s.%s", typed.Meta, typed.Property)
	case *ObjectExpression:
		parts := make([]string, 0, len(typed.Properties))
		for _, property := range typed.Properties {
			switch {
			case property.Spread:
				parts = append(parts, "..."+place(property.Value))
			case property.ComputedKey != nil:
				parts = append(parts, fmt.Sprintf("[%s]: %s", place(*property.ComputedKey), place(property.Value)))
			default:
				parts = append(parts, fmt.Sprintf("%s: %s", property.Key, place(property.Value)))
			}
		}
		return fmt.Sprintf("Object {%s}", strings.Join(parts, ", "))
	case *ObjectMethod:
		return fmt.Sprintf("ObjectMethod %s fn%d", typed.Key, typed.Function)
	case *ArrayExpression:
		parts := make([]string, 0, len(typed.Elements))
		for _, element := range typed.Elements {
			switch {
			case element.Hole:
				parts = append(parts, "<hole>")
			case element.Spread:
				parts = append(parts, "..."+place(element.Place))
			default:
				parts = append(parts, place(element.Place))
			}
		}
		return fmt.Sprintf("Array [%s]", strings.Join(parts, ", "))
	case *FunctionExpression:
		captures := make([]string, 0, len(typed.Captures))
		for _, capture := range typed.Captures {
			captures = append(captures, place(capture))
		}
		if len(captures) > 0 {
			return fmt.Sprintf("Function fn%d captures=[%s]", typed.Function, strings.Join(captures, " "))
		}
		return fmt.Sprintf("Function fn%d", typed.Function)
	case *Await:
		return fmt.Sprintf("Await %s", place(typed.Value))
	case *GetIterator:
		return fmt.Sprintf("GetIterator %s", place(typed.Value))
	case *IteratorNext:
		return fmt.Sprintf("IteratorNext %s", place(typed.Iterator))
	case *NextPropertyOf:
		return fmt.Sprintf("NextPropertyOf %s", place(typed.Value))
	case *JsxExpression:
		tag := typed.Tag.Name
		if typed.Tag.Place != nil {
			tag = place(*typed.Tag.Place)
		}
		return fmt.Sprintf("Jsx <%s> props=%d children=%d", tag, len(typed.Props), len(typed.Children))
	case *JsxFragment:
		return fmt.Sprintf("JsxFragment children=%d", len(typed.Children))
	case *JsxText:
		return fmt.Sprintf("JsxText %q", typed.Value)
	case *StartMemoize:
		// `deps=none` and `deps=0` are different facts (no dependency array at all versus an empty
		// one) and the printer distinguishes them, because a golden that spelled both `deps=0`
		// would pass while the pass conflated them.
		if typed.Deps == nil {
			return fmt.Sprintf("StartMemoize#%d deps=none", typed.ManualMemoId)
		}
		printed := make([]string, 0, len(typed.Deps))
		for _, dep := range typed.Deps {
			printed = append(printed, printManualMemoDependency(dep, place))
		}
		return fmt.Sprintf("StartMemoize#%d deps=[%s]", typed.ManualMemoId, strings.Join(printed, ", "))
	case *FinishMemoize:
		if typed.Pruned {
			return fmt.Sprintf("FinishMemoize#%d %s pruned", typed.ManualMemoId, place(typed.Value))
		}
		return fmt.Sprintf("FinishMemoize#%d %s", typed.ManualMemoId, place(typed.Value))
	case *Debugger:
		return "Debugger"
	case *UnsupportedNode:
		return fmt.Sprintf("Unsupported %s", typed.Reason)
	}
	return "<unknown>"
}

func printPattern(function *Function, pattern Pattern) string {
	switch typed := pattern.(type) {
	case *PlacePattern:
		return function.PlaceString(typed.Place)
	case *ObjectPattern:
		parts := make([]string, 0, len(typed.Properties))
		for _, property := range typed.Properties {
			key := property.Key
			if property.ComputedKey != nil {
				key = "[" + function.PlaceString(*property.ComputedKey) + "]"
			}
			parts = append(parts, fmt.Sprintf("%s: %s", key, printPattern(function, property.Value)))
		}
		if typed.Rest != nil {
			parts = append(parts, "..."+function.PlaceString(*typed.Rest))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case *ArrayPattern:
		parts := make([]string, 0, len(typed.Elements))
		for _, element := range typed.Elements {
			if element.Value == nil {
				parts = append(parts, "<hole>")
				continue
			}
			parts = append(parts, printPattern(function, element.Value))
		}
		if typed.Rest != nil {
			parts = append(parts, "..."+function.PlaceString(*typed.Rest))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return "<pattern>"
}

func printArgs(function *Function, args []Argument) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		if arg.Spread {
			parts = append(parts, "..."+function.PlaceString(arg.Place))
			continue
		}
		parts = append(parts, function.PlaceString(arg.Place))
	}
	return strings.Join(parts, ", ")
}

func optionalMark(optional bool) string {
	if optional {
		return "?"
	}
	return ""
}

func printTerminal(function *Function, terminal Terminal) string {
	place := function.PlaceString
	switch typed := terminal.(type) {
	case *Return:
		return fmt.Sprintf("Return %s", place(typed.Value))
	case *Throw:
		return fmt.Sprintf("Throw %s", place(typed.Value))
	case *Unreachable:
		return "Unreachable"
	case *Unsupported:
		return "Unsupported"
	case *Goto:
		return fmt.Sprintf("Goto bb%d%s", typed.Block, gotoVariantMark(typed.Variant))
	case *If:
		return fmt.Sprintf("If %s then bb%d else bb%d fallthrough=bb%d",
			place(typed.Test), typed.Consequent, typed.Alternate, typed.Fallthrough)
	case *Branch:
		return fmt.Sprintf("Branch %s then bb%d else bb%d fallthrough=bb%d",
			place(typed.Test), typed.Consequent, typed.Alternate, typed.Fallthrough)
	case *Switch:
		parts := make([]string, 0, len(typed.Cases))
		for _, kase := range typed.Cases {
			if kase.Test == nil {
				parts = append(parts, fmt.Sprintf("default -> bb%d", kase.Block))
				continue
			}
			parts = append(parts, fmt.Sprintf("%s -> bb%d", place(*kase.Test), kase.Block))
		}
		return fmt.Sprintf("Switch %s [%s] fallthrough=bb%d", place(typed.Test), strings.Join(parts, ", "), typed.Fallthrough)
	case *While:
		return fmt.Sprintf("While test=bb%d loop=bb%d fallthrough=bb%d", typed.Test, typed.Loop, typed.Fallthrough)
	case *DoWhile:
		return fmt.Sprintf("DoWhile loop=bb%d test=bb%d fallthrough=bb%d", typed.Loop, typed.Test, typed.Fallthrough)
	case *For:
		update := "none"
		if HasBlock(typed.Update) {
			update = fmt.Sprintf("bb%d", typed.Update)
		}
		return fmt.Sprintf("For init=bb%d test=bb%d update=%s loop=bb%d fallthrough=bb%d",
			typed.Init, typed.Test, update, typed.Loop, typed.Fallthrough)
	case *ForOf:
		return fmt.Sprintf("ForOf init=bb%d test=bb%d loop=bb%d fallthrough=bb%d",
			typed.Init, typed.Test, typed.Loop, typed.Fallthrough)
	case *ForIn:
		return fmt.Sprintf("ForIn init=bb%d loop=bb%d fallthrough=bb%d", typed.Init, typed.Loop, typed.Fallthrough)
	case *Logical:
		return fmt.Sprintf("Logical %s test=bb%d fallthrough=bb%d", typed.Operator, typed.Test, typed.Fallthrough)
	case *Ternary:
		return fmt.Sprintf("Ternary test=bb%d fallthrough=bb%d", typed.Test, typed.Fallthrough)
	case *Optional:
		return fmt.Sprintf("Optional optional=%t test=bb%d fallthrough=bb%d", typed.Optional, typed.Test, typed.Fallthrough)
	case *Sequence:
		return fmt.Sprintf("Sequence block=bb%d fallthrough=bb%d", typed.Block, typed.Fallthrough)
	case *Label:
		return fmt.Sprintf("Label block=bb%d fallthrough=bb%d", typed.Block, typed.Fallthrough)
	case *Try:
		binding := "none"
		if typed.HandlerBinding != nil {
			binding = place(*typed.HandlerBinding)
		}
		return fmt.Sprintf("Try block=bb%d handler=bb%d binding=%s fallthrough=bb%d",
			typed.Block, typed.Handler, binding, typed.Fallthrough)
	case *MaybeThrow:
		handler := "none"
		if HasBlock(typed.Handler) {
			handler = fmt.Sprintf("bb%d", typed.Handler)
		}
		return fmt.Sprintf("MaybeThrow continuation=bb%d handler=%s", typed.Continuation, handler)
	case *Scope:
		return fmt.Sprintf("Scope @%d block=bb%d fallthrough=bb%d", typed.Scope, typed.Block, typed.Fallthrough)
	}
	return "<unknown terminal>"
}

func gotoVariantMark(variant GotoVariant) string {
	switch variant {
	case GotoVariantBreak:
		return " (break)"
	case GotoVariantContinue:
		return " (continue)"
	case GotoVariantTry:
		return " (try)"
	}
	return ""
}

// printManualMemoDependency renders one written dependency as its source path.
//
// The `?.` is printed for an optional entry because that character is the entire distinction the
// `preserve-manual-memoization` rule exists to report, and a printer that dropped it would make the
// two sides of that comparison look identical in every golden.
func printManualMemoDependency(dep ManualMemoDependency, place func(Place) string) string {
	var builder strings.Builder
	if dep.Root.IsGlobal {
		builder.WriteString(dep.Root.Name)
	} else {
		builder.WriteString(place(dep.Root.Place))
	}
	for _, entry := range dep.Path {
		if entry.Optional {
			builder.WriteString("?.")
		} else {
			builder.WriteString(".")
		}
		builder.WriteString(entry.Property)
	}
	return builder.String()
}
