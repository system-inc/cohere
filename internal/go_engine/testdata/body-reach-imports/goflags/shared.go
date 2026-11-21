// Package gate repeats a struct tag's key, which SA5008 reports unless a file of the package imports go-flags.
package gate

// Target repeats a struct tag's default key.
func Target() int {
	var x struct {
		A int `default:"1" default:"2"`
	}
	return x.A
}
