// Package dotted calls regexp's functions through a dot import, which names them by their package's path all the same.
package dotted

import . "regexp"

func found() {
	MustCompile("\\.")   // found, named regexp.MustCompile
	Compile("\\.")       // found, named regexp.Compile
	(MustCompile)("\\d") // found through the function's parentheses
}

func nearMisses() {
	MustCompile(`\\.`)     // already raw
	MatchString("\\.", "") // another function of regexp
	compile := MustCompile
	compile("\\.") // a local holding the function
}
