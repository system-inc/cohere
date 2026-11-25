// Package parity holds the shapes of conversion the check reads, from byte slices of many spellings, and the shapes of
// reader it accepts or refuses, where the port and staticcheck's SA6001 still agree finding for finding. The
// differential test runs both over each and compares.
package parity

type namedBytes []byte

type namedByte byte

type key string

type holder struct{ lookup map[string]int }

func consume(values ...any) {}

// Found by both: the converted string read by two lookups.
func twoLookups(lookup map[string]int, data []byte) int {
	converted := string(data)
	return lookup[converted] + lookup[converted]
}

// Found by both: a lookup with comma-ok.
func commaOk(lookup map[string]int, data []byte) bool {
	converted := string(data)
	_, present := lookup[converted]
	return present
}

// Found by both: lookups in two different maps.
func twoMaps(first map[string]int, second map[string]bool, data []byte) (int, bool) {
	converted := string(data)
	return first[converted], second[converted]
}

// Found by both: a named byte slice type.
func namedSlice(lookup map[string]int, data namedBytes) int {
	converted := string(data)
	return lookup[converted]
}

// Found by both: a slice of a named type whose underlying type is byte.
func sliceOfNamedByte(lookup map[string]int, data []namedByte) int {
	converted := string(data)
	return lookup[converted]
}

// Found by both: a slice of an array.
func sliceOfArray(lookup map[string]int, data [8]byte) int {
	converted := string(data[:])
	return lookup[converted]
}

// Found by both: the lookup inside a loop, the converted string defined once before it.
func lookupInLoop(lookup map[string]int, data []byte, times int) int {
	converted := string(data)
	total := 0
	for index := 0; index < times; index++ {
		total += lookup[converted]
	}
	return total
}

// Found by both: the lookup on a field's map.
func fieldMap(value holder, data []byte) int {
	converted := string(data)
	return value.lookup[converted]
}

// Found by both: a type parameter of byte slices alone.
func genericBytes[T ~[]byte](lookup map[string]int, data T) int {
	converted := string(data)
	return lookup[converted]
}

// Found by both: a type parameter of a string or a byte slice, one conversion per term.
func genericStringOrBytes[T ~string | ~[]byte](lookup map[string]int, data T) int {
	converted := string(data)
	return lookup[converted]
}

// Found by both: the map's value is itself a map indexed by the same name.
func nestedMaps(lookup map[string]map[string]int, data []byte) int {
	converted := string(data)
	return lookup[converted][converted]
}

// Found by neither: converted inside the lookup, which the compiler already optimizes.
func inline(lookup map[string]int, data []byte) int {
	return lookup[string(data)]
}

// Found by neither: the converted string is printed as well as looked up.
func alsoPrinted(lookup map[string]int, data []byte) int {
	converted := string(data)
	consume(converted)
	return lookup[converted]
}

// Found by neither: the converted string is a key the map is updated at.
func mapUpdate(lookup map[string]int, data []byte) {
	converted := string(data)
	lookup[converted] = 1
}

// Found by neither: the lookup's key is in parentheses, not a bare name.
func parenthesizedKey(lookup map[string]int, data []byte) int {
	converted := string(data)
	return lookup[(converted)]
}

// Found by neither: the converted string is never read.
func unread(data []byte) {
	converted := string(data)
	_ = converted
}

// Found by neither: the string is reassigned on one path, so a phi reads the conversion.
func reassignedSometimes(lookup map[string]int, data []byte, condition bool) int {
	converted := string(data)
	if condition {
		converted = "other"
	}
	return lookup[converted]
}

// Found by neither: the string is captured by a function literal.
func captured(lookup map[string]int, data []byte) func() int {
	converted := string(data)
	return func() int { return lookup[converted] }
}

// Found by neither: the map's keys are interfaces, so the string is boxed before the lookup.
func interfaceKeys(lookup map[any]int, data []byte) int {
	converted := string(data)
	return lookup[converted]
}

// Found by neither: the conversion is to a named string type.
func namedString(lookup map[key]int, data []byte) int {
	converted := key(data)
	return lookup[converted]
}

// Found by neither: a slice of uint8, spelled so, is no slice of byte by the identity the check compares.
func sliceOfUint8(lookup map[string]int, data []uint8) int {
	converted := string(data)
	return lookup[converted]
}

// Found by neither: a slice of runes.
func runes(lookup map[string]int, data []rune) int {
	converted := string(data)
	return lookup[converted]
}

// Found by neither: a type parameter of strings and integers, no byte slice among them.
func genericNoBytes[T ~string | int](lookup map[string]int, data T) int {
	converted := string(data)
	return lookup[converted]
}

// Found by neither: the converted string is compared as well as looked up.
func alsoCompared(lookup map[string]int, data []byte) int {
	converted := string(data)
	if converted == "" {
		return 0
	}
	return lookup[converted]
}
