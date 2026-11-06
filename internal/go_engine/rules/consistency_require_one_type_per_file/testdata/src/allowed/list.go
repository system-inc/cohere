package allowed

// A generic type with its methods.
type List[T any] struct {
	items []T
}

func (list *List[T]) Push(item T) {
	list.items = append(list.items, item)
}
