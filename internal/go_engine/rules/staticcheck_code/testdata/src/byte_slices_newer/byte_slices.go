// Package byte_slices_newer is byte_slices in a module at go 1.22, with a file tagged go1.17.
package byte_slices_newer

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
