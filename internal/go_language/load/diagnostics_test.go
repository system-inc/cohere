package load

import (
	"testing"

	"golang.org/x/tools/go/packages"
)

// Every form of Pos go/packages writes is read, a go list position relative to the directory it ran in is
// made absolute, a position that leads a message with no Pos is read out of it, and an error with no position
// at all is kept with an empty one.
func TestDiagnosticOfReadsEveryPositionForm(test *testing.T) {
	test.Parallel()

	for _, expected := range []struct {
		name  string
		given packages.Error
		want  Diagnostic
	}{
		{
			name:  "file, line and column",
			given: packages.Error{Pos: "/work/a.go:3:7", Msg: "bad", Kind: packages.TypeError},
			want:  Diagnostic{Kind: KindType, File: "/work/a.go", Line: 3, Column: 7, Message: "bad"},
		},
		{
			name:  "file and line",
			given: packages.Error{Pos: "/work/a.go:3", Msg: "bad", Kind: packages.ParseError},
			want:  Diagnostic{Kind: KindParse, File: "/work/a.go", Line: 3, Message: "bad"},
		},
		{
			name:  "file alone",
			given: packages.Error{Pos: "/work/a.go", Msg: "bad", Kind: packages.ListError},
			want:  Diagnostic{Kind: KindList, File: "/work/a.go", Message: "bad"},
		},
		{
			name:  "relative to the workspace",
			given: packages.Error{Pos: "sub/a.go:3:8", Msg: "bad", Kind: packages.ListError},
			want:  Diagnostic{Kind: KindList, File: "/work/sub/a.go", Line: 3, Column: 8, Message: "bad"},
		},
		{
			name:  "a dash",
			given: packages.Error{Pos: "-", Msg: "sources missing", Kind: packages.ParseError},
			want:  Diagnostic{Kind: KindParse, Message: "sources missing"},
		},
		{
			name:  "nothing",
			given: packages.Error{Msg: "no position: at all"},
			want:  Diagnostic{Kind: KindUnknown, Message: "no position: at all"},
		},
		{
			name:  "leading the message",
			given: packages.Error{Msg: "/work/a_test.go:5:1: wrong signature", Kind: packages.ListError},
			want:  Diagnostic{Kind: KindList, File: "/work/a_test.go", Line: 5, Column: 1, Message: "wrong signature"},
		},
	} {
		test.Run(expected.name, func(test *testing.T) {
			test.Parallel()

			if got := diagnosticOf("/work", expected.given); got != expected.want {
				test.Fatalf("want %+v, got %+v", expected.want, got)
			}
		})
	}
}
