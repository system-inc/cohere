package parity

import (
	"reflect"
	"strings"
	"testing"
)

// An assembly file's comments are read as the Go assembler lexes them: a `//` in a string or a character literal
// opens none, a block comment is one, and each comment is on the lines it opens and ends on.
func TestAssemblyCommentsAreTheAssemblersComments(test *testing.T) {
	test.Parallel()
	source := strings.Join([]string{
		"// One opening the file.",
		`DATA ·text+0(SB)/8, $"// port "`,
		"MOVL $'/', AX // port finds: after a character",
		"/* a block",
		"   // port finds: in a block */ RET",
	}, "\n")
	comments, readError := assemblyComments("comments.s", []byte(source))
	if readError != nil {
		test.Fatal(readError)
	}
	wanted := []fixtureComment{
		{line: 1, endLine: 1, text: "// One opening the file."},
		{line: 3, endLine: 3, text: "// port finds: after a character"},
		{line: 4, endLine: 5, text: "/* a block\n   // port finds: in a block */"},
	}
	if !reflect.DeepEqual(comments, wanted) {
		test.Errorf("the comments are %+v, want %+v", comments, wanted)
	}
	if _, readError := assemblyComments("unterminated.s", []byte(`DATA ·text+0(SB)/8, $"open`)); readError == nil {
		test.Error("an unterminated string read clean, where the assembler refuses it")
	}
}
