package asm_decl

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strconv"

	"golang.org/x/tools/go/analysis"
)

// variableKind is what an assembly variable holds, as an instruction's operand size is judged against it: the kinds
// 1, 2, 4, 8 and 16 stand for a value of that many bytes, and the kinds below for composites whose words are judged
// one at a time. x/tools' asmdecl asmKind.
type variableKind int

// The composite kinds, none of them a valid size. x/tools' asmdecl asmString through asmComplex.
const (
	stringKind variableKind = 100 + iota
	sliceKind
	arrayKind
	interfaceKind
	emptyInterfaceKind
	structKind
	complexKind
)

// argumentFrame is the variables one Go declaration gives its assembly on one architecture: every parameter and
// result by name, and every component of each, such as a string's `_base` and `_len`. x/tools' asmdecl asmFunc.
type argumentFrame struct {
	architecture *architecture
	// size is the bytes of every argument and result together, which the `TEXT` line's `$frame-size` must state.
	size      int
	variables map[string]*variable
	// variablesByOffset holds, for every byte of the frame, the innermost variable that covers it.
	variablesByOffset map[int]*variable
}

// variable is one name assembly can address in the argument frame, `name+offset(FP)`. x/tools' asmdecl asmVar.
type variable struct {
	name     string
	kind     variableKind
	typeName string
	offset   int
	size     int
	// inner are the components directly inside a composite variable, a string's base and length, a struct's fields.
	inner []*variable
}

// component is one addressable part of a value of some type, the value itself among them: a string is the string,
// its base and its length. x/tools' asmdecl component.
type component struct {
	size   int
	offset int
	kind   variableKind
	// typeName is the component's type as a message names it: `int32`, `string base`, `half uint64`.
	typeName string
	// suffix follows the variable's name to name the component, `_base` for a string's base, `_0_lo` for the low
	// half of the first element of a [1]uint64 on a 32-bit architecture.
	suffix string
	// outer is the suffix of the composite immediately containing the component.
	outer string
}

// argumentFramesOf is declaration's argument frame on every architecture, by the architecture's name. x/tools'
// asmdecl asmParseDecl.
func argumentFramesOf(pass *analysis.Pass, declaration *ast.FuncDecl) map[string]*argumentFrame {
	frames := make(map[string]*argumentFrame)
	for _, judged := range architectures {
		frame := &argumentFrame{
			architecture:      judged,
			variables:         make(map[string]*variable),
			variablesByOffset: make(map[int]*variable),
		}
		offset := addFields(pass, frame, declaration.Type.Params.List, false, 0)
		if declaration.Type.Results != nil && len(declaration.Type.Results.List) > 0 {
			// The results start at an offset aligned for the widest value the architecture aligns.
			resultsOffset := offset + (-offset & (judged.maximumAlignment - 1))
			offset = addFields(pass, frame, declaration.Type.Results.List, true, resultsOffset)
		}
		frame.size = offset
		frames[judged.name] = frame
	}
	return frames
}

// addFields adds to frame the variables of each field in fields, the parameters or, when areResults, the results,
// laid out from offset, and returns the offset past the last. x/tools' asmdecl asmParseDecl's addParams.
func addFields(pass *analysis.Pass, frame *argumentFrame, fields []*ast.Field, areResults bool, offset int) int {
	next := offset
	namesBefore := 0
	for _, field := range fields {
		fieldType := pass.TypesInfo.Types[field.Type].Type
		// go/types records no type for a variadic parameter's `...T` (golang.org/issue/28277), which is a []T.
		if fieldType == nil {
			if ellipsis, isEllipsis := field.Type.(*ast.Ellipsis); isEllipsis {
				fieldType = types.NewSlice(pass.TypesInfo.Types[ellipsis.Elt].Type)
			}
		}
		alignment := int(frame.architecture.sizes.Alignof(fieldType))
		size := int(frame.architecture.sizes.Sizeof(fieldType))
		next += -next & (alignment - 1)
		components := componentsOfType(frame.architecture, fieldType)
		names := []string{}
		for _, identifier := range field.Names {
			names = append(names, identifier.Name)
		}
		// Unnamed parameters are called arg, arg1, arg2 and so on, and unnamed results ret, ret1, ret2, numbered by
		// how many names came before in the list.
		if len(names) == 0 {
			name := "arg"
			if areResults {
				name = "ret"
			}
			if namesBefore > 0 {
				name += strconv.Itoa(namesBefore)
			}
			names = []string{name}
		}
		namesBefore += len(names)
		for _, name := range names {
			for _, part := range components {
				added := &variable{
					name:     name + part.suffix,
					kind:     part.kind,
					typeName: part.typeName,
					offset:   next + part.offset,
					size:     part.size,
				}
				if outer := frame.variables[name+part.outer]; outer != nil {
					outer.inner = append(outer.inner, added)
				}
				frame.variables[added.name] = added
				for byteIndex := range added.size {
					frame.variablesByOffset[added.offset+byteIndex] = added
				}
			}
			next += size
		}
	}
	return next
}

// kindForType is the kind of a value of type valueType that is size bytes. x/tools' asmdecl asmKindForType.
func kindForType(valueType types.Type, size int) variableKind {
	switch underlying := valueType.Underlying().(type) {
	case *types.Basic:
		switch underlying.Kind() {
		case types.String:
			return stringKind
		case types.Complex64, types.Complex128:
			return complexKind
		default:
			return variableKind(size)
		}
	case *types.Pointer, *types.Chan, *types.Map, *types.Signature:
		return variableKind(size)
	case *types.Struct:
		return structKind
	case *types.Interface:
		if underlying.Empty() {
			return emptyInterfaceKind
		}
		return interfaceKind
	case *types.Array:
		return arrayKind
	case *types.Slice:
		return sliceKind
	default:
		// Invariant: these are every underlying type a parameter or a result can have. A type parameter's underlying
		// type is its constraint's interface, and a tuple or a union is no value's type.
		panic(fmt.Sprintf("asm_decl: no kind for %s, underlying %T", valueType, underlying))
	}
}

