package high_level_intermediate_representation

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
)

func customHookSignature(function *Function) effectSignature {
	if function != nil && function.Node != nil {
		if sourceFile := ast.GetSourceFileOfNode(function.Node); sourceFile != nil {
			firstLine, _, _ := strings.Cut(sourceFile.Text(), "\n")
			if strings.Contains(firstLine, "@enableAssumeHooksFollowRulesOfReact:false") {
				return effectSignature{Receiver: EffectRead, Rest: EffectConditionallyMutate, HasRest: true, Result: EffectValueMutable}
			}
		}
	}
	return effectSignature{Receiver: EffectRead, Rest: EffectFreeze, HasRest: true, Result: EffectValueFrozen}
}

func isModuleHookCall(function *Function, instruction *Instruction) bool {
	if function == nil || instruction == nil {
		return false
	}
	var callee Place
	switch call := instruction.Value.(type) {
	case *CallExpression:
		callee = call.Callee
	case *MethodCall:
		callee = call.Property
	default:
		return false
	}
	producers := map[IdentifierId]InstructionValue{}
	for _, candidate := range function.Instructions {
		if candidate == nil {
			continue
		}
		producers[candidate.LValue.Identifier] = candidate.Value
		if store, ok := candidate.Value.(*StoreLocal); ok {
			producers[store.LValue.Identifier] = store
		}
	}
	seen := map[IdentifierId]bool{}
	var resolve func(IdentifierId, bool) bool
	resolve = func(identifier IdentifierId, requireHook bool) bool {
		if seen[identifier] {
			return false
		}
		seen[identifier] = true
		defer delete(seen, identifier)
		switch value := producers[identifier].(type) {
		case *LoadGlobal:
			if value.BindingKind == GlobalBindingKindGlobal || value.Source == "react" {
				return false
			}
			return !requireHook || isHookName(value.Name) || isHookName(value.Imported)
		case *LoadLocal:
			return resolve(value.Place.Identifier, requireHook)
		case *StoreLocal:
			return resolve(value.Value.Identifier, requireHook)
		case *PropertyLoad:
			return (!requireHook || isHookName(string(value.Property))) && resolve(value.Object.Identifier, false)
		case *ComputedLoad:
			property, ok := producers[value.Property.Identifier].(*Primitive)
			if !ok {
				return false
			}
			name, ok := property.Value.(string)
			return ok && (!requireHook || isHookName(name)) && resolve(value.Object.Identifier, false)
		default:
			return false
		}
	}
	if call, ok := instruction.Value.(*MethodCall); ok {
		if property, ok := producers[call.Property.Identifier].(*Primitive); ok {
			name, ok := property.Value.(string)
			return ok && isHookName(name) && resolve(call.Receiver.Identifier, false)
		}
	}
	return resolve(callee.Identifier, true)
}
