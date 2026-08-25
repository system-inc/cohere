// Copying a nested function's body into its parent's id space.
//
// This is the half of `inlineImmediatelyInvokedFunctionExpressions` that upstream does not have to
// write. React mints block ids, identifier ids and instruction ids from one environment counter, so
// its inliner drops a nested function's blocks straight into the parent's map:
//
//	for (const [id, block] of body.loweredFunc.func.body.blocks) {
//	  block.preds.clear();
//	  fn.body.blocks.set(id, block);
//	}
//
// This tree cannot. Ids here are per-function and overlap between a function and its children, which
// `effects.go:573-577` records as a deliberate property: "InstructionIds are per-function and overlap
// between a function and its children ... the two spaces are kept apart here by construction." A
// nested body's tables all start at zero, so the same move would alias the parent's own values.
//
// So the copy renames as it goes, and this file is only the rename. The control-flow surgery that
// consumes it is upstream's and transcribes directly once ids are safe.
package hir

import "reflect"

// InlineRemap is the correspondence between a nested function's ids and the parent's.
//
// Returned rather than applied, so a caller can rewrite the terminals it owns -- the call site's
// continuation, the goto that replaces a return -- against the same mapping the body was copied
// under. A caller that guessed at these would produce a graph that builds and points at the wrong
// values.
type InlineRemap struct {
	// Identifiers maps a nested identifier id to the parent's.
	Identifiers map[IdentifierId]IdentifierId
	// Blocks maps a nested block id to the parent's.
	Blocks map[BlockId]BlockId
	// Instructions maps a nested instruction id to the parent's.
	Instructions map[InstructionId]InstructionId
	// Entry is the parent-space id of the nested function's entry block.
	Entry BlockId
}

