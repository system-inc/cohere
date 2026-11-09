package intermediate_representation

// The Next instruction reads and advances the (map or string)
// iterator Iterator and returns a 3-tuple value (ok, k, v).  If the
// iterator is not exhausted, ok is true and k and v are the next
// elements of the domain and range, respectively.  Otherwise ok is
// false and k and v are undefined.
//
// Components of the tuple are accessed using Extract.
//
// The IsString field distinguishes iterators over strings from those
// over maps, as the Type() alone is insufficient: consider
// map[int]rune.
//
// Type() returns a *types.Tuple for the triple (ok, k, v).
// The types of k and/or v may be types.Invalid.
//
// Example printed form:
//
//	t1 = next t0
type Next struct {
	register
	Iterator Value
	IsString bool // true => string iterator; false => map iterator.
}

func (next *Next) Operands(rands []*Value) []*Value {
	return append(rands, &next.Iterator)
}
