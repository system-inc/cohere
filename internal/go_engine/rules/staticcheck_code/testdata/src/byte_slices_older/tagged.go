//go:build go1.21

package byte_slices_older

func tagged(defined octetSlice, bytes []byte) []interface{} {
	return []interface{}{defined, bytes, []octet(nil)}
}
