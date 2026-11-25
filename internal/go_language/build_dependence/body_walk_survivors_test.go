package build_dependence

import "testing"

// A body that reads what lies under a type reads the type's underlying bit, though it names the type alone: T is Width
// bytes, so its underlying bit is set and its name bit is not, and K, a map's key, the same. Each function below takes
// its T or K by parameter, read by name, and is dependent only by the operation its body applies: Capped, `cap(x)`, a
// builtin's argument; Appended, `append(rows, x)`, a builtin's argument after the first; Sized, `unsafe.Sizeof(x)`,
// a qualified builtin's argument; Looked, `m[k]` over a map of K, the map's key; Indexed, `x[0]`; Sliced, `x[:]`;
// and Ranged, `for range x`. Kept, which hands its T on, reads nothing under it and is independent. Drop or downgrade
// the read of any one of these operations and its function is independent.
func TestABodyReadsTheUnderlyingOfWhatItsOperationsTouch(test *testing.T) {
	test.Parallel()

	host, dependence := besideAHostOnlyWidth(test, `import "unsafe"

type T [Width]byte

type K [Width]byte

type M map[K]int

func Capped(x T) int { return cap(x) }

func Appended(rows []T, x T) []T { return append(rows, x) }

func Sized(x T) uintptr { return unsafe.Sizeof(x) }

func Looked(m M, k K) int { return m[k] }

func Indexed(x T) byte { return x[0] }

func Sliced(x T) []byte { return x[:] }

func Ranged(x T) (count int) {
	for range x {
		count++
	}
	return count
}

func Kept(x T) T { return x }
`)
	expectNameBits(test, dependence, map[string]Bits{
		"T": {Underlying: true},
		"K": {Underlying: true},
		"M": {},
	})
	expectVerdicts(test, verdicts(test, host, dependence), map[string]bool{
		"type T":        true,
		"type K":        true,
		"type M":        false,
		"func Capped":   true,
		"func Appended": true,
		"func Sized":    true,
		"func Looked":   true,
		"func Indexed":  true,
		"func Sliced":   true,
		"func Ranged":   true,
		"func Kept":     false,
		"value Width":   true,
	})
}

// A comparison, a conversion, a type assertion, a type switch's case and an explicit type argument read what lies
// under the types they touch: Equal, `x == y`, and Mirrored, `y == x`, each with y an any; Converted, `T(x)`, the
// conversion's destination; Unconverted, `[4]byte(x)`, its operand; Asserted, `x.(T)`; Switched, a type switch whose
// T is the second type of its second clause; and Paired, `pair[int, T]()`, a second type argument. Each reads its T by
// name otherwise and is dependent only by the operation. Kept reads nothing under T. Drop or downgrade any one of
// these reads and its function is independent.
func TestABodyReadsTheUnderlyingOfWhatItsComparisonsConversionsAndAssertionsTouch(test *testing.T) {
	test.Parallel()

	host, dependence := besideAHostOnlyWidth(test, `type T [Width]byte

func Equal(x T, y any) bool { return x == y }

func Mirrored(x T, y any) bool { return y == x }

func Converted(x [4]byte) any { return T(x) }

func Unconverted(x T) [4]byte { return [4]byte(x) }

func Asserted(x any) bool {
	_, isT := x.(T)
	return isT
}

func Switched(x any) int {
	switch x.(type) {
	case int:
		return 1
	case string, T:
		return 2
	}
	return 0
}

func pair[A, B any]() {}

func Paired() { pair[int, T]() }

func Kept(x T) T { return x }
`)
	expectVerdicts(test, verdicts(test, host, dependence), map[string]bool{
		"type T":           true,
		"func Equal":       true,
		"func Mirrored":    true,
		"func Converted":   true,
		"func Unconverted": true,
		"func Asserted":    true,
		"func Switched":    true,
		"func pair":        false,
		"func Paired":      true,
		"func Kept":        false,
		"value Width":      true,
	})
}

// A var spec's initializers are each its unit's body: Large, the second name of a typed spec, takes `len(Plain)`, an
// array of T's, which reads T's underlying, so the spec, named by Small, is dependent, though its first initializer
// reads nothing. Walk the first initializer alone and it is independent.
func TestEveryInitializerOfAVarSpecIsRead(test *testing.T) {
	test.Parallel()

	host, dependence := besideAHostOnlyWidth(test, `type T [Width]byte

var Plain T

var Small, Large int = 1, len(Plain)
`)
	expectVerdicts(test, verdicts(test, host, dependence), map[string]bool{
		"type T":      true,
		"value Plain": false,
		"value Small": true,
		"value Width": true,
	})
}

