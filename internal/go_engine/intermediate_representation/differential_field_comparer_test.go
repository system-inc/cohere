package intermediate_representation_test

import (
	"fmt"
	"go/types"
	"math/big"
	"reflect"
	"strings"

	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// differentialTheirPlaces names each value local to one go/ir function by where it sits in that function: a
// value-defining instruction by block and index, a parameter or free variable by index. differentialOurPlaces
// names ours the same way, and the two IRs' operands are compared by these names.
type differentialTheirPlaces map[ir.Value]string

type differentialOurPlaces map[intermediate_representation.Value]string

func differentialPlaceTheirs(function *ir.Function) differentialTheirPlaces {
	places := differentialTheirPlaces{}
	for index, parameter := range function.Params {
		places[parameter] = fmt.Sprintf("parameter %d", index)
	}
	for index, free := range function.FreeVars {
		places[free] = fmt.Sprintf("free variable %d", index)
	}
	for _, block := range function.Blocks {
		for index, instruction := range block.Instrs {
			if value, isValue := instruction.(ir.Value); isValue {
				places[value] = fmt.Sprintf("b%d.%d", block.Index, index)
			}
		}
	}
	return places
}

func differentialPlaceOurs(function *intermediate_representation.Function) differentialOurPlaces {
	places := differentialOurPlaces{}
	for index, parameter := range function.Parameters {
		places[parameter] = fmt.Sprintf("parameter %d", index)
	}
	for index, free := range function.FreeVariables {
		places[free] = fmt.Sprintf("free variable %d", index)
	}
	for _, block := range function.Blocks {
		for index, instruction := range block.Instructions {
			if value, isValue := instruction.(intermediate_representation.Value); isValue {
				places[value] = fmt.Sprintf("b%d.%d", block.Index, index)
			}
		}
	}
	return places
}

// differentialTheirOperand names a go/ir operand the way differentialOurOperand names ours: a local value by its
// place, a constant by its value and type, a global, function or builtin by its name.
func differentialTheirOperand(places differentialTheirPlaces, value ir.Value) string {
	if value == nil {
		return "nil"
	}
	if place, isPlace := places[value]; isPlace {
		return place
	}
	switch value := value.(type) {
	case *ir.Const:
		return "constant " + value.String()
	case *ir.Global:
		return "global " + value.RelString(nil)
	case *ir.Function:
		return "function " + differentialTheirFunctionName(value)
	case *ir.Builtin:
		return "builtin " + value.Name()
	}
	return fmt.Sprintf("unplaced %T", value)
}

func differentialOurOperand(places differentialOurPlaces, value intermediate_representation.Value) string {
	if value == nil {
		return "nil"
	}
	if place, isPlace := places[value]; isPlace {
		return place
	}
	switch value := value.(type) {
	case *intermediate_representation.Constant:
		return "constant " + value.String()
	case *intermediate_representation.Global:
		return "global " + value.RelativeString(nil)
	case *intermediate_representation.Function:
		return "function " + value.RelativeString(nil)
	case *intermediate_representation.Builtin:
		return "builtin " + value.Name()
	}
	return fmt.Sprintf("unplaced %T", value)
}

// differentialTheirFunctionName is value's name as go/ir prints it, but for an instantiation wrapper of a function:
// ours names that from its type arguments unaliased (unalias.go), by the decision on #6pt3bt8 that a lazy or
// concurrent build's names can't follow its build order, while go/ir spells them as whichever call created the
// wrapper first did. So go/ir's is compared unaliased. A real naming difference still fails: the origin and the
// unaliased arguments must both agree.
func differentialTheirFunctionName(value *ir.Function) string {
	if canonical, isInstance := differentialCanonicalInstanceName(value); isInstance {
		return canonical
	}
	return value.RelString(nil)
}

// differentialCanonicalInstanceName is the name ours gives the instantiation wrapper value is, and whether value is
// one: its origin's name and its type arguments unaliased.
func differentialCanonicalInstanceName(value *ir.Function) (string, bool) {
	if !strings.HasPrefix(value.Synthetic, "instantiation wrapper of ") || value.Signature.Recv() != nil ||
		len(value.TypeArgs()) == 0 {
		return "", false
	}
	arguments := make([]string, 0, len(value.TypeArgs()))
	for _, argument := range value.TypeArgs() {
		arguments = append(arguments, types.TypeString(intermediate_representation.UnaliasDeeplyForTest(argument), nil))
	}
	return value.Origin().RelString(nil) + "[" + strings.Join(arguments, ", ") + "]", true
}

// differentialAliasSpelledInstances counts the instantiation wrappers functions call whose go/ir name spells a type
// argument through an alias, the names differentialTheirFunctionName compares unaliased.
func differentialAliasSpelledInstances(functions []*ir.Function) int {
	spelled := map[*ir.Function]bool{}
	for _, function := range functions {
		for _, block := range function.Blocks {
			for _, instruction := range block.Instrs {
				for _, operand := range instruction.Operands(nil) {
					callee, isFunction := (*operand).(*ir.Function)
					if !isFunction {
						continue
					}
					if canonical, isInstance := differentialCanonicalInstanceName(callee); isInstance &&
						canonical != callee.RelString(nil) {
						spelled[callee] = true
					}
				}
			}
		}
	}
	return len(spelled)
}

// differentialKind is the name of an instruction's or value's pointer type, go/ir's mapped to ours.
func differentialKind(kind reflect.Type) string {
	name := kind.Elem().Name()
	if ours, isRenamed := differentialKinds[name]; isRenamed {
		return ours
	}
	return name
}

// differentialCompareInstruction compares one instruction pair: kind, position, type when it is a value, then
// every exported field go/ir declares, matched to ours by name through differentialFields.
func differentialCompareInstruction(theirPlaces differentialTheirPlaces, ourPlaces differentialOurPlaces,
	theirs ir.Instruction, ours intermediate_representation.Instruction) (cause string, theirRendering string,
	ourRendering string, differs bool) {
	theirKind, ourKind := differentialKind(reflect.TypeOf(theirs)), differentialKind(reflect.TypeOf(ours))
	if theirKind != ourKind {
		return "instruction kind", theirKind, ourKind, true
	}
	if theirs.Pos() != ours.Pos() {
		return "position of " + ourKind, fmt.Sprint(theirs.Pos()), fmt.Sprint(ours.Pos()), true
	}
	theirValue, theirIsValue := theirs.(ir.Value)
	ourValue, ourIsValue := ours.(intermediate_representation.Value)
	if theirIsValue != ourIsValue {
		return "value or not, " + ourKind, fmt.Sprint(theirIsValue), fmt.Sprint(ourIsValue), true
	}
	if theirIsValue && !differentialIdenticalTypes(theirValue.Type(), ourValue.Type()) {
		return "type of " + ourKind, fmt.Sprint(theirValue.Type()), fmt.Sprint(ourValue.Type()), true
	}
	comparer := differentialFieldComparer{theirPlaces: theirPlaces, ourPlaces: ourPlaces}
	return comparer.compareStruct(reflect.TypeOf(theirs).Elem().Name(), ourKind, reflect.ValueOf(theirs).Elem(),
		reflect.ValueOf(ours).Elem())
}

func differentialIdenticalTypes(theirs types.Type, ours types.Type) (identical bool) {
	if theirs == nil || ours == nil {
		return theirs == nil && ours == nil
	}
	// Each IR types its ssa:deferstack values with its own typeutil.DeferStack, a types.Type outside go/types that
	// types.Identical panics on wherever it sits in a type. Such a pair is compared by how it prints.
	defer func() {
		if recovered := recover(); recovered != nil {
			identical = types.TypeString(theirs, nil) == types.TypeString(ours, nil)
		}
	}()
	return types.Identical(theirs, ours)
}

// differentialFieldComparer compares two structs field by field, by reflection, knowing an operand by the place it
// names in its own IR.
type differentialFieldComparer struct {
	theirPlaces differentialTheirPlaces
	ourPlaces   differentialOurPlaces
}

var (
	differentialTheirValueType = reflect.TypeFor[ir.Value]()
	differentialOurValueType   = reflect.TypeFor[intermediate_representation.Value]()
	differentialTypeType       = reflect.TypeFor[types.Type]()
	differentialBigType        = reflect.TypeFor[big.Int]()
)

// differentialDroppedFields are go/ir's fields ours doesn't carry by design. SelectState.DebugNode holds the syntax
// a debug reference points at, and debug references are out of ours (go-ir-shape.md, section 1: "Members nobody
// reads are not built. That includes DebugRef").
var differentialDroppedFields = map[string]map[string]bool{
	"SelectState": {"DebugNode": true},
}

// compareStruct compares every exported field of go/ir's struct, named theirName, against ours. A field go/ir has
// and ours lacks, and a field kind it doesn't know, are differences of their own, so a field is never passed over
// silently.
func (comparer differentialFieldComparer) compareStruct(theirName string, ourName string, theirs reflect.Value,
	ours reflect.Value) (cause string, theirRendering string, ourRendering string, differs bool) {
	for index := range theirs.NumField() {
		field := theirs.Type().Field(index)
		if !field.IsExported() || field.Anonymous {
			continue
		}
		if differentialDroppedFields[theirName][field.Name] {
			// go/ir sets it only in debug mode, which neither build here runs, so it must be empty.
			if !theirs.Field(index).IsZero() {
				return "go/ir's debug field " + theirName + "." + field.Name + " is set", field.Name, "", true
			}
			continue
		}
		ourFieldName := field.Name
		if renamed, isRenamed := differentialFields[theirName][field.Name]; isRenamed {
			ourFieldName = renamed
		}
		ourField := ours.FieldByName(ourFieldName)
		label := ourName + "." + ourFieldName
		if !ourField.IsValid() {
			return "comparator: no field " + label, field.Name, "", true
		}
		cause, theirRendering, ourRendering, differs = comparer.compareField(label, theirs.Field(index), ourField)
		if differs {
			return cause, theirRendering, ourRendering, true
		}
	}
	return "", "", "", false
}

func (comparer differentialFieldComparer) compareField(label string, theirs reflect.Value, ours reflect.Value) (
	cause string, theirRendering string, ourRendering string, differs bool) {
	switch {
	case theirs.Type() == differentialTheirValueType:
		if ours.Type() != differentialOurValueType {
			return "comparator: field " + label + " is no operand in ours", theirs.Type().String(),
				ours.Type().String(), true
		}
		theirOperand := differentialTheirOperand(comparer.theirPlaces, differentialInterface[ir.Value](theirs))
		ourOperand := differentialOurOperand(comparer.ourPlaces,
			differentialInterface[intermediate_representation.Value](ours))
		if theirOperand != ourOperand {
			return "operand of " + strings.Split(label, ".")[0], label + " " + theirOperand,
				label + " " + ourOperand, true
		}
		return "", "", "", false
	case theirs.Type() == differentialTypeType:
		theirType := differentialInterface[types.Type](theirs)
		ourType := differentialInterface[types.Type](ours)
		if !differentialIdenticalTypes(theirType, ourType) {
			return "type field " + label, fmt.Sprint(theirType), fmt.Sprint(ourType), true
		}
		return "", "", "", false
	case theirs.Type() == differentialBigType:
		theirBits, theirIsBig := theirs.Addr().Interface().(*big.Int)
		ourBits, ourIsBig := ours.Addr().Interface().(*big.Int)
		if !theirIsBig || !ourIsBig || theirBits.Cmp(ourBits) != 0 {
			return "field " + label, theirBits.String(), fmt.Sprint(ours.Interface()), true
		}
		return "", "", "", false
	}
	switch theirs.Kind() {
	case reflect.Slice:
		if theirs.Len() != ours.Len() {
			return "length of " + label, fmt.Sprint(theirs.Len()), fmt.Sprint(ours.Len()), true
		}
		for index := range theirs.Len() {
			cause, theirRendering, ourRendering, differs = comparer.compareField(fmt.Sprintf("%s[%d]", label, index),
				theirs.Index(index), ours.Index(index))
			if differs {
				return cause, theirRendering, ourRendering, true
			}
		}
		return "", "", "", false
	case reflect.Pointer:
		element := theirs.Type().Elem()
		if element.Kind() == reflect.Struct && element.PkgPath() == differentialTheirValueType.PkgPath() {
			if theirs.IsNil() || ours.IsNil() {
				if theirs.IsNil() != ours.IsNil() {
					return "nil " + label, fmt.Sprint(theirs.IsNil()), fmt.Sprint(ours.IsNil()), true
				}
				return "", "", "", false
			}
			return comparer.compareStruct(theirs.Type().Elem().Name(), ours.Type().Elem().Name(), theirs.Elem(),
				ours.Elem())
		}
		// A pointer into go/types, as CallCommon.Method's *types.Func, is the same object on both sides.
		if theirs.Interface() != ours.Interface() {
			return "field " + label, fmt.Sprint(theirs.Interface()), fmt.Sprint(ours.Interface()), true
		}
		return "", "", "", false
	case reflect.Struct:
		if theirs.Type().PkgPath() == differentialTheirValueType.PkgPath() {
			return comparer.compareStruct(theirs.Type().Name(), ours.Type().Name(), theirs, ours)
		}
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint,
		reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.String:
		if theirs.Type() != ours.Type() || theirs.Interface() != ours.Interface() {
			return "field " + label, fmt.Sprint(theirs.Interface()), fmt.Sprint(ours.Interface()), true
		}
		return "", "", "", false
	}
	return "comparator: unknown field kind " + label, theirs.Type().String(), ours.Type().String(), true
}

// differentialInterface is the interface a reflected field holds, or the zero value for a nil one.
func differentialInterface[T any](field reflect.Value) T {
	var zero T
	if field.IsNil() {
		return zero
	}
	held, isHeld := field.Interface().(T)
	if !isHeld {
		return zero
	}
	return held
}