// componentsOfType is every component of a value of type valueType on judged, the value itself first: a string, its
// base and its length. x/tools' asmdecl componentsOfType.
func componentsOfType(judged *architecture, valueType types.Type) []component {
	return appendComponents(judged, valueType, nil, "", 0)
}

// appendComponents appends to components those of a value of type valueType at offset, named by suffix, and returns
// them. A struct's fields and an array's elements may be composites themselves, so it recurses. x/tools' asmdecl
// appendComponentsRecursive.
func appendComponents(
	judged *architecture, valueType types.Type, components []component, suffix string, offset int,
) []component {
	typeName := valueType.String()
	size := int(judged.sizes.Sizeof(valueType))
	kind := kindForType(valueType, size)
	appended := append(components, component{
		suffix: suffix, kind: kind, typeName: typeName, offset: offset, size: size, outer: suffix,
	})
	pointerKind := variableKind(judged.pointerSize)
	integerKind := variableKind(judged.integerSize)
	switch kind {
	case 8:
		// A 32-bit architecture moves an 8-byte value as two 4-byte halves, the low one first unless it is big endian.
		if judged.pointerSize == 4 {
			first, second := "lo", "hi"
			if judged.bigEndian {
				first, second = second, first
			}
			appended = append(appended,
				wordComponent(suffix, "_"+first, 4, "half "+typeName, offset, 4),
				wordComponent(suffix, "_"+second, 4, "half "+typeName, offset+4, 4),
			)
		}
	case emptyInterfaceKind:
		appended = append(appended,
			wordComponent(suffix, "_type", pointerKind, "interface type", offset, judged.pointerSize),
			wordComponent(suffix, "_data", pointerKind, "interface data", offset+judged.pointerSize,
				judged.pointerSize),
		)
	case interfaceKind:
		appended = append(appended,
			wordComponent(suffix, "_itable", pointerKind, "interface itable", offset, judged.pointerSize),
			wordComponent(suffix, "_data", pointerKind, "interface data", offset+judged.pointerSize,
				judged.pointerSize),
		)
	case sliceKind:
		appended = append(appended,
			wordComponent(suffix, "_base", pointerKind, "slice base", offset, judged.pointerSize),
			wordComponent(suffix, "_len", integerKind, "slice len", offset+judged.pointerSize, judged.integerSize),
			wordComponent(suffix, "_cap", integerKind, "slice cap", offset+judged.pointerSize+judged.integerSize,
				judged.integerSize),
		)
	case stringKind:
		appended = append(appended,
			wordComponent(suffix, "_base", pointerKind, "string base", offset, judged.pointerSize),
			wordComponent(suffix, "_len", integerKind, "string len", offset+judged.pointerSize, judged.integerSize),
		)
	case complexKind:
		halfSize := size / 2
		appended = append(appended,
			wordComponent(suffix, "_real", variableKind(halfSize), fmt.Sprintf("real(complex%d)", size*8), offset,
				halfSize),
			wordComponent(suffix, "_imag", variableKind(halfSize), fmt.Sprintf("imag(complex%d)", size*8),
				offset+halfSize, halfSize),
		)
	case structKind:
		structType, isStruct := valueType.Underlying().(*types.Struct)
		if !isStruct {
			// Invariant: kindForType gives structKind only to a type whose underlying type is a struct.
			panic("asm_decl: a struct kind's type is not a struct")
		}
		fields := make([]*types.Var, structType.NumFields())
		for fieldIndex := range structType.NumFields() {
			fields[fieldIndex] = structType.Field(fieldIndex)
		}
		offsets := judged.sizes.Offsetsof(fields)
		for fieldIndex, field := range fields {
			appended = appendComponents(judged, field.Type(), appended, suffix+"_"+field.Name(),
				offset+int(offsets[fieldIndex]))
		}
	case arrayKind:
		arrayType, isArray := valueType.Underlying().(*types.Array)
		if !isArray {
			// Invariant: kindForType gives arrayKind only to a type whose underlying type is an array.
			panic("asm_decl: an array kind's type is not an array")
		}
		// The stride between elements is the offset of the second of two fields of the element's type, which
		// counts the padding the element's alignment adds after it.
		pair := []*types.Var{
			types.NewField(token.NoPos, nil, "fake0", arrayType.Elem(), false),
			types.NewField(token.NoPos, nil, "fake1", arrayType.Elem(), false),
		}
		stride := int(judged.sizes.Offsetsof(pair)[1])
		for elementIndex := range int(arrayType.Len()) {
			appended = appendComponents(judged, arrayType.Elem(), appended, suffix+"_"+strconv.Itoa(elementIndex),
				offset+elementIndex*stride)
		}
	}
	return appended
}

// wordComponent is the component of one word inside a composite named by outer: an 8-byte value's halves, an
// interface's type or data, a slice's or a string's base and lengths, a complex number's parts. x/tools' asmdecl
// newComponent, for the components whose suffix extends their composite's.
func wordComponent(outer string, suffix string, kind variableKind, typeName string, offset int, size int) component {
	return component{suffix: outer + suffix, kind: kind, typeName: typeName, offset: offset, size: size, outer: outer}
}
