package check

import (
	"bytes"
	"cmp"
	"encoding"
	"encoding/gob"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// factValueDigest is the digest of a fact's value as gob carries it, which is what an importer's restored fact holds,
// so two runs that export equal facts digest them alike whatever bytes gob wrote. The bytes cannot be the digest: gob
// numbers each type the first time the process encodes one, from a counter every encoder in the process shares, and
// writes that number into each fact's bytes, so an unchanged fact's bytes move with whatever the process encoded
// before it, which in saveMisses is the order its goroutines ran. On one edit of cohere's own root, 749 facts
// differed in their bytes and none in value, and two cold runs disagreed on the bytes of 577 packages' facts
// (#z14ch7c).
//
// The walk reads what gob reads, by gobValueParts' rules. It runs only over a fact encodeFact has already encoded,
// so a value gob refuses never reaches it, and a kind it meets that gob cannot carry is an error rather than a part
// silently skipped.
func factValueDigest(fact analysis.Fact) (cache.Key, error) {
	parts, walkError := gobValueParts(reflect.ValueOf(fact))
	if walkError != nil {
		return cache.Key{}, fmt.Errorf("digesting a %T fact: %w", fact, walkError)
	}
	return cache.DigestOf(parts...), nil
}

// gobEncoderType, binaryMarshalerType and textMarshalerType are the interfaces gob asks a type for, in the order it
// asks, before it reads the type's fields: a type that implements one is sent as the bytes it gives.
var (
	gobEncoderType      = reflect.TypeFor[gob.GobEncoder]()
	binaryMarshalerType = reflect.TypeFor[encoding.BinaryMarshaler]()
	textMarshalerType   = reflect.TypeFor[encoding.TextMarshaler]()
)

// gobValueParts is the parts of value, each one tagged with what it is so no two values' parts run together:
//
//   - A type that implements gob.GobEncoder, encoding.BinaryMarshaler or encoding.TextMarshaler, on its value or its
//     pointer, is its own bytes, as gob sends it, asked in gob's order.
//   - A struct is each field gob sends, in declaration order, by name and value: an exported field, an embedded one by
//     its type's name as gob names it, and never an unexported field or one of a chan or func type or a pointer to
//     one, which gob leaves out of the stream and so out of the restored fact.
//   - A slice is its length and its elements in order, so a nil slice and an empty one are alike, as gob, which sends
//     neither, makes them; an array is the same with its fixed length.
//   - A map is its length and its entries, each key's digest and value's digest, sorted by the key's digest, since a
//     map's order is random and gob's is too.
//   - A pointer is followed, and a nil pointer is a part of its own. That is wider than gob, which also leaves out a
//     pointer to a zero number or string and restores it as nil, while it sends a pointer to an empty struct and
//     restores it as one: telling nil from every pointer costs at most a miss where gob would have restored equal
//     facts, while folding them together would digest alike two facts an importer could tell apart.
//   - An interface is the full name of its dynamic type and its value, since gob sends the name it registered for the
//     concrete type, one name per type, and the value; a nil interface is a part of its own.
//   - A boolean, number or string is its kind and its value, a float by its bits.
//
// A chan, a func or an unsafe pointer anywhere else is an error, as gob refuses it.
func gobValueParts(value reflect.Value) ([][]byte, error) {
	if !value.IsValid() {
		return nil, fmt.Errorf("an invalid value")
	}
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return [][]byte{[]byte("nil pointer"), []byte(gobTypeName(value.Type()))}, nil
	}
	if marshaled, isMarshaled, marshalError := gobMarshaled(value); isMarshaled {
		return [][]byte{[]byte("marshaled"), marshaled}, marshalError
	}
	switch value.Kind() {
	case reflect.Pointer:
		return taggedParts([]byte("pointer"))(gobValueParts(value.Elem()))
	case reflect.Interface:
		if value.IsNil() {
			return [][]byte{[]byte("nil interface")}, nil
		}
		return taggedParts([]byte("interface"), []byte(gobTypeName(value.Elem().Type())))(gobValueParts(value.Elem()))
	case reflect.Struct:
		return gobStructParts(value)
	case reflect.Slice, reflect.Array:
		parts := [][]byte{[]byte(value.Kind().String()), []byte(strconv.Itoa(value.Len()))}
		for index := range value.Len() {
			elementParts, elementError := gobValueParts(value.Index(index))
			if elementError != nil {
				return nil, elementError
			}
			parts = append(parts, elementParts...)
		}
		return parts, nil
	case reflect.Map:
		return gobMapParts(value)
	case reflect.Bool:
		return [][]byte{[]byte("bool"), []byte(strconv.FormatBool(value.Bool()))}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return [][]byte{[]byte("int"), []byte(strconv.FormatInt(value.Int(), 10))}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return [][]byte{[]byte("uint"), []byte(strconv.FormatUint(value.Uint(), 10))}, nil
	case reflect.Float32, reflect.Float64:
		return [][]byte{[]byte("float"), []byte(strconv.FormatUint(math.Float64bits(value.Float()), 16))}, nil
	case reflect.Complex64, reflect.Complex128:
		return [][]byte{
			[]byte("complex"),
			[]byte(strconv.FormatUint(math.Float64bits(real(value.Complex())), 16)),
			[]byte(strconv.FormatUint(math.Float64bits(imag(value.Complex())), 16)),
		}, nil
	case reflect.String:
		return [][]byte{[]byte("string"), []byte(value.String())}, nil
	default:
		return nil, fmt.Errorf("a %s, which gob cannot carry", value.Type())
	}
}

