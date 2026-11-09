package canonical_function_hash

import (
	"fmt"

	"github.com/system-inc/cohere/internal/duplicate_functions"
	"github.com/system-inc/cohere/internal/lint/ecmascript/high_level_intermediate_representation"
)

func (hasher *functionHasher) places(
	builder *duplicate_functions.DigestBuilder,
	places []high_level_intermediate_representation.Place,
) error {
	builder.Number(len(places))
	for _, place := range places {
		value, valueError := hasher.value(place)
		if valueError != nil {
			return valueError
		}
		builder.Digest(value)
	}
	return nil
}

func (hasher *functionHasher) arguments(
	builder *duplicate_functions.DigestBuilder,
	arguments []high_level_intermediate_representation.Argument,
) error {
	builder.Number(len(arguments))
	for _, argument := range arguments {
		value, valueError := hasher.value(argument.Place)
		if valueError != nil {
			return valueError
		}
		builder.Flag(argument.Spread).Digest(value)
	}
	return nil
}

// cellLoad is a read of a memory binding, which hashes with the effect chain it reads.
func (hasher *functionHasher) cellLoad(
	place high_level_intermediate_representation.Place,
	memory duplicate_functions.Digest,
) duplicate_functions.Digest {
	return duplicate_functions.NewDigestBuilder("cell-load").Digest(hasher.cell(place.Identifier)).Digest(memory).Sum()
}

// cellStore is a write of a memory binding, which joins the effect chain.
func (hasher *functionHasher) cellStore(
	place high_level_intermediate_representation.Place,
	value duplicate_functions.Digest,
	memory *duplicate_functions.Digest,
) {
	effect(
		duplicate_functions.NewDigestBuilder("cell-store").Digest(hasher.cell(place.Identifier)).Digest(value),
		memory,
	)
}

