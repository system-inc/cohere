package flagged

// A theme is for a file of a type's methods. The file declaring the type is named for the type alone.
type Queue struct { // want "`Queue` is the one type with methods served in `queue_helpers.go`, .* `queue.go`.$"
	items []string
}

func (queue *Queue) Push(item string) {
	queue.items = append(queue.items, item)
}
