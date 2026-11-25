// Package calls holds the calls the kit's test rules judge, one of each shape the kit places a mark on differently.
package calls

func single(value int) {}

type receiver struct{}

func (receiver) method(first int, second string) {}

func (*receiver) pointerMethod(value any) {}

func pair() (int, string) { return 0, "" }

func takesPair(first int, second string) {}

func variadic(values ...int) {}

func sequence(yield func() bool) {}

func calls(condition bool) {
	single(1 + 2)
	receiver{}.method(3, "four")
	(&receiver{}).pointerMethod(5)
	takesPair(pair())
	defer single(6)
	go single(7)
	variadic()
	variadic(8, 9)
	number := 10
	if condition {
		number = 11
	}
	single(number)
	for range sequence {
	}
}