// instruction hashes one instruction into the values it defines and, for an effect, into memory, the block's chain.
func (hasher *functionHasher) instruction(
	instruction *high_level_intermediate_representation.Instruction,
	memory *duplicate_functions.Digest,
) error {
	switch instruction.Value.(type) {
	case *high_level_intermediate_representation.LoadLocal,
		*high_level_intermediate_representation.StoreLocal,
		*high_level_intermediate_representation.TypeCastExpression,
		*high_level_intermediate_representation.DeclareLocal,
		*high_level_intermediate_representation.StartMemoize,
		*high_level_intermediate_representation.FinishMemoize:
	default:
		hasher.size++
	}
	lvalue := instruction.LValue
	switch typed := instruction.Value.(type) {
	case *high_level_intermediate_representation.LoadLocal:
		return hasher.load(lvalue, typed.Place, *memory)
	case *high_level_intermediate_representation.LoadContext:
		return hasher.load(lvalue, typed.Place, *memory)
	case *high_level_intermediate_representation.DeclareLocal:
		undefined := primitive(high_level_intermediate_representation.PrimitiveUndefined{})
		if hasher.isCell(typed.LValue.Identifier) {
			hasher.cellStore(typed.LValue, undefined, memory)
		} else {
			hasher.set(typed.LValue, undefined)
		}
		hasher.set(lvalue, undefined)
		return nil
	case *high_level_intermediate_representation.DeclareContext:
		undefined := primitive(high_level_intermediate_representation.PrimitiveUndefined{})
		hasher.cellStore(typed.LValue, undefined, memory)
		hasher.set(lvalue, undefined)
		return nil
	case *high_level_intermediate_representation.StoreLocal:
		value, valueError := hasher.value(typed.Value)
		if valueError != nil {
			return valueError
		}
		hasher.storeUpdate(typed.LValue, value, memory)
		hasher.set(lvalue, value)
		return nil
	case *high_level_intermediate_representation.StoreContext:
		value, valueError := hasher.value(typed.Value)
		if valueError != nil {
			return valueError
		}
		hasher.cellStore(typed.LValue, value, memory)
		hasher.set(lvalue, value)
		return nil
	case *high_level_intermediate_representation.Destructure:
		source, sourceError := hasher.value(typed.Value)
		if sourceError != nil {
			return sourceError
		}
		if patternError := hasher.pattern(typed.Pattern, source, memory); patternError != nil {
			return patternError
		}
		hasher.set(lvalue, source)
		return nil
	case *high_level_intermediate_representation.LoadGlobal:
		hasher.set(lvalue, hasher.file.global(instruction.Node, typed.Name, typed.BindingKind, *memory,
			hasher.dependencies))
		return nil
	case *high_level_intermediate_representation.StoreGlobal:
		value, valueError := hasher.value(typed.Value)
		if valueError != nil {
			return valueError
		}
		identity, _, _ := hasher.file.globalIdentity(storeTarget(instruction.Node), typed.Name,
			high_level_intermediate_representation.GlobalBindingKindGlobal)
		effect(duplicate_functions.NewDigestBuilder("global-store").Text(identity).Digest(value), memory)
		hasher.set(lvalue, value)
		return nil
	case *high_level_intermediate_representation.PropertyLoad:
		object, objectError := hasher.value(typed.Object)
		hasher.set(lvalue, duplicate_functions.NewDigestBuilder("property-load").Digest(object).Text(
			typed.Property,
		).Flag(typed.Optional).
			Digest(*memory).Sum())
		return objectError
	case *high_level_intermediate_representation.PropertyStore:
		object, objectError := hasher.value(typed.Object)
		if objectError != nil {
			return objectError
		}
		value, valueError := hasher.value(typed.Value)
		if valueError != nil {
			return valueError
		}
		effect(
			duplicate_functions.NewDigestBuilder("property-store").Digest(object).Text(typed.Property).Digest(value),
			memory,
		)
		hasher.set(lvalue, value)
		return nil
	case *high_level_intermediate_representation.PropertyDelete:
		object, objectError := hasher.value(typed.Object)
		hasher.set(
			lvalue,
			effect(duplicate_functions.NewDigestBuilder("property-delete").Digest(object).Text(typed.Property), memory),
		)
		return objectError
	case *high_level_intermediate_representation.ComputedLoad:
		builder := duplicate_functions.NewDigestBuilder("computed-load").Flag(typed.Optional)
		placesError := hasher.places(
			builder,
			[]high_level_intermediate_representation.Place{typed.Object, typed.Property},
		)
		hasher.set(lvalue, builder.Digest(*memory).Sum())
		return placesError
	case *high_level_intermediate_representation.ComputedStore:
		builder := duplicate_functions.NewDigestBuilder("computed-store")
		if placesError := hasher.places(
			builder,
			[]high_level_intermediate_representation.Place{typed.Object, typed.Property, typed.Value},
		); placesError != nil {
			return placesError
		}
		effect(builder, memory)
		value, valueError := hasher.value(typed.Value)
		hasher.set(lvalue, value)
		return valueError
	case *high_level_intermediate_representation.ComputedDelete:
		builder := duplicate_functions.NewDigestBuilder("computed-delete")
		placesError := hasher.places(
			builder,
			[]high_level_intermediate_representation.Place{typed.Object, typed.Property},
		)
		hasher.set(lvalue, effect(builder, memory))
		return placesError
	case *high_level_intermediate_representation.CallExpression:
		callee, calleeError := hasher.value(typed.Callee)
		if calleeError != nil {
			return calleeError
		}
		builder := duplicate_functions.NewDigestBuilder("call").Digest(callee).Flag(typed.Optional)
		if argumentsError := hasher.arguments(builder, typed.Arguments); argumentsError != nil {
			return argumentsError
		}
		hasher.set(lvalue, effect(builder, memory))
		return nil
	case *high_level_intermediate_representation.MethodCall:
		builder := duplicate_functions.NewDigestBuilder("method-call").Flag(typed.Optional)
		if placesError := hasher.places(
			builder,
			[]high_level_intermediate_representation.Place{typed.Receiver, typed.Property},
		); placesError != nil {
			return placesError
		}
		if argumentsError := hasher.arguments(builder, typed.Arguments); argumentsError != nil {
			return argumentsError
		}
		hasher.set(lvalue, effect(builder, memory))
		return nil
	case *high_level_intermediate_representation.NewExpression:
		callee, calleeError := hasher.value(typed.Callee)
		if calleeError != nil {
			return calleeError
		}
		builder := duplicate_functions.NewDigestBuilder("new").Digest(callee)
		if argumentsError := hasher.arguments(builder, typed.Arguments); argumentsError != nil {
			return argumentsError
		}
		hasher.set(lvalue, effect(builder, memory))
		return nil
	case *high_level_intermediate_representation.BinaryExpression:
		left, leftError := hasher.value(typed.Left)
		if leftError != nil {
			return leftError
		}
		right, rightError := hasher.value(typed.Right)
		if rightError != nil {
			return rightError
		}
		if hasher.commutative(typed.Operator, instruction.Node) && right.Less(left) {
			left, right = right, left
		}
		hasher.set(
			lvalue,
			duplicate_functions.NewDigestBuilder("binary").Text(typed.Operator).Digest(left).Digest(right).Sum(),
		)
		return nil
	case *high_level_intermediate_representation.UnaryExpression:
		value, valueError := hasher.value(typed.Value)
		hasher.set(lvalue, duplicate_functions.NewDigestBuilder("unary").Text(typed.Operator).Digest(value).Sum())
		return valueError
	case *high_level_intermediate_representation.PrefixUpdate:
		old, oldError := hasher.value(typed.Value)
		if oldError != nil {
			return oldError
		}
		updated := duplicate_functions.NewDigestBuilder("update").Text(typed.Operation).Digest(old).Sum()
		hasher.storeUpdate(typed.LValue, updated, memory)
		hasher.set(lvalue, updated)
		return nil
	case *high_level_intermediate_representation.PostfixUpdate:
		old, oldError := hasher.value(typed.Value)
		if oldError != nil {
			return oldError
		}
		updated := duplicate_functions.NewDigestBuilder("update").Text(typed.Operation).Digest(old).Sum()
		hasher.storeUpdate(typed.LValue, updated, memory)
		hasher.set(lvalue, duplicate_functions.NewDigestBuilder("to-numeric").Digest(old).Sum())
		return nil
	case *high_level_intermediate_representation.Primitive:
		hasher.set(lvalue, primitive(typed.Value))
		return nil
	case *high_level_intermediate_representation.RegExpLiteral:
		hasher.set(
			lvalue,
			hasher.allocate(duplicate_functions.NewDigestBuilder("regexp").Text(typed.Pattern).Text(typed.Flags).Sum()),
		)
		return nil
	case *high_level_intermediate_representation.TemplateLiteral:
		builder := duplicate_functions.NewDigestBuilder("template").Number(len(typed.Quasis))
		for _, quasi := range typed.Quasis {
			builder.Text(quasi)
		}
		placesError := hasher.places(builder, typed.Subexprs)
		hasher.set(lvalue, builder.Sum())
		return placesError
	case *high_level_intermediate_representation.TaggedTemplateExpression:
		tag, tagError := hasher.value(typed.Tag)
		if tagError != nil {
			return tagError
		}
		builder := duplicate_functions.NewDigestBuilder("tagged-template").Digest(tag).Number(len(typed.Quasis))
		for _, quasi := range typed.Quasis {
			builder.Text(quasi)
		}
		if placesError := hasher.places(builder, typed.Subexprs); placesError != nil {
			return placesError
		}
		hasher.set(lvalue, effect(builder, memory))
		return nil
	case *high_level_intermediate_representation.TypeCastExpression:
		value, valueError := hasher.value(typed.Value)
		hasher.set(lvalue, value)
		return valueError
	case *high_level_intermediate_representation.MetaProperty:
		hasher.set(lvalue, duplicate_functions.NewDigestBuilder("meta").Text(typed.Meta).Text(typed.Property).Sum())
		return nil
	case *high_level_intermediate_representation.ObjectExpression:
		return hasher.object(lvalue, typed)
	case *high_level_intermediate_representation.ObjectMethod:
		nested, nestedError := hasher.nested(typed.Function)
		if nestedError != nil {
			return nestedError
		}
		// ObjectMethod records no captures, and a context identifier carries no node to recover the pairing from, so
		// what the method closes over is unknown here. Worse, the enclosing values it reads look dead and would drop
		// out of the hash.
		if len(hasher.function.Functions[typed.Function].Context) > 0 {
			return refuse(duplicate_functions.RefusalObjectMethodCapture, "")
		}
		hasher.set(
			lvalue,
			hasher.allocate(duplicate_functions.NewDigestBuilder("object-method").Text(typed.Key).Digest(nested).Sum()),
		)
		return nil
	case *high_level_intermediate_representation.ArrayExpression:
		builder := duplicate_functions.NewDigestBuilder("array").Number(len(typed.Elements))
		for _, element := range typed.Elements {
			if element.Hole {
				builder.Text("hole")
				continue
			}
			value, valueError := hasher.value(element.Place)
			if valueError != nil {
				return valueError
			}
			builder.Flag(element.Spread).Digest(value)
		}
		hasher.set(lvalue, hasher.allocate(builder.Sum()))
		return nil
	case *high_level_intermediate_representation.FunctionExpression:
		return hasher.closure(lvalue, typed)
	case *high_level_intermediate_representation.Await:
		value, valueError := hasher.value(typed.Value)
		if valueError != nil {
			return valueError
		}
		hasher.set(lvalue, effect(duplicate_functions.NewDigestBuilder("await").Digest(value), memory))
		return nil
	case *high_level_intermediate_representation.GetIterator:
		value, valueError := hasher.value(typed.Value)
		if valueError != nil {
			return valueError
		}
		hasher.set(lvalue, effect(duplicate_functions.NewDigestBuilder("get-iterator").Digest(value), memory))
		return nil
	case *high_level_intermediate_representation.IteratorNext:
		builder := duplicate_functions.NewDigestBuilder("iterator-next")
		if placesError := hasher.places(
			builder,
			[]high_level_intermediate_representation.Place{typed.Iterator, typed.Collection},
		); placesError != nil {
			return placesError
		}
		hasher.set(lvalue, effect(builder, memory))
		return nil
	case *high_level_intermediate_representation.NextPropertyOf:
		value, valueError := hasher.value(typed.Value)
		if valueError != nil {
			return valueError
		}
		hasher.set(lvalue, effect(duplicate_functions.NewDigestBuilder("next-property").Digest(value), memory))
		return nil
	case *high_level_intermediate_representation.JsxExpression:
		return hasher.jsx(lvalue, typed)
	case *high_level_intermediate_representation.JsxFragment:
		builder := duplicate_functions.NewDigestBuilder("jsx-fragment")
		if placesError := hasher.places(builder, typed.Children); placesError != nil {
			return placesError
		}
		hasher.set(lvalue, hasher.allocate(builder.Sum()))
		return nil
	case *high_level_intermediate_representation.JsxText:
		hasher.set(lvalue, duplicate_functions.NewDigestBuilder("jsx-text").Text(typed.Value).Sum())
		return nil
	case *high_level_intermediate_representation.StartMemoize:
		return nil
	case *high_level_intermediate_representation.FinishMemoize:
		value, valueError := hasher.value(typed.Value)
		hasher.set(lvalue, value)
		return valueError
	case *high_level_intermediate_representation.Debugger:
		effect(duplicate_functions.NewDigestBuilder("debugger"), memory)
		return nil
	case *high_level_intermediate_representation.UnsupportedNode:
		return refuse(duplicate_functions.RefusalUnsupportedNode, typed.Reason)
	}
	return notHashable(fmt.Sprintf("instruction %T", instruction.Value))
}

