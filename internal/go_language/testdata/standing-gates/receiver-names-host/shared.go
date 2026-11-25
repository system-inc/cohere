package gate

type Thing struct{}

func (t Thing) A() {}

func (t *Thing) B() {}

// Target stands in the other build, and everything in its body looks like a receiver of Thing without being one: a
// local Thing of its own, a closure whose named parameters are one and a pointer to one, and a function value whose
// named parameter is one. It names nothing of the package's Thing, which the other build's methods differ on, so the
// reach leaves it standing.
func Target() int {
	type Thing struct{ count int }
	describe := func(thing *Thing, t Thing) int { return thing.count + t.count }
	var through func(thing Thing) int = func(t Thing) int { return t.count }
	_, _ = describe, through
	return Thing{}.count
}
