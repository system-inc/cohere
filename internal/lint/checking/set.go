// Source: typescript-go `internal/core/set.go`, by way of tsgolint's copy of it.
//
// Our shim re-exports `core` but not `Set`, and upstream has since moved the type to
// `internal/collections`, which is not shimmed either. So this is the only `Set` reachable from our
// code rather than a third copy of one we already have.
package type_checking

type Set[T comparable] struct {
	M map[T]struct{}
}

// NewSetWithSizeHint creates a new Set with a hint for the number of elements it will contain.
func NewSetWithSizeHint[T comparable](hint int) *Set[T] {
	return &Set[T]{
		M: make(map[T]struct{}, hint),
	}
}

func (set *Set[T]) Has(key T) bool {
	_, ok := set.M[key]
	return ok
}

func (set *Set[T]) Add(key T) {
	if set.M == nil {
		set.M = make(map[T]struct{})
	}
	set.M[key] = struct{}{}
}

func (set *Set[T]) Delete(key T) {
	delete(set.M, key)
}

func (set *Set[T]) Len() int {
	return len(set.M)
}

func (set *Set[T]) Keys() map[T]struct{} {
	return set.M
}

func (set *Set[T]) Clear() {
	clear(set.M)
}

func NewSetFromItems[T comparable](items ...T) *Set[T] {
	s := &Set[T]{}
	for _, item := range items {
		s.Add(item)
	}
	return s
}
