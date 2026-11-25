package tests

// Exported is read by the external test.
var Exported = 1

//line elsewhere_test.go:1
func underTestDirective() int { return Exported }
