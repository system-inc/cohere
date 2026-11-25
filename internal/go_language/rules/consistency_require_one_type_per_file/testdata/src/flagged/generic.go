package flagged

// A generic receiver counts as its type, the type arguments dropped, so Queue[T] here is Queue.
type Stack struct {
	items []int
}

func (stack *Stack) Push(item int) {
	stack.items = append(stack.items, item)
}

func (queue *Queue[T]) Push(item T) { // want "already serves `Stack`, and `Queue` is a second type with methods"
	queue.items = append(queue.items, item)
}
