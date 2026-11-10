package staticcheck_code

import (
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// IsInTest is whether node is in a test file, one whose name ends in _test.go. It is staticcheck's code.IsInTest,
// from honnef.co/go/tools@v0.8.1 analysis/code/code.go.
//
// It reads the name the file was parsed from, never one a line directive gives a position, so a test file whose
// directives name another file is still a test file. A node at no position, or at one in no file of the file set, is
// in none: the package initializer the intermediate representation synthesizes is at none, so the initializers of a
// test file's package-level variables, built into it, are in no test file, as the original's own comment admits, kept.
func IsInTest(context *rule.Context, node staticcheck_report.Positioner) bool {
	file := context.FileSet.File(node.Pos())
	return file != nil && strings.HasSuffix(file.Name(), "_test.go")
}
