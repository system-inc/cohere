//go:build go1.7

package versions

func lenient() {
	v1 := t1{}
	_ = t2{v1.a, v1.b}     // reported
	_ = tagged{v1.a, v1.b} // reported, tags ignored where the file's version is go1.21
	v2 := otherTag{}
	_ = tagged{v2.a, v2.b} // reported
}
