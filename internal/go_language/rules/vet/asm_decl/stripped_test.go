package asm_decl

import (
	"reflect"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/parity"
)

// The pass judges only the declarations without a body (analyzer.go's run), and stage 3's strip takes a standing
// body out of the type check alone (#9fbrnra), so the pass, reading the original syntax, still sees foo's body. The
// stripped run finds what the plain one does, finding for finding: foo's assembly has no Go declaration, and bar's
// frame, laid out from a signature whose nodes the strip shares, is the wrong size. A strip that handed the pass the
// bodiless copies would give foo a frame, and foo's finding would go, so the fixture tells the two apart.
func TestAStrippedBodyIsStillABody(test *testing.T) {
	test.Parallel()
	plainFindings, strippedFindings := parity.StrippedFindings(test, Analyzer, analysistest.TestData(),
		parity.Strip{Functions: []string{"foo"}}, "stripped")
	missing := "] foo: function foo missing Go declaration"
	wrongSize := "] bar: wrong argument size 8; expected $...-4"
	if len(plainFindings) != 2 || !strings.HasSuffix(plainFindings[0].Message, missing) ||
		!strings.HasSuffix(plainFindings[1].Message, wrongSize) {
		test.Fatalf("the plain run found %v, want foo's missing declaration and bar's wrong size", plainFindings)
	}
	if !reflect.DeepEqual(strippedFindings, plainFindings) {
		test.Errorf("stripping foo's body from the type check found %v, where the plain run found %v",
			strippedFindings, plainFindings)
	}

	_, bodilessFindings := parity.StrippedFindings(test, Analyzer, analysistest.TestData(),
		parity.Strip{Functions: []string{"foo"}, BodilessSyntax: true}, "stripped")
	if lost := parity.Difference(plainFindings, bodilessFindings); len(lost) != 1 ||
		!strings.HasSuffix(lost[0].Message, missing) || len(bodilessFindings) != 1 {
		test.Errorf("a strip that hands the pass foo without its body found %v, want all but foo's missing "+
			"declaration of the plain run's %v", bodilessFindings, plainFindings)
	}
}
