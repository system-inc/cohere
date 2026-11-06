package allowed

// A generic type is named without its type parameters.
type List[T any] struct {
	items []T
}

func (list *List[T]) Push(item T) {
	list.items = append(list.items, item)
}