// load is a read of a local or a captured binding: its value, or for a memory binding a read of the chain.
func (hasher *functionHasher) load(
	lvalue high_level_intermediate_representation.Place,
	place high_level_intermediate_representation.Place,
	memory duplicate_functions.Digest,
) error {
	if hasher.isCell(place.Identifier) {
		hasher.set(lvalue, hasher.cellLoad(place, memory))
		return nil
	}
	value, valueError := hasher.value(place)
	hasher.set(lvalue, value)
	return valueError
}

func (hasher *functionHasher) storeUpdate(
	place high_level_intermediate_representation.Place,
	updated duplicate_functions.Digest,
	memory *duplicate_functions.Digest,
) {
	if hasher.isCell(place.Identifier) {
		hasher.cellStore(place, updated, memory)
		return
	}
	hasher.set(place, updated)
}

func (hasher *functionHasher) object(
	lvalue high_level_intermediate_representation.Place,
	typed *high_level_intermediate_representation.ObjectExpression,
) error {
	builder := duplicate_functions.NewDigestBuilder("object").Number(len(typed.Properties))
	for _, property := range typed.Properties {
		value, valueError := hasher.value(property.Value)
		if valueError != nil {
			return valueError
		}
		builder.Flag(property.Spread).Text(property.Key).Digest(value)
		if property.ComputedKey != nil {
			key, keyError := hasher.value(*property.ComputedKey)
			if keyError != nil {
				return keyError
			}
			builder.Text("computed").Digest(key)
		}
	}
	hasher.set(lvalue, hasher.allocate(builder.Sum()))
	return nil
}