// A constant expression reads its type in full: in `take(1)` the constant 1 takes take's parameter type N[T], whose
// type argument T is then read in full, so Constant is dependent, where Variable, which passes an N[T] it holds,
// reads N[T] by name and is not. Skip the full read of a constant's type, or take it for every expression but a
// constant, and Constant is independent.
func TestAConstantExpressionReadsItsTypeInFull(test *testing.T) {
	test.Parallel()

	host, dependence := besideAHostOnlyWidth(test, `type T [Width]byte

type N[P any] int

func take(N[T]) {}

func Constant() { take(1) }

func Variable(n N[T]) { take(n) }
`)
	expectVerdicts(test, verdicts(test, host, dependence), map[string]bool{
		"type T":        true,
		"type N":        false,
		"func take":     false,
		"func Constant": true,
		"func Variable": false,
		"value Width":   true,
	})
}

// A receive, a dereference, a switch's tag, a send, ++, an assignment operator, a parenthesized builtin, a call of a
// named func type or an alias of one, and a field promoted through an embedded pointer read what lies under the types
// they touch: Received, `<-c`, and Sent, `c <- v`, over Ch, a channel of Width bytes; Dereferenced, `*p` over P, a
// pointer to them; Tagged, `switch x`; Incremented, `n++`, and Shifted, `n <<= s`, over N[T], whose type argument a
// full read reaches; ParenCapped, `(cap)(x)`; Called, through Fn, and CalledThroughAlias, through an alias of Fn,
// each a func taking Width bytes; and Counted, Count promoted through an embedded *Inner of an unnamed struct, whose
// own walk reads Inner by name alone. Each is dependent only by its operation. Drop or downgrade any one of these
// reads and its function is independent.
func TestABodyReadsTheUnderlyingOfWhatItsStatementsAndCallsTouch(test *testing.T) {
	test.Parallel()

	host, dependence := besideAHostOnlyWidth(test, `type T [Width]byte

type Ch chan [Width]byte

type P *[Width]byte

type N[A any] int

type Fn func([Width]byte)

type FnAlias = Fn

type Empty struct{}

type Inner struct {
	items [Width]byte
	Count int
}

func Received(c Ch) { _ = <-c }

func Sent(c Ch) { c <- [4]byte{} }

func Dereferenced(p P) { _ = *p }

func Tagged(x, y T) bool {
	switch x {
	case y:
		return true
	}
	return false
}

func Incremented(n N[T]) N[T] {
	n++
	return n
}

func Shifted(n N[T], s uint) N[T] {
	n <<= s
	return n
}

func ParenCapped(x T) int { return (cap)(x) }

func Called(f Fn) { f([4]byte{}) }

func CalledThroughAlias(f FnAlias) { f([4]byte{}) }

func Counted(o struct {
	Empty
	*Inner
}) int {
	return o.Count
}
`)
	expectVerdicts(test, verdicts(test, host, dependence), map[string]bool{
		"type T":                  true,
		"type Ch":                 true,
		"type P":                  true,
		"type N":                  false,
		"type Fn":                 true,
		"type FnAlias":            true,
		"type Empty":              false,
		"type Inner":              true,
		"func Received":           true,
		"func Sent":               true,
		"func Dereferenced":       true,
		"func Tagged":             true,
		"func Incremented":        true,
		"func Shifted":            true,
		"func ParenCapped":        true,
		"func Called":             true,
		"func CalledThroughAlias": true,
		"func Counted":            true,
		"value Width":             true,
	})
}

// A local the body defines is read through its type as every object no unit declares is (throughKey), as a member's
// own type, so an unused parameter of a func literal whose unnamed struct holds a Pad in an unexported field reads
// Pad's underlying, as that parameter's line observes it, and Literal is dependent. The literal's own type reads Pad by
// name alone. Read a defined identifier's object from Uses alone and Literal is independent.
func TestABodyReadsTheLocalsItDefinesThroughTheirTypes(test *testing.T) {
	test.Parallel()

	host, dependence := besideAHostOnlyWidth(test, `type Pad struct{ _ [Width]byte }

func Literal() { _ = func(local struct{ p Pad }) {} }
`)
	expectVerdicts(test, verdicts(test, host, dependence), map[string]bool{
		"type Pad":     true,
		"func Literal": true,
		"value Width":  true,
	})
}
