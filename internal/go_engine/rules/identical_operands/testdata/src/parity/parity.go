// Package parity holds binary expressions with identical operands under every operator, of every kind of type the
// check tells floats apart from, and the expressions it spares, where the port and staticcheck's SA4000 still agree
// finding for finding. The differential test runs both over each and compares.
package parity

import (
	"math/rand"
	randv2 "math/rand/v2"
)

type point struct{ x, y int }

type measured struct {
	name  string
	value float64
}

type nestedFloat struct{ inner measured }

type celsius float64

type grid [3][3]int

type samples [3]float32

func compute() int { return 1 }

// Found by both, one each: every operator for which identical operands are a mistake.
func operators(a int, b bool) {
	_ = a == a
	_ = a != a
	_ = a - a
	_ = a / a
	_ = a & a
	_ = a % a
	_ = a | a
	_ = a ^ a
	_ = a &^ a
	_ = b && b
	_ = b || b
	_ = a < a
	_ = a > a
	_ = a <= a
	_ = a >= a
}

// Found by neither: operators for which identical operands make sense.
func sensibleOperators(a int, text string) {
	_ = a + a
	_ = a * a
	_ = a << a
	_ = a >> a
	_ = text + text
}

// Found by both: strings, pointers, channels, structs and arrays of no floats, and complex numbers.
func otherTypes(text string, pointer *int, channel chan int, value point, cells grid, number complex128) {
	_ = text < text
	_ = pointer == pointer
	_ = channel == channel
	_ = value == value
	_ = cells == cells
	_ = number == number
}

// Found by neither: floats and what holds them.
func floats(value float64, temperature celsius, record measured, nested nestedFloat, values samples, small float32) {
	_ = value == value
	_ = value - value
	_ = temperature != temperature
	_ = record == record
	_ = nested == nested
	_ = values == values
	_ = small < small
}

// Found by neither: an interface, whose dynamic value may be a float.
func interfaces(value any, err error) {
	_ = value == value
	_ = err != err
}

// Found by both: parenthesized operands, both of one kind.
func parenthesized(a int) {
	_ = (a) == (a)
	_ = (a + 1) == (a + 1)
}

// Found by neither: one operand parenthesized and the other not, two kinds of syntax.
func mixedParentheses(a int) {
	_ = (a) == a
}

// Found by both: calls printed alike, even if they have effects.
func calls(values []int) {
	_ = compute() == compute()
	_ = len(values) - len(values)
	_ = rand.Perm(3)[0] == rand.Perm(3)[0]
}

// Found by neither: two calls of one random function.
func random(generator *rand.Rand, generatorV2 *randv2.Rand) {
	_ = rand.Intn(2) - rand.Intn(2)
	_ = generator.Int63() == generator.Int63()
	_ = randv2.IntN(2) - randv2.IntN(2)
	_ = generatorV2.Uint64() != generatorV2.Uint64()
	_ = randv2.N(5) == randv2.N(5)
}

// Found by both: a random function's result on the right only, which is not spared.
func randomOnTheRight(a int) {
	_ = a+rand.Intn(2) == a+rand.Intn(2)
}

// Found by both: constants and literals.
func constants() {
	const limit = 3
	_ = limit == limit
	_ = 0 == 0
	_ = "a" == "a"
	_ = true && true
}

// Found by neither: different operands.
func different(a int, b int) {
	_ = a == b
	_ = a - 1
}

// Found by both: type parameters whose types are all neither floats nor unrestricted.
func genericIntegers[T ~int | ~string](value T) {
	_ = value == value
}

// Found by neither: a type parameter that may be a float, and one with no restriction on its types.
func genericFloats[T ~int | ~float64, U comparable](value T, other U) {
	_ = value == value
	_ = other == other
}

// Found by neither: a blank variable given two empty literals compared, a compile-time assertion of comparability.
var _ = point{} == point{}

// Found by both: a named variable given the same comparison.
var named = point{} == point{}

// Found by both: the comparison in parentheses, which the assertion's exception does not look through.
var _ = (point{} == point{})

// Found by both: literals that are not empty.
var _ = point{1, 2} == point{1, 2}

// Found by neither: the blank variable of a pair.
var _, second = point{} == point{}, 1

// Found by both: the second of a pair, named.
var first, other = 1, point{} == point{}

func assignment() {
	// Found by both: an assignment, not a variable's declaration.
	_ = point{} == point{}
}
