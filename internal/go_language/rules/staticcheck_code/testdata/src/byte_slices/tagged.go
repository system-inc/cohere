//go:build go1.18

package byte_slices

func tagged(defined octetSlice, bytes []byte) []interface{} {
	return []interface{}{defined, bytes, []octet(nil)}
}

func generic[Slice ~[]byte](slice Slice) []interface{} { return []interface{}{slice, []byte(slice)} }
