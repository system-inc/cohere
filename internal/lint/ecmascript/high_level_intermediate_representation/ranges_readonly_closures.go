package high_level_intermediate_representation

func hasReadOnlyClosureEffects(function *Function, seen map[*Function]bool) bool {
	if function == nil || seen[function] {
		return false
	}
	seen[function] = true
	defer delete(seen, function)
	effects := InferAliasingEffects(function)
	for _, instruction := range function.Instructions {
		if instruction == nil {
			continue
		}
		switch value := instruction.Value.(type) {
		case *Await, *StoreGlobal, *StoreContext, *ObjectMethod:
			return false
		case *FunctionExpression:
			if int(value.Function) >= len(function.Functions) || !hasReadOnlyClosureEffects(function.Functions[value.Function], seen) {
				return false
			}
		case *LoadGlobal:
			if nested := nestedFunctionHeldBy(function, instruction.LValue.Identifier); nested != nil && !hasReadOnlyClosureEffects(nested, seen) {
				return false
			}
		}
		for _, effect := range effects.Get(instruction.Id) {
			if effect.Kind.IsMutation() || effect.Kind == AliasingEffectFreeze {
				return false
			}
		}
	}
	return true
}

func refDerivedValues(function *Function) map[IdentifierId]bool {
	refs := useRefResultValues(function)
	for {
		changed := false
		mark := func(from, into IdentifierId) {
			if refs[from] && !refs[into] {
				refs[into] = true
				changed = true
			}
		}
		for _, block := range function.Blocks {
			if block == nil {
				continue
			}
			for _, phi := range block.Phis {
				for _, operand := range phi.Operands {
					mark(operand.Identifier, phi.Place.Identifier)
				}
			}
			for _, instructionId := range block.Instructions {
				instruction := function.Instructions[instructionId]
				if instruction == nil {
					continue
				}
				switch value := instruction.Value.(type) {
				case *LoadLocal:
					mark(value.Place.Identifier, instruction.LValue.Identifier)
				case *StoreLocal:
					mark(value.Value.Identifier, value.LValue.Identifier)
					mark(value.Value.Identifier, instruction.LValue.Identifier)
				case *PropertyLoad:
					mark(value.Object.Identifier, instruction.LValue.Identifier)
				case *ComputedLoad:
					mark(value.Object.Identifier, instruction.LValue.Identifier)
				}
			}
		}
		if !changed {
			return refs
		}
	}
}
