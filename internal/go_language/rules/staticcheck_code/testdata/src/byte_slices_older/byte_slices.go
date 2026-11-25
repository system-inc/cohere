// Package byte_slices_older is byte_slices in a module at go 1.17, with a file tagged go1.21.
package byte_slices_older

type octet byte

type octetAlias = byte

type byteSlice []byte

type octetSlice []octet

type byteSliceAlias = []byte

type octets = []octet

func slices(text string, bytes []byte, uints []uint8, defined octetSlice, runes []rune, signed []int8) []interface{} {
	var aliased []octetAlias
	var named byteSlice
	var aliasSlice byteSliceAlias
	var aliasOfOctets octets
	var array [4]byte
	var pointer *[]byte
	var octetElements []octet
	return []interface{}{
		text, bytes, uints, defined, runes, signed, aliased, named, aliasSlice, aliasOfOctets, array, pointer,
		octetElements, bytes[1:], array[:], *pointer, []byte(text), []octet(nil), named[:0], defined[0], 7,
	}
}
