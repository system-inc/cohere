// Package differences declares, without bodies, methods of a generic type whose frames go/types cannot measure,
// where the original crashes, beside functions whose frames it can, so the port's skip of the first is shown to leave
// the second judged.
package differences

type box[T any] struct{}

type pair[T any] struct {
	first  T
	second int
}

type other struct{}

// Each of these holds a type parameter by value, as a parameter, an array's element, a struct's field or a result.
func (box[T]) value(x T)
func (box[T]) array(x [2]T)
func (box[T]) wrapped(x pair[T])
func (box[T]) result() T

// These hold a type parameter only behind a pointer and a slice, and measure as words.
func (box[T]) pointer(x *T, l []T)

func plain(x int64) int64

// A later declaration of a name takes an earlier one's place, as the original keeps them: earlier's frame is the
// unmeasured one, and later's the measured one.
func (other) earlier(x int8)
func (box[T]) earlier(x T)
func (box[T]) later(x T)
func (other) later(x int8)
