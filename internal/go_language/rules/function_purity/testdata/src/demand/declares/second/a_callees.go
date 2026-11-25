package second

// Zero is pure: a basic parameter, a result, and arithmetic. Its type parameter is unused.
func Zero[T any](value int) int {
	return value + 1
}

// Count is a basic type with a method.
type Count int

// Double is pure, and its receiver is a value.
func (count Count) Double() int {
	return int(count)*2 + 1
}