func (hasher *functionHasher) closure(
	lvalue high_level_intermediate_representation.Place,
	typed *high_level_intermediate_representation.FunctionExpression,
) error {
	nested, nestedError := hasher.nested(typed.Function)
	if nestedError != nil {
		return nestedError
	}
	builder := duplicate_functions.NewDigestBuilder("closure").Digest(nested).Number(len(typed.Captures))
	for _, capture := range typed.Captures {
		// Lowering writes an empty place for a capture it could not resolve, and identifier 0 is the Returns place,
		// so reading it would hash the return value in the capture's stead.
		if capture.Identifier == hasher.function.Returns.Identifier {
			return refuse(duplicate_functions.RefusalUnresolvedCapture, "")
		}
		if hasher.isCell(capture.Identifier) {
			builder.Text("cell").Digest(hasher.cell(capture.Identifier))
			continue
		}
		value, valueError := hasher.value(capture)
		if valueError != nil {
			return valueError
		}
		builder.Digest(value)
	}
	hasher.set(lvalue, hasher.allocate(builder.Sum()))
	return nil
}

func (hasher *functionHasher) jsx(
	lvalue high_level_intermediate_representation.Place,
	typed *high_level_intermediate_representation.JsxExpression,
) error {
	builder := duplicate_functions.NewDigestBuilder("jsx")
	if typed.Tag.Place != nil {
		tag, tagError := hasher.value(*typed.Tag.Place)
		if tagError != nil {
			return tagError
		}
		builder.Digest(tag)
	} else {
		builder.Text(typed.Tag.Name)
	}
	builder.Number(len(typed.Properties))
	for _, attribute := range typed.Properties {
		value, valueError := hasher.value(attribute.Value)
		if valueError != nil {
			return valueError
		}
		builder.Text(attribute.Name).Flag(attribute.Spread).Digest(value)
	}
	if placesError := hasher.places(builder, typed.Children); placesError != nil {
		return placesError
	}
	hasher.set(lvalue, hasher.allocate(builder.Sum()))
	return nil
}

