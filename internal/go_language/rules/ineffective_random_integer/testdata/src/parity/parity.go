// Package parity holds the shapes the pattern `(CallExpr (Symbol name@(Or ...)) [(IntegerLiteral "1")])` accepts and
// refuses: each family of the functions it names, the ways a call reaches one, through methods, embedding, aliases and
// instantiations, and the spellings of an argument that is or is not the literal 1. Each function's comment says what
// the original makes of it; the parity test holds only that the port makes the same.
package parity

import (
	"math/rand"
	randv2 "math/rand/v2"
	"time"
)

type source = *rand.Rand

type embedding struct {
	*rand.Rand
}

type holder struct {
	generator randv2.Rand
}

type bounded interface {
	Intn(n int) int
}

const one = 1

func use(values ...any) {}

// Intn here is the package's own, whose full name is no bounded function's.
func Intn(n int) int { return n }

// Found thrice: math/rand's package functions.
func packageFunctions() {
	use(rand.Int31n(1), rand.Int63n(1), rand.Intn(1))
}

// Found thrice: math/rand's methods, on a pointer, an address taken and an alias.
func methods(generator *rand.Rand, value rand.Rand, aliased source) {
	use(generator.Int31n(1), (&value).Int63n(1), aliased.Intn(1))
}

// Found: a method promoted through an embedded pointer.
func promoted(container embedding) {
	use(container.Intn(1))
}

// Found seven times: math/rand/v2's package functions.
func packageFunctionsV2() {
	use(randv2.Int32N(1), randv2.Int64N(1), randv2.IntN(1), randv2.N(1))
	use(randv2.Uint32N(1), randv2.Uint64N(1), randv2.UintN(1))
}

// Found six times: math/rand/v2's methods, through a field of a value.
func methodsV2(container holder) {
	use(container.generator.Int32N(1), container.generator.Int64N(1), container.generator.IntN(1))
	use(container.generator.Uint32N(1), container.generator.Uint64N(1), container.generator.UintN(1))
}

// Found thrice: N instantiated, written plainly and in parentheses, and of a duration.
func instantiated() {
	use(randv2.N[int](1), (randv2.N[int64])(1), randv2.N[time.Duration](1))
}

// Found five times: the literal 1 in other spellings, each a constant that prints as 1.
func spellings() {
	use(rand.Intn(0x1), rand.Intn(0b1), rand.Intn(+1), rand.Intn(-(-1)), rand.Intn(01))
}

// Found twice: parentheses on the function and on the argument.
func parentheses() {
	use((rand.Intn)(1), rand.Intn((1)))
}

// Found: deferred and in a goroutine, each a call.
func deferredAndSpawned() {
	defer rand.Intn(1)
	go rand.Intn(1)
}

// Not found: other bounds, and a value of 1 that is no literal.
func otherArguments(bound int) {
	use(rand.Intn(2), rand.Intn(0), rand.Intn(bound), rand.Intn(one), rand.Intn(int(1)), rand.Intn(1.0))
	use(randv2.N(time.Duration(1)), rand.Intn('\x01'), rand.Intn(2-1))
}

// Not found: functions the original does not name, an interface's method, a method value and the package's own Intn.
func otherFunctions(generator bounded) {
	use(rand.Int31(), rand.Float64(), randv2.Int(), generator.Intn(1), Intn(1))
	bound := rand.Intn
	use(bound(1))
}
