package flagged

type Queue[T any] struct {
	items []T
}

func (queue Queue[T]) Length() int {
	return len(queue.items)
}