// taggedParts is a function that puts tags before the parts a walk returns, or passes on the walk's error.
func taggedParts(tags ...[]byte) func([][]byte, error) ([][]byte, error) {
	return func(parts [][]byte, walkError error) ([][]byte, error) {
		if walkError != nil {
			return nil, walkError
		}
		return append(append([][]byte{}, tags...), parts...), nil
	}
}

// gobStructParts is the parts of a struct value, each field gob sends by name and value, in declaration order.
func gobStructParts(value reflect.Value) ([][]byte, error) {
	parts := [][]byte{[]byte("struct"), []byte(gobTypeName(value.Type()))}
	for index := range value.NumField() {
		field := value.Type().Field(index)
		if !gobSends(field) {
			continue
		}
		fieldParts, fieldError := gobValueParts(value.Field(index))
		if fieldError != nil {
			return nil, fmt.Errorf("field %s: %w", field.Name, fieldError)
		}
		parts = append(append(parts, []byte("field"), []byte(field.Name)), fieldParts...)
	}
	return parts, nil
}

// gobSends reports whether gob sends a struct field, as its isSent does: an exported field whose type is not a chan
// or a func, nor a pointer to one.
func gobSends(field reflect.StructField) bool {
	if !field.IsExported() {
		return false
	}
	fieldType := field.Type
	for fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
	}
	return fieldType.Kind() != reflect.Chan && fieldType.Kind() != reflect.Func
}

// gobMapParts is the parts of a map value, its length and each entry's key and value digests, sorted by the key's.
func gobMapParts(value reflect.Value) ([][]byte, error) {
	type entryDigest struct {
		key   cache.Key
		value cache.Key
	}
	entries := make([]entryDigest, 0, value.Len())
	iterator := value.MapRange()
	for iterator.Next() {
		keyParts, keyError := gobValueParts(iterator.Key())
		if keyError != nil {
			return nil, fmt.Errorf("a map key: %w", keyError)
		}
		valueParts, valueError := gobValueParts(iterator.Value())
		if valueError != nil {
			return nil, fmt.Errorf("a map value: %w", valueError)
		}
		entries = append(entries, entryDigest{key: cache.DigestOf(keyParts...), value: cache.DigestOf(valueParts...)})
	}
	slices.SortFunc(entries, func(left, right entryDigest) int {
		return cmp.Or(bytes.Compare(left.key[:], right.key[:]), bytes.Compare(left.value[:], right.value[:]))
	})
	parts := [][]byte{[]byte("map"), []byte(strconv.Itoa(len(entries)))}
	for _, entry := range entries {
		parts = append(parts, entry.key[:], entry.value[:])
	}
	return parts, nil
}

// gobMarshaled is the bytes a value gives gob through the first of gob.GobEncoder, encoding.BinaryMarshaler and
// encoding.TextMarshaler it implements, on its value or on a pointer to a copy of it, and whether it implements one.
func gobMarshaled(value reflect.Value) ([]byte, bool, error) {
	for _, marshaler := range []reflect.Type{gobEncoderType, binaryMarshalerType, textMarshalerType} {
		receiver := value
		if !value.Type().Implements(marshaler) {
			if !reflect.PointerTo(value.Type()).Implements(marshaler) {
				continue
			}
			receiver = reflect.New(value.Type())
			receiver.Elem().Set(value)
		}
		var marshaled []byte
		var marshalError error
		switch implementing := receiver.Interface().(type) {
		case gob.GobEncoder:
			marshaled, marshalError = implementing.GobEncode()
		case encoding.BinaryMarshaler:
			marshaled, marshalError = implementing.MarshalBinary()
		case encoding.TextMarshaler:
			marshaled, marshalError = implementing.MarshalText()
		}
		if marshalError != nil {
			return nil, true, fmt.Errorf("marshaling a %s: %w", value.Type(), marshalError)
		}
		return marshaled, true, nil
	}
	return nil, false, nil
}

// gobTypeName is a type's full name, a defined type by its package path and name with a star for each pointer, an
// unnamed type as reflect writes it: one name per type, as gob registers one name per concrete type.
func gobTypeName(reflected reflect.Type) string {
	stars := ""
	named := reflected
	for named.Name() == "" && named.Kind() == reflect.Pointer {
		stars += "*"
		named = named.Elem()
	}
	if named.Name() == "" || named.PkgPath() == "" {
		return stars + named.String()
	}
	return stars + named.PkgPath() + "." + named.Name()
}
