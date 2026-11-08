//go:build go1.17

package byte_slices_newer

func tagged(defined octetSlice, bytes []byte) []interface{} {
	return []interface{}{defined, bytes, []octet(nil)}
}
