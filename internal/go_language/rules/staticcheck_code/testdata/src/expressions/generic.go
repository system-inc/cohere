//go:build go1.18

package expressions

type pair[First any, Second any] struct {
	first  First
	second Second
}

func identity[Value any](value Value) Value {
	return value
}

func generic() {
	_ = pair[int, string]{first: 1, second: "one"}
	_ = identity[int](1)
	_ = identity(2)
}
