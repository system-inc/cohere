// Package byte_slices holds slices of bytes and of types defined on byte, and near misses of each, in a package
// loaded outside a module, its files of no language version but the one a `//go:build` line names.
package byte_slices

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
