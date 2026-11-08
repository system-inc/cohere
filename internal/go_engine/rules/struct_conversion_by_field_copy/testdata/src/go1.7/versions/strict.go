// Package versions holds the shapes the version gate parts, in a module whose go line is 1.7: an untagged file of the
// module's version, where a conversion requires identical tags, and a tagged file, which the type checker gives Go
// 1.21 at least, where tags are ignored.
package versions

type t1 struct {
	a int
	b int
}

type t2 struct {
	a int
	b int
}

type tagged struct {
	a int `tag`
	b int `tag`
}

type otherTag struct {
	a int `other`
	b int `tag`
}

func strict() {
	v1 := t1{}
	_ = t2{v1.a, v1.b} // reported
	_ = tagged{v1.a, v1.b}
	_ = tagged{a: v1.a, b: v1.b}
	v2 := otherTag{}
	_ = tagged{v2.a, v2.b}
	v3 := tagged{}
	_ = tagged{v3.a, v3.b}
}
