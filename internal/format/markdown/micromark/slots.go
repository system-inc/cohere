package micromark

// slots hands out values of T, each made once by its fresh function and reused by later parses.
type slots[T any] struct {
	values []*T
	used   int
}

func (slots *slots[T]) take(fresh func() *T) *T {
	if slots.used == len(slots.values) {
		slots.values = append(slots.values, fresh())
	}
	value := slots.values[slots.used]
	slots.used++
	return value
}

// release hands every value taken back, each reset for its next parse.
func (slots *slots[T]) release(reset func(*T)) {
	for _, value := range slots.values[:slots.used] {
		reset(value)
	}
	slots.used = 0
}
