package intermediate_representation

import "strings"

// IsExample reports whether function is a testable example: its name starts with Example and it is declared in a
// _test.go file. It is staticcheck's irutil.IsExample (honnef.co/go/tools@v0.8.1 go/ir/irutil/util.go), translated
// onto our IR, reading the file through Program.FileSet where go/ir reads Prog.Fset.
func IsExample(function *Function) bool {
	if !strings.HasPrefix(function.Name(), "Example") {
		return false
	}
	// A function with no position, as a synthetic one has, is in no file.
	file := function.Program.FileSet.File(function.Pos())
	if file == nil {
		return false
	}
	return strings.HasSuffix(file.Name(), "_test.go")
}