// CopyNestedBodyInto copies a nested function's blocks, instructions and identifiers into parent.
//
// The nested function is left untouched: every structure is copied rather than moved, because
// `Function.Functions` still holds it and a later pass reading it must not see a half-renamed body.
// The cost is one allocation per instruction and per block, bounded by the body being inlined.
//
// # What is renamed, and the enumeration is the point
//
// Four id spaces cross the boundary and missing any one produces a graph that builds:
//
//   - `IdentifierId`, on every `Place` in every instruction value, lvalue, phi and terminal.
//   - `BlockId`, on every terminal reference and every phi operand key.
//   - `InstructionId`, on every block's instruction list.
//   - `FunctionId`, on a `FunctionExpression` or `ObjectMethod` nested inside the body being copied.
//
// The fourth is the one a reader does not expect. A closure inside the inlined closure indexes
// `Function.Functions` of the nested function, and that slice is not the parent's.
//
// # Captures become the mapping's seed rather than new values
//
// A nested function's `Context` names the values it closed over, and `FunctionExpression.Captures`
// names the same values in the parent. Index i of the two is one binding seen from both sides, so
// the mapping starts by pairing them: a capture must resolve to the parent's existing value, not to
// a fresh copy of it, or the inlined body reads a different variable than the code around it.
func CopyNestedBodyInto(parent *Function, nested *Function, captures []Place) (*InlineRemap, bool) {
	if parent == nil || nested == nil {
		return nil, false
	}
	if len(nested.Context) != len(captures) {
		// Upstream pairs these by index and asserts the lengths match. Declining is the honest
		// answer here: a mismatch means the capture list and the context disagree about what was
		// closed over, and inlining under that disagreement rewires reads silently.
		return nil, false
	}

	remap := &InlineRemap{
		Identifiers:  map[IdentifierId]IdentifierId{},
		Blocks:       map[BlockId]BlockId{},
		Instructions: map[InstructionId]InstructionId{},
	}

	// Seed: a captured value keeps the parent's identifier.
	for index, contextValue := range nested.Context {
		remap.Identifiers[contextValue.Identifier] = captures[index].Identifier
	}

	// Identifiers. A declaration id is carried across unchanged when the value is a capture, and
	// minted fresh otherwise, because a value local to the inlined body is a new binding in the
	// parent and must not group with anything already there.
	for id, identifier := range nested.Identifiers {
		if identifier == nil {
			continue
		}
		if _, seeded := remap.Identifiers[IdentifierId(id)]; seeded {
			continue
		}
		copied := parent.NewIdentifier(identifier.Name, identifier.Node, 0)
		remap.Identifiers[IdentifierId(id)] = copied.Id
	}

	// Blocks, allocated before instructions so a terminal can name a block copied later.
	for _, block := range nested.Blocks {
		if block == nil {
			continue
		}
		remap.Blocks[block.Id] = parent.NewBlock(block.Kind).Id
	}
	remap.Entry = remap.Blocks[nested.Entry]

	// Nested functions of the nested function, appended to the parent's table.
	functionRemap := map[FunctionId]FunctionId{}
	for id, inner := range nested.Functions {
		if inner == nil {
			continue
		}
		parent.Functions = append(parent.Functions, inner)
		functionRemap[FunctionId(id)] = FunctionId(len(parent.Functions) - 1)
	}

	for _, block := range nested.Blocks {
		if block == nil {
			continue
		}
		target, found := parent.Block(remap.Blocks[block.Id])
		if !found || target == nil {
			return nil, false
		}
		for _, instructionId := range block.Instructions {
			source := nested.Instructions[instructionId]
			if source == nil {
				continue
			}
			copied := &Instruction{
				Order:  source.Order,
				LValue: source.LValue,
				Value:  source.Value,
				Node:   source.Node,
			}
			copied.LValue.Identifier = remap.Identifiers[source.LValue.Identifier]
			EachInstructionPlacePointer(copied, func(place *Place, role PlaceRole) {
				if mapped, ok := remap.Identifiers[place.Identifier]; ok {
					place.Identifier = mapped
				}
			})
			remapNestedFunctionIds(copied.Value, functionRemap)
			remap.Instructions[instructionId] = parent.AddInstruction(target, copied)
		}

		target.Terminal = shallowCopyTerminal(block.Terminal)
		EachTerminalPlacePointer(target.Terminal, func(place *Place, role PlaceRole) {
			if mapped, ok := remap.Identifiers[place.Identifier]; ok {
				place.Identifier = mapped
			}
		})
		EachBlockReferencePointer(target.Terminal, func(reference *BlockId) {
			if mapped, ok := remap.Blocks[*reference]; ok {
				*reference = mapped
			}
		})

		for _, phi := range block.Phis {
			operands := map[BlockId]Place{}
			for predecessor, operand := range phi.Operands {
				mappedBlock, blockOk := remap.Blocks[predecessor]
				if !blockOk {
					mappedBlock = predecessor
				}
				if mapped, ok := remap.Identifiers[operand.Identifier]; ok {
					operand.Identifier = mapped
				}
				operands[mappedBlock] = operand
			}
			place := phi.Place
			if mapped, ok := remap.Identifiers[place.Identifier]; ok {
				place.Identifier = mapped
			}
			target.Phis = append(target.Phis, &Phi{Place: place, Operands: operands})
		}
	}

	return remap, true
}

// remapNestedFunctionIds repoints a value that names a function in the nested table.
func remapNestedFunctionIds(value InstructionValue, functionRemap map[FunctionId]FunctionId) {
	switch shape := value.(type) {
	case *FunctionExpression:
		if mapped, ok := functionRemap[shape.Function]; ok {
			shape.Function = mapped
		}
	case *ObjectMethod:
		if mapped, ok := functionRemap[shape.Function]; ok {
			shape.Function = mapped
		}
	}
}

// shallowCopyTerminal returns a terminal holding the same field values as its argument, in fresh
// storage.
//
// The copy exists because remapping rewrites through pointers. `EachTerminalPlacePointer` and
// `EachBlockReferencePointer` both hand out pointers into the terminal they are given, and the
// caller writes through them; handing them the nested function's own terminal would rename the
// nested body rather than the copy. Copying is shallow on purpose: the fields a terminal holds are
// places, block ids and evaluation orders, all values, so one level is the whole structure.
//
// It is written by reflection rather than as one arm per terminal because the arms would say
// nothing. A hand-written `case *If: return &If{...}` restates the struct definition, and the
// restatement is what rots: a field added to a terminal is silently dropped by a copier that
// predates it, and nothing fails until a rule reads the field through an inlined body. Reflection
// copies whatever is there.
func shallowCopyTerminal(terminal Terminal) Terminal {
	if terminal == nil {
		return nil
	}
	value := reflect.ValueOf(terminal)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return terminal
	}
	copied := reflect.New(value.Elem().Type())
	copied.Elem().Set(value.Elem())
	clone, ok := copied.Interface().(Terminal)
	if !ok {
		return terminal
	}
	return clone
}