// pattern binds a destructuring pattern's leaves, each as a read of its path from the source.
func (hasher *functionHasher) pattern(
	pattern high_level_intermediate_representation.Pattern,
	source duplicate_functions.Digest,
	memory *duplicate_functions.Digest,
) error {
	switch typed := pattern.(type) {
	case *high_level_intermediate_representation.PlacePattern:
		hasher.storeUpdate(typed.Place, source, memory)
		return nil
	case *high_level_intermediate_representation.ObjectPattern:
		var keys []string
		for _, property := range typed.Properties {
			builder := duplicate_functions.NewDigestBuilder("destructure-property").Digest(source).Text(property.Key)
			keys = append(keys, property.Key)
			if property.ComputedKey != nil {
				key, keyError := hasher.value(*property.ComputedKey)
				if keyError != nil {
					return keyError
				}
				builder.Text("computed").Digest(key)
			}
			if property.Default != nil {
				value, valueError := hasher.value(*property.Default)
				if valueError != nil {
					return valueError
				}
				builder.Text("default").Digest(value)
			}
			if patternError := hasher.pattern(
				property.Value,
				builder.Digest(*memory).Sum(),
				memory,
			); patternError != nil {
				return patternError
			}
		}
		if typed.Rest != nil {
			builder := duplicate_functions.NewDigestBuilder("destructure-object-rest").Digest(source)
			for _, key := range keys {
				builder.Text(key)
			}
			return hasher.pattern(
				&high_level_intermediate_representation.PlacePattern{Place: *typed.Rest},
				builder.Digest(*memory).Sum(),
				memory,
			)
		}
		return nil
	case *high_level_intermediate_representation.ArrayPattern:
		for index, element := range typed.Elements {
			if element.Value == nil {
				continue
			}
			builder := duplicate_functions.NewDigestBuilder("destructure-element").Digest(source).Number(index)
			if element.Default != nil {
				value, valueError := hasher.value(*element.Default)
				if valueError != nil {
					return valueError
				}
				builder.Text("default").Digest(value)
			}
			if patternError := hasher.pattern(
				element.Value,
				builder.Digest(*memory).Sum(),
				memory,
			); patternError != nil {
				return patternError
			}
		}
		if typed.Rest != nil {
			builder := duplicate_functions.NewDigestBuilder("destructure-array-rest").Digest(source).Number(
				len(typed.Elements),
			)
			return hasher.pattern(
				&high_level_intermediate_representation.PlacePattern{Place: *typed.Rest},
				builder.Digest(*memory).Sum(),
				memory,
			)
		}
		return nil
	}
	return notHashable(fmt.Sprintf("pattern %T", pattern))
}

// nested hashes a function lowered inside this one, by its own body alone; what it captures is hashed by its parent.
func (hasher *functionHasher) nested(
	id high_level_intermediate_representation.FunctionId,
) (duplicate_functions.Digest, error) {
	if int(id) >= len(hasher.function.Functions) {
		return duplicate_functions.Digest{}, notHashable("a nested function out of range")
	}
	inner := newFunctionHasher(hasher.file, hasher.function.Functions[id])
	inner.dependencies = hasher.dependencies
	digest, hashError := inner.hashFunction()
	hasher.size += inner.size
	return digest, hashError
}
