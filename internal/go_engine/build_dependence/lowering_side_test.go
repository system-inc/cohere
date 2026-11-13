package build_dependence

import (
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"math/big"
	"reflect"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// loweringSide is one build's half of a comparison: its file set, which writes a position as its file and offset,
// the same in both builds since a shared file's bytes are, and the synthetic functions its instructions reached,
// compared after the function that reached them.
type loweringSide struct {
	fileSet *token.FileSet
	places  map[intermediate_representation.Value]string
	reached []*intermediate_representation.Function

	// unknownFields are the instruction fields of a kind the rendering doesn't know, which would otherwise compare
	// as alike without being read.
	unknownFields map[string]bool
}

// place writes position as the file it's in and its offset there, past no //line directive, as both builds parsed it.
func (side *loweringSide) place(position token.Pos) string {
	tokenFile := side.fileSet.File(position)
	if tokenFile == nil {
		return "no position"
	}
	return fmt.Sprintf("%s:%d", tokenFile.Name(), tokenFile.Offset(position))
}

// objectKey names object the same way in both builds: its package's path, the file it's declared in and its offset,
// and its name. An object of the universe, or one without a position, is named by its package and name alone.
func (side *loweringSide) objectKey(object types.Object) string {
	if object == nil {
		return "no object"
	}
	packagePath := "universe"
	if object.Pkg() != nil {
		packagePath = object.Pkg().Path()
	}
	return fmt.Sprintf("%s %s %s", packagePath, side.place(object.Pos()), object.Name())
}

// typeKey writes a type with every package by its path, as types.TypeString does, but for an alias, written as the
// type it stands for, however deep. A shared synthetic function, an instantiation wrapper above all, is created by
// whichever of its callers the concurrent build reaches first, and spells its signature and its values' types as that
// call spelled them, through an alias or not, which differs from build to build with no difference in the lowering.
// An interface, a union and a type outside go/types are written by types.TypeString as they are.
func typeKey(written types.Type) string {
	if written == nil {
		return "no type"
	}
	var builder strings.Builder
	writeUnaliased(&builder, written)
	return builder.String()
}

// writeUnaliased writes written to builder as typeKey says.
func writeUnaliased(builder *strings.Builder, written types.Type) {
	switch typed := written.(type) {
	case *types.Alias:
		writeUnaliased(builder, types.Unalias(typed))
	case *types.Named:
		if typed.Obj().Pkg() != nil {
			builder.WriteString(typed.Obj().Pkg().Path() + ".")
		}
		builder.WriteString(typed.Obj().Name())
		if arguments := typed.TypeArgs(); arguments.Len() > 0 {
			builder.WriteString("[")
			for index := range arguments.Len() {
				if index > 0 {
					builder.WriteString(", ")
				}
				writeUnaliased(builder, arguments.At(index))
			}
			builder.WriteString("]")
		}
	case *types.Pointer:
		builder.WriteString("*")
		writeUnaliased(builder, typed.Elem())
	case *types.Slice:
		builder.WriteString("[]")
		writeUnaliased(builder, typed.Elem())
	case *types.Array:
		fmt.Fprintf(builder, "[%d]", typed.Len())
		writeUnaliased(builder, typed.Elem())
	case *types.Map:
		builder.WriteString("map[")
		writeUnaliased(builder, typed.Key())
		builder.WriteString("]")
		writeUnaliased(builder, typed.Elem())
	case *types.Chan:
		fmt.Fprintf(builder, "chan(%d) ", typed.Dir())
		writeUnaliased(builder, typed.Elem())
	case *types.Signature:
		builder.WriteString("func")
		writeUnaliased(builder, typed.Params())
		if typed.Variadic() {
			builder.WriteString(" variadic")
		}
		builder.WriteString(" ")
		writeUnaliased(builder, typed.Results())
	case *types.Tuple:
		builder.WriteString("(")
		for index := range typed.Len() {
			variable := typed.At(index)
			if index > 0 {
				builder.WriteString(", ")
			}
			builder.WriteString(variable.Name() + " ")
			writeUnaliased(builder, variable.Type())
		}
		builder.WriteString(")")
	case *types.Struct:
		builder.WriteString("struct{")
		for index := range typed.NumFields() {
			field := typed.Field(index)
			if index > 0 {
				builder.WriteString("; ")
			}
			fmt.Fprintf(builder, "%s embedded %t ", field.Name(), field.Embedded())
			writeUnaliased(builder, field.Type())
			if tag := typed.Tag(index); tag != "" {
				fmt.Fprintf(builder, " %q", tag)
			}
		}
		builder.WriteString("}")
	default:
		builder.WriteString(types.TypeString(written, qualifierByPackagePath))
	}
}

// constantKey writes a constant value exactly, with its kind.
func constantKey(value constant.Value) string {
	if value == nil {
		return "zero"
	}
	return fmt.Sprintf("%s %s", value.Kind(), value.ExactString())
}

// placeFunction names each value local to function by where it sits: a parameter or free variable by its index, a
// value-defining instruction by its block and index. Operands are compared by these names.
func (side *loweringSide) placeFunction(function *intermediate_representation.Function) {
	side.places = map[intermediate_representation.Value]string{}
	for index, parameter := range function.Parameters {
		side.places[parameter] = fmt.Sprintf("parameter %d", index)
	}
	for index, free := range function.FreeVariables {
		side.places[free] = fmt.Sprintf("free variable %d", index)
	}
	for _, block := range function.Blocks {
		for index, instruction := range block.Instructions {
			if value, isValue := instruction.(intermediate_representation.Value); isValue {
				side.places[value] = fmt.Sprintf("b%d.%d", block.Index, index)
			}
		}
	}
}

// functionKey names a function the same way in both builds: a declared one by its object, an anonymous one by its
// parent and its index there, and a synthetic one, a wrapper, thunk, bound method or instantiation, by its provenance
// and name, with its object where it has one.
func (side *loweringSide) functionKey(function *intermediate_representation.Function) string {
	if parent := function.Parent(); parent != nil {
		// By its place rather than its index, since a variable's function literal in a file only one build compiles
		// moves the package initializer's others.
		return fmt.Sprintf("anonymous at %s of %s", side.place(function.Syntax().Pos()), side.functionKey(parent))
	}
	if function.Synthetic == "" {
		return "function " + side.objectKey(function.Object())
	}
	return fmt.Sprintf("synthetic %q %s %s", function.Synthetic, function.RelativeString(nil),
		side.objectKey(function.Object()))
}

// operand names an operand: a local value by its place, anything else by a key the same in both builds. A synthetic
// function with a body is kept, to be compared after the function that reached it.
func (side *loweringSide) operand(value intermediate_representation.Value) string {
	if value == nil {
		return "nil"
	}
	if place, isPlace := side.places[value]; isPlace {
		return place
	}
	switch typed := value.(type) {
	case *intermediate_representation.Constant:
		return fmt.Sprintf("constant %s %s", typeKey(typed.Type()), constantKey(typed.Value))
	case *intermediate_representation.Global:
		if typed.Object() == nil {
			return fmt.Sprintf("global %s.%s %s", typed.Package.Types.Path(), typed.Name(), typeKey(typed.Type()))
		}
		return "global " + side.objectKey(typed.Object())
	case *intermediate_representation.Function:
		if typed.Parent() == nil && typed.Synthetic != "" && len(typed.Blocks) > 0 {
			side.reached = append(side.reached, typed)
			return side.functionKey(typed) + " built"
		}
		return side.functionKey(typed)
	case *intermediate_representation.Builtin:
		return fmt.Sprintf("builtin %s %s", typed.Name(), typeKey(typed.Type()))
	}
	return fmt.Sprintf("unplaced %T %s %s", value, value.Name(), typeKey(value.Type()))
}

// The reflected types an instruction's fields are rendered by.
var (
	valueInterfaceType    = reflect.TypeFor[intermediate_representation.Value]()
	typeInterfaceType     = reflect.TypeFor[types.Type]()
	functionObjectType    = reflect.TypeFor[*types.Func]()
	positionType          = reflect.TypeFor[token.Pos]()
	constantValueType     = reflect.TypeFor[constant.Value]()
	bigIntegerType        = reflect.TypeFor[big.Int]()
	basicBlockPointerType = reflect.TypeFor[*intermediate_representation.BasicBlock]()
)

// instruction renders one instruction whole: its kind, its position, its comment, its type if it is a value, and
// every exported field it declares, operands by their names, types and objects by their keys, positions as file
// offsets and constants exactly.
func (side *loweringSide) instruction(instruction intermediate_representation.Instruction) string {
	reflected := reflect.ValueOf(instruction).Elem()
	rendered := fmt.Sprintf("%s at %s", reflected.Type().Name(), side.place(instruction.Pos()))
	if comment := instruction.Comment(); comment != "" {
		rendered += fmt.Sprintf(" (%s)", comment)
	}
	if value, isValue := instruction.(intermediate_representation.Value); isValue {
		rendered += " type " + typeKey(value.Type())
	}
	return rendered + " " + side.structure(reflected)
}

// structure renders a struct of our intermediate representation by its exported fields, the embedded ones, which
// hold only what instruction renders itself, left out.
func (side *loweringSide) structure(reflected reflect.Value) string {
	var fields []string
	for index := range reflected.NumField() {
		field := reflected.Type().Field(index)
		if !field.IsExported() || field.Anonymous {
			continue
		}
		fields = append(fields, field.Name+": "+side.field(reflected.Type().Name()+"."+field.Name,
			reflected.Field(index)))
	}
	return "{" + strings.Join(fields, ", ") + "}"
}

// field renders one field's value, recording a kind it doesn't know rather than passing over it.
func (side *loweringSide) field(label string, reflected reflect.Value) string {
	fieldType := reflected.Type()
	switch {
	case fieldType == valueInterfaceType || fieldType.Kind() == reflect.Pointer && fieldType.Implements(
		valueInterfaceType):
		if reflected.IsNil() {
			return "nil"
		}
		operand, isOperand := reflected.Interface().(intermediate_representation.Value)
		if !isOperand {
			return "nil"
		}
		return side.operand(operand)
	case fieldType == basicBlockPointerType:
		block, isBlock := reflected.Interface().(*intermediate_representation.BasicBlock)
		if !isBlock || block == nil {
			return "no block"
		}
		return fmt.Sprintf("block %d", block.Index)
	case fieldType == functionObjectType:
		object, isObject := reflected.Interface().(*types.Func)
		if !isObject || object == nil {
			return "no object"
		}
		return side.objectKey(object)
	case fieldType == typeInterfaceType || fieldType.Kind() == reflect.Pointer && fieldType.Implements(
		typeInterfaceType):
		if reflected.IsNil() {
			return "no type"
		}
		written, isType := reflected.Interface().(types.Type)
		if !isType {
			return "no type"
		}
		return typeKey(written)
	case fieldType == positionType:
		position, isPosition := reflected.Interface().(token.Pos)
		if !isPosition {
			return "no position"
		}
		return side.place(position)
	case fieldType == constantValueType:
		if reflected.IsNil() {
			return "zero"
		}
		value, isValue := reflected.Interface().(constant.Value)
		if !isValue {
			return "zero"
		}
		return constantKey(value)
	case fieldType == bigIntegerType:
		integer, isInteger := reflected.Addr().Interface().(*big.Int)
		if !isInteger {
			return "no integer"
		}
		return integer.String()
	}
	switch fieldType.Kind() {
	case reflect.Slice:
		elements := make([]string, 0, reflected.Len())
		for index := range reflected.Len() {
			elements = append(elements, side.field(fmt.Sprintf("%s[%d]", label, index), reflected.Index(index)))
		}
		return "[" + strings.Join(elements, ", ") + "]"
	case reflect.Pointer:
		if fieldType.Elem().Kind() == reflect.Struct && fieldType.Elem().PkgPath() == valueInterfaceType.PkgPath() {
			if reflected.IsNil() {
				return "nil"
			}
			return side.structure(reflected.Elem())
		}
	case reflect.Struct:
		if fieldType.PkgPath() == valueInterfaceType.PkgPath() {
			return side.structure(reflected)
		}
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint,
		reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.String:
		return fmt.Sprint(reflected.Interface())
	}
	side.unknownFields[label+" "+fieldType.String()] = true
	return "unknown " + fieldType.String()
}
