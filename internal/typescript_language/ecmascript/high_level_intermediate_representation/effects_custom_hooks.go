package high_level_intermediate_representation

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/mutation_aliasing"
	"github.com/system-inc/cohere/static_single_assignment"
)

func customHookSignature(function *Function) effectSignature {
	if function != nil && function.Node != nil {
		if sourceFile := ast.GetSourceFileOfNode(function.Node); sourceFile != nil {
			firstLine, _, _ := strings.Cut(sourceFile.Text(), "\n")
			if strings.Contains(firstLine, "@enableAssumeHooksFollowRulesOfReact:false") {
				return effectSignature{
					Receiver: EffectRead,
					Rest:     EffectConditionallyMutate,
					HasRest:  true,
					Result:   mutation_aliasing.EffectValueMutable,
				}
			}
		}
	}
	return effectSignature{
		Receiver: EffectRead,
		Rest:     EffectFreeze,
		HasRest:  true,
		Result:   mutation_aliasing.EffectValueFrozen,
	}
}

func newCalleeProducers(function *Function) *calleeProducers {
	return &calleeProducers{function: function}
}

func isModuleHookCall(function *Function, producers *calleeProducers, instruction *Instruction) bool {
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
	seen := map[static_single_assignment.IdentifierId]bool{}
	var resolve func(static_single_assignment.IdentifierId, bool) bool
	resolve = func(identifier static_single_assignment.IdentifierId, requireHook bool) bool {
		if seen[identifier] {
			return false
		}
		seen[identifier] = true
		defer delete(seen, identifier)
		switch definition := producers.producer(identifier).(type) {
		case *LoadGlobal:
			if definition.BindingKind == GlobalBindingKindGlobal || definition.Source == "react" {
				return false
			}
			return !requireHook || isHookName(definition.Name) || isHookName(definition.Imported)
		case *LoadLocal:
			return resolve(definition.Place.Identifier, requireHook)
		case *StoreLocal:
			return resolve(definition.Value.Identifier, requireHook)
		case *PropertyLoad:
			return (!requireHook || isHookName(string(definition.Property))) &&
				resolve(definition.Object.Identifier, false)
		case *ComputedLoad:
			property, ok := producers.producer(definition.Property.Identifier).(*Primitive)
			if !ok {
				return false
			}
			// A numeric or bigint key names a property as much as a string does, so
			// `modules[0].useThing()` reaches its module through the first entry. Only a string can
			// pass the hook test, but an object on the way to the callee need not.
			var name string
			switch key := property.Value.(type) {
			case PrimitiveString:
				name = string(key)
			case PrimitiveNumber:
				name = string(key)
			case PrimitiveBigInt:
				name = string(key)
			default:
				return false
			}
			return (!requireHook || isHookName(name)) && resolve(definition.Object.Identifier, false)
		default:
			return false
		}
	}
	if call, ok := instruction.Value.(*MethodCall); ok {
		if property, ok := producers.producer(call.Property.Identifier).(*Primitive); ok {
			name, ok := property.Value.(PrimitiveString)
			return ok && isHookName(string(name)) && resolve(call.Receiver.Identifier, false)
		}
	}
	return resolve(callee.Identifier, true)
}
