package high_level_intermediate_representation

import "github.com/system-inc/cohere/static_single_assignment"

type primitiveConstraintKey struct {
	function   *Function
	identifier static_single_assignment.IdentifierId
}

type primitiveConstraintKind uint8

const (
	primitiveConstraintUnknown primitiveConstraintKind = iota
	primitiveConstraintPrimitive
	primitiveConstraintOther
)

type primitiveConstraintState struct {
	parents  map[primitiveConstraintKey]primitiveConstraintKey
	kinds    map[primitiveConstraintKey]primitiveConstraintKind
	constant map[primitiveConstraintKey]bool
	visited  map[*Function]bool
}

func (state *primitiveConstraintState) root(key primitiveConstraintKey) primitiveConstraintKey {
	if parent, exists := state.parents[key]; exists {
		parent = state.root(parent)
		state.parents[key] = parent
		return parent
	}
	return key
}

func (state *primitiveConstraintState) bind(key primitiveConstraintKey, kind primitiveConstraintKind) {
	root := state.root(key)
	if state.kinds[root] == primitiveConstraintUnknown {
		state.kinds[root] = kind
	}
}

func (state *primitiveConstraintState) alias(left, right primitiveConstraintKey) {
	leftRoot, rightRoot := state.root(left), state.root(right)
	if leftRoot == rightRoot {
		return
	}
	leftKind, rightKind := state.kinds[leftRoot], state.kinds[rightRoot]
	if leftKind != primitiveConstraintUnknown && rightKind != primitiveConstraintUnknown && leftKind != rightKind {
		return
	}
	state.parents[leftRoot] = rightRoot
	if rightKind == primitiveConstraintUnknown {
		state.kinds[rightRoot] = leftKind
	}
}

func (state *primitiveConstraintState) visit(function *Function) {
	if function == nil || state.visited[function] {
		return
	}
	state.visited[function] = true
	key := func(place Place) primitiveConstraintKey { return primitiveConstraintKey{function, place.Identifier} }
	if function.Kind == FunctionKindComponent {
		for _, parameter := range function.Parameters {
			state.bind(key(parameter), primitiveConstraintOther)
		}
	}
	for _, block := range function.Blocks {
		if block == nil {
			continue
		}
		for _, phi := range block.Phis {
			state.bind(key(phi.Place), primitiveConstraintOther)
		}
		for _, instructionId := range block.Instructions {
			instruction := function.Instructions[instructionId]
			if instruction == nil {
				continue
			}
			left := key(instruction.LValue)
			switch shape := instruction.Value.(type) {
			case *Primitive, *TemplateLiteral, *UnaryExpression:
				state.bind(left, primitiveConstraintPrimitive)
			case *LoadLocal:
				state.alias(left, key(shape.Place))
			case *StoreLocal:
				state.alias(left, key(shape.Value))
				state.alias(key(shape.LValue), key(shape.Value))
				if shape.Kind == InstructionKindConst {
					state.constant[key(shape.LValue)] = true
				}
			case *LoadContext:
				if state.constant[key(shape.Place)] {
					state.alias(left, key(shape.Place))
				}
			case *StoreContext:
				if shape.Kind == InstructionKindConst {
					state.alias(key(shape.LValue), key(shape.Value))
					state.constant[key(shape.LValue)] = true
				}
			case *BinaryExpression:
				switch shape.Operator {
				case "+", "-", "/", "%", "*", "**", "&", "|", ">>", "<<", "^", ">", "<", ">=", "<=", "|>":
					state.bind(key(shape.Left), primitiveConstraintPrimitive)
					state.bind(key(shape.Right), primitiveConstraintPrimitive)
				}
				state.bind(left, primitiveConstraintPrimitive)
			case *PrefixUpdate:
				state.bind(key(shape.Value), primitiveConstraintPrimitive)
				state.bind(key(shape.LValue), primitiveConstraintPrimitive)
				state.bind(left, primitiveConstraintPrimitive)
			case *PostfixUpdate:
				state.bind(key(shape.Value), primitiveConstraintPrimitive)
				state.bind(key(shape.LValue), primitiveConstraintPrimitive)
				state.bind(left, primitiveConstraintPrimitive)
			case *LoadGlobal, *ObjectExpression, *ArrayExpression, *RegExpLiteral, *ObjectMethod, *NewExpression:
				state.bind(left, primitiveConstraintOther)
			case *PropertyLoad:
				if shape.Property == "current" ||
					state.kinds[state.root(key(shape.Object))] != primitiveConstraintUnknown {
					state.bind(left, primitiveConstraintOther)
				}
			case *ComputedLoad:
				state.bind(left, primitiveConstraintOther)
			case *CallExpression:
				state.bind(key(shape.Callee), primitiveConstraintOther)
				if shape.CalleeOrigin.Module == "react" && (shape.CalleeOrigin.Export == "useRef" ||
					shape.CalleeOrigin.Export == "useState" ||
					shape.CalleeOrigin.Export == "useReducer" ||
					shape.CalleeOrigin.Export == "useCallback") {
					state.bind(left, primitiveConstraintOther)
				}
			case *MethodCall:
				if shape.CalleeOrigin.Module == "react" && (shape.CalleeOrigin.Export == "useRef" ||
					shape.CalleeOrigin.Export == "useState" ||
					shape.CalleeOrigin.Export == "useReducer" ||
					shape.CalleeOrigin.Export == "useCallback") {
					state.bind(left, primitiveConstraintOther)
				}
			case *FunctionExpression:
				state.bind(left, primitiveConstraintOther)
				if int(shape.Function) >= len(function.Functions) {
					continue
				}
				nested := function.Functions[shape.Function]
				if nested != nil && len(nested.Context) == len(shape.Captures) {
					for index, capture := range shape.Captures {
						if state.constant[key(capture)] {
							context := primitiveConstraintKey{nested, nested.Context[index].Identifier}
							state.constant[context] = true
							state.alias(context, key(capture))
						}
					}
				}
				state.visit(nested)
			}
		}
	}
}

func inferPrimitivePropertyReads(function *Function) map[static_single_assignment.IdentifierId]bool {
	state := primitiveConstraintState{
		parents:  map[primitiveConstraintKey]primitiveConstraintKey{},
		kinds:    map[primitiveConstraintKey]primitiveConstraintKind{},
		constant: map[primitiveConstraintKey]bool{},
		visited:  map[*Function]bool{},
	}
	state.visit(function)
	result := map[static_single_assignment.IdentifierId]bool{}
	if function != nil {
		for _, instruction := range function.Instructions {
			if instruction == nil {
				continue
			}
			if _, property := instruction.Value.(*PropertyLoad); property && state.kinds[state.root(
				primitiveConstraintKey{function, instruction.LValue.Identifier},
			)] == primitiveConstraintPrimitive {
				result[instruction.LValue.Identifier] = true
			}
		}
	}
	return result
}
