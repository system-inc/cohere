package build_reads

import (
	"cmp"
	"go/types"
)

// objectOrdering orders the objects of one sort (see compare), remembering how go/types wrote each object it had to
// write, so an object tied with others on package and name is written once in the sort and not again in each of its
// comparisons. Ties are common: this package's own uses hold 36 names tied so, twelve fields of go/ast named X among
// them, and a sort compares each tied object with its rivals several times. It is made for one sort and dropped when
// the sort returns: its key is the object, by identity, and its value a string, which nothing writes and nothing past
// the sort holds, so it shares nothing with the sorted list it helped build or with any later sort. The zero value is
// ready, and makes no map until a tie needs one.
type objectOrdering struct {
	written map[types.Object]string
}

// compare orders two objects by their package's path, then their name, then how go/types writes them, which tells
// apart two fields or methods of one name on different types, and last by position, for two written alike. One
// object compared with itself is equal before anything is written: the selections a package makes through one
// receiver all compare that receiver with itself, as do the uses of one object in several kinds, and writing a large
// struct twice for each of those comparisons doubled every build's decide on deeafbf4 (#p1s9gdk).
func (ordering *objectOrdering) compare(left types.Object, right types.Object) int {
	if left == right {
		return 0
	}
	order := cmp.Or(cmp.Compare(left.Pkg().Path(), right.Pkg().Path()), cmp.Compare(left.Name(), right.Name()))
	if order != 0 {
		return order
	}
	// Written only for two distinct objects of one package and name, two fields or methods of one name on different
	// types.
	return cmp.Or(
		cmp.Compare(ordering.writing(left), ordering.writing(right)),
		cmp.Compare(left.Pos(), right.Pos()),
	)
}

// writing is how go/types writes object, types.ObjectString with no qualifier, written the first time the sort asks
// and remembered for the rest of it.
func (ordering *objectOrdering) writing(object types.Object) string {
	if written, remembered := ordering.written[object]; remembered {
		return written
	}
	written := types.ObjectString(object, nil)
	if ordering.written == nil {
		ordering.written = map[types.Object]string{}
	}
	ordering.written[object] = written
	return written
}
