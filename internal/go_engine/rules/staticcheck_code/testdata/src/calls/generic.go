//go:build go1.21

package calls

import "slices"

type list[Element any] struct{ elements []Element }

func (l *list[Element]) push(element Element) { l.elements = append(l.elements, element) }

type wrapper[Value any] struct{ value Value }

func identity[Value any](value Value) Value { return value }

func pair[First, Second any](first First, second Second) (First, Second) { return first, second }

func generic(values []int) {
	_ = identity[int](1)
	_ = identity(2)
	_ = (identity[int])(3)
	_, _ = pair[int, string](1, "one")
	_, _ = pair[int](2, "two")
	_ = slices.Contains[[]int](values, 1)
	_ = slices.Index[[]int, int](values, 1)
	_ = slices.Contains(values, 2)
	l := &list[int]{}
	l.push(1)
	(&list[string]{}).push("one")
	_ = wrapper[int](wrapper[int]{value: 1})
	_ = min(1, 2)
	_ = max(1, 2)
	clear(values)
	_ = func(push func(int)) int { push(1); return 0 }(l.push)
}
