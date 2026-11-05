package lookalike

import "fmt"

func start() {}

func sum(values ...int) int { return len(values) }

func describe(values ...any) string { return fmt.Sprint(values...) }

// boundTypeSwitch prints an int in one clause and an any in the other: the same tokens pass two different types.
func boundTypeSwitch(value any) {
	switch typed := value.(type) {
	case int:
		fmt.Println(typed)
	default:
		fmt.Println(typed)
	}
}

// spreadAgainstSlice differ only in the ellipsis: one passes each element, the other the slice as one value.
func spreadAgainstSlice(each bool, values []any) string {
	if each {
		return describe(values...)
	} else {
		return describe(values)
	}
}

// rawAgainstQuoted spell the same text two ways, and literals are compared as written.
func rawAgainstQuoted(highQuality bool) string {
	if highQuality {
		return "pro"
	} else {
		return `pro`
	}
}

// fallsThrough ties its first clause to the default, so the clauses differ by the fallthrough.
func fallsThrough(fast bool) {
	switch {
	case fast:
		start()
		fallthrough
	default:
		start()
	}
}

// waitsOnEither does the same with whichever channel is ready first, which is what a select is for.
func waitsOnEither(first chan int, second chan int) int {
	select {
	case value := <-first:
		return sum(value)
	case value := <-second:
		return sum(value)
	}
}

// sameShapeDifferentCall differ in the one name they call.
func sameShapeDifferentCall(ready bool) {
	if ready {
		fmt.Println("ready")
	} else {
		fmt.Print("ready")
	}
}
